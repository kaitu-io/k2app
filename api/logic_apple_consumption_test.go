package center

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// setTestAppleAPIKey 给 appstore.GenerateJwtToken 注入一把临时 ES256 私钥（viper 全局，用完还原）。
func setTestAppleAPIKey(t *testing.T) {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(k)
	require.NoError(t, err)
	vals := map[string]string{
		"appstore.iap.key":    string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})),
		"appstore.iap.keyId":  "TESTKEY123",
		"appstore.iap.issuer": "test-issuer",
	}
	for k, v := range vals {
		old := viper.GetString(k)
		viper.Set(k, v)
		t.Cleanup(func() { viper.Set(k, old) })
	}
}

// fakeConsumptionAPI 起一个 httptest 服务替换两个环境的 base URL，记录收到的每个请求。
type fakeConsumptionAPI struct {
	srv    *httptest.Server
	mu     sync.Mutex
	paths  []string
	bodies []map[string]any
	auths  []string
	method []string
	status atomic.Int32
}

func (f *fakeConsumptionAPI) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.paths)
}

func newFakeConsumptionAPI(t *testing.T) *fakeConsumptionAPI {
	t.Helper()
	f := &fakeConsumptionAPI{}
	f.status.Store(http.StatusAccepted)
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		f.mu.Lock()
		f.paths = append(f.paths, r.URL.Path)
		f.bodies = append(f.bodies, m)
		f.auths = append(f.auths, r.Header.Get("Authorization"))
		f.method = append(f.method, r.Method)
		f.mu.Unlock()
		w.WriteHeader(int(f.status.Load()))
	}))
	t.Cleanup(f.srv.Close)
	old := appleConsumptionBaseURL
	appleConsumptionBaseURL = map[bool]string{false: f.srv.URL, true: f.srv.URL + "/sandbox"}
	t.Cleanup(func() { appleConsumptionBaseURL = old })
	setTestAppleAPIKey(t)
	return f
}

func TestRespondAppleConsumption_NoConsentNoCall(t *testing.T) {
	api := newFakeConsumptionAPI(t)
	sub := &Subscription{TrialEndsAt: 0}
	for _, consent := range []*bool{nil, BoolPtr(false)} {
		u := &User{ID: 1, AppleConsumptionConsent: consent}
		require.NoError(t, respondAppleConsumption(context.Background(), u, sub, "2000000001", "io.kaitu.test", false))
	}
	assert.Equal(t, 0, api.count(), "没同意就不能调用 Apple")
}

func TestRespondAppleConsumption_SendsV2Body(t *testing.T) {
	api := newFakeConsumptionAPI(t)
	cases := []struct {
		brand       Brand
		trialEndsAt int64
		sandbox     bool
		wantPath    string
		wantSample  bool
	}{
		{BrandKaitu, 0, false, "/inApps/v2/transactions/consumption/2000000001", false},
		{BrandOverleap, time.Now().Unix(), true, "/sandbox/inApps/v2/transactions/consumption/2000000001", true},
	}
	for i, tc := range cases {
		u := &User{ID: uint64(i + 1), Brand: string(tc.brand), AppleConsumptionConsent: BoolPtr(true)}
		sub := &Subscription{TrialEndsAt: tc.trialEndsAt}
		require.NoError(t, respondAppleConsumption(context.Background(), u, sub, "2000000001", "io.kaitu.test", tc.sandbox))
		require.Equal(t, i+1, api.count())

		assert.Equal(t, http.MethodPut, api.method[i])
		assert.Equal(t, tc.wantPath, api.paths[i])
		assert.True(t, strings.HasPrefix(api.auths[i], "Bearer ey"), "Bearer JWT: %q", api.auths[i])
		body := api.bodies[i]
		require.NotNil(t, body)
		assert.Equal(t, true, body["customerConsented"])
		assert.Equal(t, "DELIVERED", body["deliveryStatus"])
		assert.Equal(t, "GRANT_PRORATED", body["refundPreference"], "brand=%s", tc.brand)
		assert.Equal(t, tc.wantSample, body["sampleContentProvided"])
		_, has := body["consumptionPercentage"]
		assert.False(t, has, "自动续订 + GRANT_PRORATED 不能带 consumptionPercentage（Apple 400）")
	}
}

func TestRespondAppleConsumption_Non2xxIsError(t *testing.T) {
	api := newFakeConsumptionAPI(t)
	api.status.Store(http.StatusServiceUnavailable)
	u := &User{ID: 1, AppleConsumptionConsent: BoolPtr(true)}
	err := respondAppleConsumption(context.Background(), u, &Subscription{}, "2000000001", "io.kaitu.test", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "503")
}

// ---------- webhook 端到端（DB） ----------

// consumptionWebhookPoster 返回一个投递签名合法的 CONSUMPTION_REQUEST 通知的函数（按 uuid）。
func consumptionWebhookPoster(t *testing.T, f *iapOrderFixture, txnID string, t0 int64) func(uuid string) int {
	rootPEM, leafKey, leafDER, intDER := makeTestChain(t)
	old := appleRootCAPEM
	appleRootCAPEM = rootPEM
	t.Cleanup(func() { appleRootCAPEM = old })
	return func(uuid string) int {
		tj, _ := json.Marshal(map[string]any{
			"bundleId": "io.kaitu.test", "originalTransactionId": f.origTxn, "transactionId": txnID, "productId": f.productID,
			"inAppOwnershipType": "PURCHASED", "environment": "Production",
			"purchaseDate": t0 * 1000, "expiresDate": (t0 + 365*aDay) * 1000, "signedDate": time.Now().UnixMilli(),
		})
		inner := signJWS(t, leafKey, leafDER, intDER, string(tj))
		outer, _ := json.Marshal(map[string]any{
			"notificationType": "CONSUMPTION_REQUEST", "notificationUUID": uuid,
			"version": "2.0", "signedDate": time.Now().UnixMilli(),
			"data": map[string]any{"bundleId": "io.kaitu.test", "environment": "Production", "signedTransactionInfo": inner},
		})
		body, _ := json.Marshal(map[string]string{"signedPayload": signJWS(t, leafKey, leafDER, intDER, string(outer))})
		gin.SetMode(gin.TestMode)
		r := gin.New()
		r.POST("/webhook/apple", api_apple_webhook)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/webhook/apple", bytes.NewReader(body)))
		return w.Code
	}
}

func consumptionWebhookFixture(t *testing.T) (*iapOrderFixture, func(uuid string) int) {
	t.Helper()
	skipIfNoDB(t)
	setTestAppleBundleID(t)
	captureBillingAlerts(t)
	f := setupIAPOrderFixture(t, 30, 10)
	f.noInvite(t)
	t0 := time.Now().Unix()
	require.NoError(t, f.credit(t, "WH-CR1", t0, t0+365*aDay))
	require.NoError(t, db.Get().Model(&User{}).Where("id = ?", f.buyer.ID).
		Update("apple_consumption_consent", true).Error)
	return f, consumptionWebhookPoster(t, f, "WH-CR1", t0)
}

func TestAppleWebhook_ConsumptionApple5xxReturns500(t *testing.T) {
	f, post := consumptionWebhookFixture(t)
	api := newFakeConsumptionAPI(t)
	api.status.Store(http.StatusServiceUnavailable)

	uuid := fmt.Sprintf("uuid-cr-5xx-%d", time.Now().UnixNano())
	assert.Equal(t, 500, post(uuid), "Apple 5xx → 让 Apple 重投")
	assert.Equal(t, 1, api.count())
	assert.NotEqual(t, uuid, f.subNow(t).LastEventID, "失败不记 UUID，否则重投被去重挡掉")
}

func TestAppleWebhook_ConsumptionDuplicateUUIDSendsOnce(t *testing.T) {
	f, post := consumptionWebhookFixture(t)
	api := newFakeConsumptionAPI(t)

	uuid := fmt.Sprintf("uuid-cr-dup-%d", time.Now().UnixNano())
	require.Equal(t, 200, post(uuid))
	require.Equal(t, 200, post(uuid))
	assert.Equal(t, 1, api.count(), "同一 UUID 只应答一次")
	assert.Equal(t, "/inApps/v2/transactions/consumption/WH-CR1", api.paths[0])
	assert.Equal(t, uuid, f.subNow(t).LastEventID)
}

func TestAppleWebhook_ConsumptionNoConsentAcksWithoutCall(t *testing.T) {
	f, post := consumptionWebhookFixture(t)
	require.NoError(t, db.Get().Model(&User{}).Where("id = ?", f.buyer.ID).
		Update("apple_consumption_consent", nil).Error)
	api := newFakeConsumptionAPI(t)

	uuid := fmt.Sprintf("uuid-cr-nc-%d", time.Now().UnixNano())
	require.Equal(t, 200, post(uuid))
	assert.Equal(t, 0, api.count())
	assert.Equal(t, uuid, f.subNow(t).LastEventID)
}

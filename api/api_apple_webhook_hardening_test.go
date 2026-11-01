package center

import (
	"bytes"
	"context"
	"encoding/asn1"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wordgate/qtoolkit/appstore"
	db "github.com/wordgate/qtoolkit/db"
)

// 链到 Apple 根不够：Apple 根还签发别的用途的证书，webhook 必须像客户端交易一样校验用途扩展。
func TestVerifyAppleJWS_RequiresAppStoreOIDs(t *testing.T) {
	other := asn1.ObjectIdentifier{1, 2, 3, 4}
	cases := []struct {
		name        string
		leaf, inter asn1.ObjectIdentifier
	}{
		{"leaf without OID", nil, appleJWSIntermediateOID},
		{"intermediate without OID", appleJWSLeafOID, nil},
		{"leaf with another purpose", other, appleJWSIntermediateOID},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rootPEM, key, leafDER, intDER := makeTestChainWithOIDs(t, tc.leaf, tc.inter)
			jws := signJWS(t, key, leafDER, intDER, `{"notificationType":"REFUND"}`)
			require.ErrorContains(t, verifyAppleJWSWithRoot(jws, rootPEM), "App Store signing chain")
		})
	}
	t.Run("correct chain still passes", func(t *testing.T) {
		rootPEM, key, leafDER, intDER := makeTestChain(t)
		require.NoError(t, verifyAppleJWSWithRoot(signJWS(t, key, leafDER, intDER, `{}`), rootPEM))
	})
}

func postSignedNotification(t *testing.T, claims map[string]any) int {
	t.Helper()
	rootPEM, key, leafDER, intDER := makeTestChain(t)
	old := appleRootCAPEM
	appleRootCAPEM = rootPEM
	t.Cleanup(func() { appleRootCAPEM = old })
	raw, err := json.Marshal(claims)
	require.NoError(t, err)
	body, err := json.Marshal(map[string]string{"signedPayload": signJWS(t, key, leafDER, intDER, string(raw))})
	require.NoError(t, err)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/webhook/appstore", api_apple_webhook)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/webhook/appstore", bytes.NewReader(body)))
	return w.Code
}

// 不带交易的通知（TEST、SUMMARY）不是坏请求：回 400 只会被 Apple 反复重投。
func TestAppleWebhook_NotificationWithoutTransactionIsAcked(t *testing.T) {
	code := postSignedNotification(t, map[string]any{
		"notificationType": "TEST", "notificationUUID": "uuid-test", "version": "2.0",
		"data": map[string]any{"bundleId": "io.kaitu.test", "environment": "Production"},
	})
	assert.Equal(t, 200, code)
}

// 带了交易却解不开是真问题，不能当"没有交易"吞掉。
func TestAppleWebhook_UndecodableTransactionIsRejected(t *testing.T) {
	code := postSignedNotification(t, map[string]any{
		"notificationType": "DID_RENEW", "notificationUUID": "uuid-bad", "version": "2.0",
		"data": map[string]any{"bundleId": "io.kaitu.test", "signedTransactionInfo": "not-a-jws"},
	})
	assert.Equal(t, 400, code)
}

func TestAppleWebhookBodyLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	var readErr error
	r.POST("/x", appleWebhookBodyLimit(), func(c *gin.Context) {
		buf := make([]byte, 64*1024)
		for readErr == nil {
			_, readErr = c.Request.Body.Read(buf)
		}
		c.Status(200)
	})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/x",
		strings.NewReader(strings.Repeat("a", appleWebhookMaxBody+1))))
	var tooBig *http.MaxBytesError
	require.ErrorAs(t, readErr, &tooBig)
}

// 限制必须挂在请求日志之前：日志中间件会先把整个 body 读进内存并写日志。
func TestAppleWebhookRoute_BodyLimitBeforeRequestLog(t *testing.T) {
	raw, err := os.ReadFile("route.go")
	require.NoError(t, err)
	src := string(raw)
	i := strings.Index(src, `r.POST("/webhook/appstore"`)
	require.GreaterOrEqual(t, i, 0)
	line := src[i : i+strings.Index(src[i:], "\n")]
	limit, logMw := strings.Index(line, "appleWebhookBodyLimit()"), strings.Index(line, "log.MiddlewareRequestLog")
	require.GreaterOrEqual(t, limit, 0)
	assert.Less(t, limit, logMw)
}

func TestAppleWebhookDrift(t *testing.T) {
	live := appleWebhookLiveSince.Add(time.Hour)
	before := func() *Subscription {
		return &Subscription{Provider: SubscriptionProviderApple, AutoRenew: true, Environment: "Production",
			Status: "active", CreatedAt: live, LastEventID: "evt-1"}
	}
	after := func(mut func(*Subscription)) *Subscription {
		s := before()
		s.AutoRenew = false
		if mut != nil {
			mut(s)
		}
		return s
	}
	assert.True(t, appleWebhookDrift(before(), after(nil)), "对账才发现关续费 = webhook 本该送达")
	assert.False(t, appleWebhookDrift(before(), before()), "无变化")
	assert.False(t, appleWebhookDrift(before(), after(func(s *Subscription) { s.Environment = "Sandbox" })), "沙盒不算")
	assert.False(t, appleWebhookDrift(before(), after(func(s *Subscription) { s.CreatedAt = appleWebhookLiveSince.Add(-time.Hour) })), "配置 webhook 之前的链是存量陈旧值")
	assert.False(t, appleWebhookDrift(before(), after(func(s *Subscription) { s.Provider = SubscriptionProviderStripe })), "Stripe 不算")
	assert.False(t, appleWebhookDrift(before(), after(func(s *Subscription) { s.LastEventID = "evt-2" })), "扫描途中 webhook 刚写过")
	expired := before()
	expired.Status = "expired"
	assert.False(t, appleWebhookDrift(expired, after(nil)), "过期行由对账补写开关，Apple 不发通知")
}

// 迟到 / 重投的 EXPIRED 不能把已重新订阅的链打回过期，也不能清试用标志。
func TestAppleExpired_StaleNotificationIgnoredAfterResubscribe(t *testing.T) {
	trialDB(t)
	f := setupIAPOrderFixture(t, 30, 10)
	f.noInvite(t)
	t0 := time.Now().Unix()
	require.NoError(t, creditInfo(t, f.buyer.ID, f.trialTxn("HD-E1", t0, t0+7*aDay)))
	sub := f.subNow(t)
	require.NoError(t, applyAppleExpired(context.Background(), &sub))
	assert.Equal(t, "active", f.subNow(t).Status, "周期仍在未来")
	assert.True(t, f.userNow(t, f.buyer.ID).InTrial, "试用标志不动")

	// 对照：周期确实结束后同一通知生效。
	require.NoError(t, db.Get().Model(&Subscription{}).Where("id = ?", sub.ID).Update("current_period_end", t0-60).Error)
	sub = f.subNow(t)
	require.NoError(t, applyAppleExpired(context.Background(), &sub))
	assert.Equal(t, "expired", f.subNow(t).Status)
	assert.False(t, f.userNow(t, f.buyer.ID).InTrial)
}

// Apple 不保证通知顺序：更早签名的续订状态不能盖掉更新的。
func TestApplyRenewalInfo_IgnoresOlderSignedState(t *testing.T) {
	trialDB(t)
	f := setupIAPOrderFixture(t, 30, 10)
	f.noInvite(t)
	t0 := time.Now().Unix()
	require.NoError(t, creditInfo(t, f.buyer.ID, f.trialTxn("HD-R1", t0, t0+7*aDay)))
	sub := f.subNow(t)
	nowMs := time.Now().UnixMilli()
	on := &appstore.RenewalInfo{AutoRenewStatus: appstore.AutoRenewStatus_On, SignedDate: nowMs}
	off := &appstore.RenewalInfo{AutoRenewStatus: appstore.AutoRenewStatus_Off, SignedDate: nowMs - 60_000}

	require.NoError(t, applyRenewalInfo(context.Background(), &sub, on, appstore.Subtype_AUTO_RENEW_ENABLED))
	require.NoError(t, applyRenewalInfo(context.Background(), &sub, off, appstore.Subtype_AUTO_RENEW_DISABLED))
	assert.True(t, f.subNow(t).AutoRenew, "旧的关闭通知被忽略")

	newer := &appstore.RenewalInfo{AutoRenewStatus: appstore.AutoRenewStatus_Off, SignedDate: nowMs + 60_000}
	require.NoError(t, applyRenewalInfo(context.Background(), &sub, newer, appstore.Subtype_AUTO_RENEW_DISABLED))
	assert.False(t, f.subNow(t).AutoRenew, "更新的状态正常落地")
}

// 全量对账不看 id 分桶：连着 7 个 id 必然跨遍所有桶。沙盒和已过期行不碰。
func TestReconcileAppleActive_CoversEveryBucketOnlyActiveProduction(t *testing.T) {
	trialDB(t)
	setTestAppleBundleID(t)
	f := setupIAPOrderFixture(t, 30, 10)
	now := time.Now()

	polled := map[string]bool{}
	oldFetch := fetchAppleSubStatus
	fetchAppleSubStatus = func(_ context.Context, _, otx string) (*appleSubStatus, error) {
		polled[otx] = true
		if !strings.HasPrefix(otx, "HD-ALL-") {
			return nil, errors.New("not part of this test")
		}
		return &appleSubStatus{status: 1, renewal: &appstore.RenewalInfo{
			AutoRenewStatus: appstore.AutoRenewStatus_Off, SignedDate: now.UnixMilli(),
		}}, nil
	}
	t.Cleanup(func() { fetchAppleSubStatus = oldFetch })

	seed := func(prefix, env, status string, i int) string {
		otx := fmt.Sprintf("%s%d-%d", prefix, now.UnixNano(), i)
		sub := Subscription{
			UserID: f.buyer.ID, Provider: SubscriptionProviderApple, ProviderSubscriptionID: otx,
			ProductID: f.plan.AppleProductID, CurrentPeriodEnd: now.Unix() + 30*86400, AutoRenew: true,
			Environment: env, Status: status,
		}
		require.NoError(t, db.Get().Create(&sub).Error)
		t.Cleanup(func() { db.Get().Delete(&Subscription{}, sub.ID) })
		return otx
	}
	var want []string
	for i := 0; i < appleReconcileBuckets; i++ {
		want = append(want, seed("HD-ALL-", "Production", "active", i))
	}
	sandbox := seed("HD-SKIP-", "Sandbox", "active", 0)
	expired := seed("HD-SKIP-", "Production", "expired", 1)

	_, _, _, off, err := ReconcileAppleActive(context.Background())
	require.NoError(t, err)
	for _, otx := range want {
		assert.True(t, polled[otx], otx)
	}
	assert.False(t, polled[sandbox])
	assert.False(t, polled[expired])
	assert.GreaterOrEqual(t, off, appleReconcileBuckets)
}

// 告警走完整的每日对账：对账发现 webhook 本该送达的开关变化 → 发 APPLE-WEBHOOK-SILENT；少于阈值不发。
func TestReconcileSweep_AlertsWhenWebhookSilent(t *testing.T) {
	trialDB(t)
	setTestAppleBundleID(t)
	f := setupIAPOrderFixture(t, 30, 10)
	now := time.Now()

	var alerts []string
	oldAlert := alertPaymentAnomaly
	alertPaymentAnomaly = func(_ context.Context, tag, _ string, _ ...any) { alerts = append(alerts, tag) }
	t.Cleanup(func() { alertPaymentAnomaly = oldAlert })

	drifted := map[string]bool{}
	oldFetch := fetchAppleSubStatus
	fetchAppleSubStatus = func(_ context.Context, _, otx string) (*appleSubStatus, error) {
		if !drifted[otx] {
			return nil, errors.New("not part of this test")
		}
		return &appleSubStatus{status: 1, renewal: &appstore.RenewalInfo{
			AutoRenewStatus: appstore.AutoRenewStatus_Off, SignedDate: now.UnixMilli(),
		}}, nil
	}
	t.Cleanup(func() { fetchAppleSubStatus = oldFetch })

	seed := func(n int) {
		for i := 0; i < n; i++ {
			otx := fmt.Sprintf("HD-DRIFT-%d-%d", now.UnixNano(), len(drifted))
			sub := Subscription{
				UserID: f.buyer.ID, Provider: SubscriptionProviderApple, ProviderSubscriptionID: otx,
				ProductID: f.plan.AppleProductID, CurrentPeriodEnd: now.Unix() + 3600, AutoRenew: true,
				Environment: "Production", Status: "active",
			}
			require.NoError(t, db.Get().Create(&sub).Error)
			drifted[otx] = true
			t.Cleanup(func() { db.Get().Delete(&Subscription{}, sub.ID) })
		}
	}

	seed(appleWebhookDriftAlertMin - 1)
	require.NoError(t, handleSubscriptionReconcileTask(context.Background(), nil))
	assert.Empty(t, alerts, "低于阈值不告警")

	seed(appleWebhookDriftAlertMin)
	require.NoError(t, handleSubscriptionReconcileTask(context.Background(), nil))
	assert.Equal(t, []string{"APPLE-WEBHOOK-SILENT"}, alerts)
}

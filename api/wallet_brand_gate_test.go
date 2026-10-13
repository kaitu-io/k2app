package center

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
	"gorm.io/gorm"
)

// 钱包对没有钱包的品牌（overleap）关闭（spec 2026-10-07 A 期 §5）。

func respCode(t *testing.T, w *httptest.ResponseRecorder) int {
	t.Helper()
	var body struct {
		Code int `json:"code"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body), w.Body.String())
	return body.Code
}

// #26 结构守卫：用生产路由枚举全部 /api/wallet* 路由，overleap 用户逐个请求都被拒。
// 以后新增钱包路由忘了挂 WalletRequired()，这里直接红。
func TestWalletRoutes_AllGatedForBrandsWithoutWallet(t *testing.T) {
	skipIfNoConfig(t)
	require.NoError(t, Migrate())
	testInitConfig()
	gin.SetMode(gin.TestMode)
	r := SetupRouter()

	_, olKey := createBrandIsoAccessKeyUser(t, BrandOverleap, false)
	_, ktKey := createBrandIsoAccessKeyUser(t, BrandKaitu, false)

	call := func(method, path, host, key string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewReader([]byte(`{}`)))
		req.Host = host
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Access-Key", key)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	n := 0
	for _, rt := range r.Routes() {
		if !strings.HasPrefix(rt.Path, "/api/wallet") {
			continue
		}
		n++
		path := strings.ReplaceAll(rt.Path, ":id", "1")
		w := call(rt.Method, path, "overleap.io", olKey)
		assert.Equal(t, int(ErrorNotSupported), respCode(t, w), "%s %s must reject overleap", rt.Method, rt.Path)
	}
	assert.GreaterOrEqual(t, n, 8, "wallet route enumeration found too few routes — router shape changed?")

	// 正向对照：门不能把有钱包的品牌也拦掉
	w := call(http.MethodGet, "/api/wallet", "kaitu.io", ktKey)
	assert.NotEqual(t, int(ErrorNotSupported), respCode(t, w), w.Body.String())
}

func TestWalletRequired_NoUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/x", WalletRequired(), func(c *gin.Context) { c.String(200, "reached") })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
	assert.Equal(t, int(ErrorNotLogin), respCode(t, w))
}

func createGateTestOrder(t *testing.T, userID uint64) *Order {
	t.Helper()
	now := time.Now()
	o := &Order{UUID: generateId("ord-gate"), Title: "gate test", UserID: userID, PayAmount: 7900, OriginAmount: 7900,
		IsPaid: BoolPtr(true), PaidAt: &now, Channel: OrderChannelAppleIAP, Meta: "{}"}
	require.NoError(t, db.Get().Create(o).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(o) })
	return o
}

// #27 后台退款进钱包：执行点与 HTTP 预校验都拒 overleap，不写任何东西
func TestOrderRefund_RejectsBrandWithoutWallet(t *testing.T) {
	skipIfNoConfig(t)
	require.NoError(t, Migrate())
	ctx := context.Background()

	u := createStripeTestUser(t, BrandOverleap)
	o := createGateTestOrder(t, u.ID)
	t.Cleanup(func() { db.Get().Unscoped().Where("user_id = ?", u.ID).Delete(&Wallet{}) })

	t.Run("executor (also covers an already-pending approval)", func(t *testing.T) {
		err := ProcessOrderRefund(ctx, o.ID, "test", 1)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "没有钱包")
		var got Order
		require.NoError(t, db.Get().First(&got, o.ID).Error)
		assert.False(t, got.IsRefunded != nil && *got.IsRefunded)
		var wallets int64
		db.Get().Model(&Wallet{}).Where("user_id = ?", u.ID).Count(&wallets)
		assert.Zero(t, wallets)
	})

	t.Run("admin endpoint rejects before creating an approval", func(t *testing.T) {
		var before int64
		db.Get().Model(&AdminApproval{}).Count(&before)
		gin.SetMode(gin.TestMode)
		r := gin.New()
		r.POST("/app/orders/:uuid/refund", api_admin_refund_order)
		body, _ := json.Marshal(map[string]string{"reason": "valid test reason"})
		req := httptest.NewRequest(http.MethodPost, "/app/orders/"+o.UUID+"/refund", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, int(ErrorNotSupported), respCode(t, w), w.Body.String())
		var after int64
		db.Get().Model(&AdminApproval{}).Count(&after)
		assert.Equal(t, before, after)
	})
}

// #28 分销返现：收款人品牌没有钱包 → 不入账
func TestCashback_SkipsPayeeWithoutWallet(t *testing.T) {
	skipIfNoConfig(t)
	require.NoError(t, Migrate())
	installStripeFakes(t) // 截获告警
	ctx := context.Background()

	ol := createStripeTestUser(t, BrandOverleap)
	kt := createStripeTestUser(t, BrandKaitu)
	t.Cleanup(func() {
		for _, id := range []uint64{ol.ID, kt.ID} {
			var w Wallet
			if db.Get().Where("user_id = ?", id).First(&w).Error == nil {
				db.Get().Unscoped().Where("wallet_id = ?", w.ID).Delete(&WalletChange{})
				db.Get().Unscoped().Delete(&w)
			}
		}
	})
	o := createGateTestOrder(t, kt.ID)

	require.NoError(t, db.Get().Transaction(func(tx *gorm.DB) error {
		return addCashbackIncomeInTx(ctx, tx, ol.ID, o.ID, 500, 0, "test")
	}))
	var n int64
	db.Get().Model(&Wallet{}).Where("user_id = ?", ol.ID).Count(&n)
	assert.Zero(t, n, "no wallet may be created for a brand without wallet")

	require.NoError(t, db.Get().Transaction(func(tx *gorm.DB) error {
		return addCashbackIncomeInTx(ctx, tx, kt.ID, o.ID, 500, 0, "test")
	}))
	db.Get().Model(&Wallet{}).Where("user_id = ?", kt.ID).Count(&n)
	assert.Equal(t, int64(1), n, "kaitu payee still gets cashback")
}

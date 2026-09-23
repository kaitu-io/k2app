package center

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

func payRedirectRouter() *gin.Engine {
	r := gin.New()
	r.GET("/api/orders/:uuid/pay", api_order_pay_redirect)
	return r
}

// newUnpaidNextpayOrder 建一张未付的 nextpay 渠道订单（Meta 含 plan），测试结束硬删。
func newUnpaidNextpayOrder(t *testing.T, user *User, plan *Plan) *Order {
	t.Helper()
	o := &Order{UUID: "po-" + plan.PID, Title: "一年 x 1", OriginAmount: plan.Price, PayAmount: plan.Price,
		UserID: user.ID, IsPaid: BoolPtr(false), Channel: OrderChannelNextpay}
	require.NoError(t, o.SetOrderMeta(plan, nil, nil, true))
	require.NoError(t, db.Get().Create(o).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(o) })
	return o
}

func get302(t *testing.T, path string) string {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	payRedirectRouter().ServeHTTP(w, req)
	require.Equal(t, http.StatusFound, w.Code, w.Body.String())
	return w.Header().Get("Location")
}

func TestPayRedirect_FreshCache_Reused(t *testing.T) {
	skipIfNoDB(t)
	require.NoError(t, Migrate())
	gin.SetMode(gin.TestMode)
	setNextpayReady(t)
	plan := nextpayTestPlan(t)
	user := CreateTestUser(t)
	o := newUnpaidNextpayOrder(t, user, plan)
	require.NoError(t, o.SetOrderCheckout(payRedirectURL(o.UUID), "https://checkout.stripe.com/c/pay/cached", time.Now().Unix()-3600))
	require.NoError(t, db.Get().Save(o).Error)

	orig := createNextpayCheckoutFn
	t.Cleanup(func() { createNextpayCheckoutFn = orig })
	createNextpayCheckoutFn = func(context.Context, *User, *Order, *Plan) (*nextpayCheckout, error) {
		t.Error("缓存 1h 前的 session 必须复用，不得重建")
		return nil, nil
	}

	assert.Equal(t, "https://checkout.stripe.com/c/pay/cached", get302(t, "/api/orders/"+o.UUID+"/pay"))
}

func TestPayRedirect_StaleCache_Rebuilt(t *testing.T) {
	skipIfNoDB(t)
	require.NoError(t, Migrate())
	gin.SetMode(gin.TestMode)
	setNextpayReady(t)
	plan := nextpayTestPlan(t)
	user := CreateTestUser(t)
	o := newUnpaidNextpayOrder(t, user, plan)
	require.NoError(t, o.SetOrderCheckout(payRedirectURL(o.UUID), "https://checkout.stripe.com/c/pay/old", time.Now().Add(-25*time.Hour).Unix()))
	require.NoError(t, db.Get().Save(o).Error)

	orig := createNextpayCheckoutFn
	t.Cleanup(func() { createNextpayCheckoutFn = orig })
	calls := 0
	createNextpayCheckoutFn = func(context.Context, *User, *Order, *Plan) (*nextpayCheckout, error) {
		calls++
		return &nextpayCheckout{NextpayOrderID: "np-2", CheckoutURL: "https://checkout.stripe.com/c/pay/new"}, nil
	}

	assert.Equal(t, "https://checkout.stripe.com/c/pay/new", get302(t, "/api/orders/"+o.UUID+"/pay"))
	assert.Equal(t, 1, calls)
	var saved Order
	require.NoError(t, db.Get().Where("uuid = ?", o.UUID).First(&saved).Error)
	assert.Equal(t, "np-2", saved.NextpayOrderID)
}

// 重建 checkout 期间（NextPay 往返里）webhook 并发把这张单入账：落库绝不能把 is_paid
// 洗回 false（否则第二个 session 付款会二次入账且不触发 DOUBLE-PAY 告警），且应回 pay-result。
func TestPayRedirect_PaidDuringRebuild_DoesNotUnpay(t *testing.T) {
	skipIfNoDB(t)
	require.NoError(t, Migrate())
	gin.SetMode(gin.TestMode)
	setNextpayReady(t)
	plan := nextpayTestPlan(t)
	user := CreateTestUser(t)
	o := newUnpaidNextpayOrder(t, user, plan)
	require.NoError(t, o.SetOrderCheckout(payRedirectURL(o.UUID), "https://checkout.stripe.com/c/pay/old", time.Now().Add(-25*time.Hour).Unix()))
	require.NoError(t, db.Get().Save(o).Error)

	orig := createNextpayCheckoutFn
	t.Cleanup(func() { createNextpayCheckoutFn = orig })
	createNextpayCheckoutFn = func(context.Context, *User, *Order, *Plan) (*nextpayCheckout, error) {
		// 模拟并发 webhook：另一条连接提交 is_paid=true。
		now := time.Now()
		require.NoError(t, db.Get().Model(&Order{}).Where("id = ?", o.ID).
			Updates(map[string]any{"is_paid": true, "paid_at": now, "nextpay_order_id": "np-paid"}).Error)
		return &nextpayCheckout{NextpayOrderID: "np-2", CheckoutURL: "https://checkout.stripe.com/c/pay/new"}, nil
	}

	assert.Equal(t, payResultURL(kaituSiteLocale(user.Language), o.UUID), get302(t, "/api/orders/"+o.UUID+"/pay"))
	var saved Order
	require.NoError(t, db.Get().Where("uuid = ?", o.UUID).First(&saved).Error)
	require.NotNil(t, saved.IsPaid)
	assert.True(t, *saved.IsPaid, "重建落库把已付订单洗回未付")
	assert.NotNil(t, saved.PaidAt)
	assert.Equal(t, "np-paid", saved.NextpayOrderID, "已付订单的 nextpay_order_id 不得被新 session 覆盖")
}

func TestPayRedirect_Paid_GoesToPayResult(t *testing.T) {
	skipIfNoDB(t)
	require.NoError(t, Migrate())
	gin.SetMode(gin.TestMode)
	setNextpayReady(t)
	plan := nextpayTestPlan(t)
	user := CreateTestUser(t)
	o := newUnpaidNextpayOrder(t, user, plan)
	require.NoError(t, db.Get().Model(o).Update("is_paid", true).Error)

	assert.Equal(t, payResultURL(kaituSiteLocale(user.Language), o.UUID), get302(t, "/api/orders/"+o.UUID+"/pay"))
}

func TestPayRedirect_Unknown_GoesToPurchase(t *testing.T) {
	skipIfNoDB(t)
	require.NoError(t, Migrate())
	gin.SetMode(gin.TestMode)
	setNextpayReady(t)
	assert.Equal(t, BrandKaitu.Config().BaseURL+"/zh-CN/purchase", get302(t, "/api/orders/no-such-order/pay"))
}

func TestPayRedirect_OtherBrand_GoesToPurchase(t *testing.T) {
	skipIfNoDB(t)
	require.NoError(t, Migrate())
	gin.SetMode(gin.TestMode)
	setNextpayReady(t)
	plan := nextpayTestPlan(t)
	user := CreateTestUser(t)
	require.NoError(t, db.Get().Model(user).Update("brand", string(BrandOverleap)).Error)
	o := newUnpaidNextpayOrder(t, user, plan)

	orig := createNextpayCheckoutFn
	t.Cleanup(func() { createNextpayCheckoutFn = orig })
	createNextpayCheckoutFn = func(context.Context, *User, *Order, *Plan) (*nextpayCheckout, error) {
		t.Error("非 kaitu 品牌的订单绝不能去 NextPay 建单")
		return nil, nil
	}

	assert.Equal(t, BrandKaitu.Config().BaseURL+"/zh-CN/purchase", get302(t, "/api/orders/"+o.UUID+"/pay"))
}

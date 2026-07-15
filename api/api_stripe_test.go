package center

import (
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
	stripe "github.com/stripe/stripe-go/v82"
	db "github.com/wordgate/qtoolkit/db"
)

// ginStripeCtx 构造带 authContext 的 gin.Context（模式取自 brand_isolation_e2e_test.go
// 的 ginCtxWithAuthAndHost；本文件独立复制以免跨文件测试耦合）。
func ginStripeCtx(body string, user *User) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/user/stripe/checkout", strings.NewReader(body))
	c.Request.Host = "www.overleap.io"
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("authContext", &authContext{UserID: user.ID, User: user})
	return c, w
}

func decodeCode(t *testing.T, w *httptest.ResponseRecorder) (code int, data map[string]any) {
	t.Helper()
	var resp struct {
		Code int            `json:"code"`
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return resp.Code, resp.Data
}

// withStripeTestEmail 给测试用户挂一个 email 登录身份（checkout 预填邮箱走它）。
func withStripeTestEmail(t *testing.T, u *User, email string) {
	t.Helper()
	enc, err := secretEncryptString(context.Background(), email)
	require.NoError(t, err)
	li := &LoginIdentify{UserID: u.ID, Type: "email", IndexID: "stripetest-" + stripeUniq(),
		EncryptedValue: enc, Brand: u.Brand}
	require.NoError(t, db.Get().Create(li).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(li) })
}

func TestStripeCheckoutHandler(t *testing.T) {
	skipIfNoConfig(t)
	require.NoError(t, Migrate())

	stubSession := func(t *testing.T, capture **stripe.CheckoutSessionParams, captureKey *string) {
		t.Helper()
		orig := stripeNewCheckoutSession
		t.Cleanup(func() { stripeNewCheckoutSession = orig })
		stripeNewCheckoutSession = func(key string, params *stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error) {
			if capture != nil {
				*capture = params
			}
			if captureKey != nil {
				*captureKey = key
			}
			return &stripe.CheckoutSession{URL: "https://checkout.stripe.com/c/pay/cs_test_123"}, nil
		}
	}

	t.Run("KaituUser_Rejected405001", func(t *testing.T) {
		setStripeTestConfig(t, "sk_test_x", "whsec_x")
		u := createStripeTestUser(t, BrandKaitu)
		p := createStripeTestPlan(t)
		c, w := ginStripeCtx(`{"plan":"`+p.PID+`"}`, u)
		api_stripe_checkout(c)
		code, _ := decodeCode(t, w)
		assert.Equal(t, int(ErrorPaymentChannelUnavailable), code)
	})

	t.Run("ConfigMissing_Rejected405001", func(t *testing.T) {
		setStripeTestConfig(t, "", "")
		u := createStripeTestUser(t, BrandOverleap)
		p := createStripeTestPlan(t)
		c, w := ginStripeCtx(`{"plan":"`+p.PID+`"}`, u)
		api_stripe_checkout(c)
		code, _ := decodeCode(t, w)
		assert.Equal(t, int(ErrorPaymentChannelUnavailable), code)
	})

	t.Run("PlanWithoutStripePrice_Rejected", func(t *testing.T) {
		setStripeTestConfig(t, "sk_test_x", "whsec_x")
		u := createStripeTestUser(t, BrandOverleap)
		p := createStripeTestPlan(t)
		require.NoError(t, db.Get().Model(p).Update("stripe_price_id", "").Error)
		c, w := ginStripeCtx(`{"plan":"`+p.PID+`"}`, u)
		api_stripe_checkout(c)
		code, _ := decodeCode(t, w)
		assert.Equal(t, int(ErrorInvalidArgument), code)
	})

	t.Run("CrossBrandPlanPID_Rejected", func(t *testing.T) {
		setStripeTestConfig(t, "sk_test_x", "whsec_x")
		u := createStripeTestUser(t, BrandOverleap)
		kp := &Plan{PID: "ktst_" + stripeUniq(), Label: "kaitu plan", Price: 999, OriginPrice: 999,
			Month: 1, Tier: TierBasic, IsActive: BoolPtr(true), Brand: string(BrandKaitu),
			StripePriceID: "price_should_never_sell_" + stripeUniq()}
		require.NoError(t, db.Get().Create(kp).Error)
		t.Cleanup(func() { db.Get().Unscoped().Delete(kp) })
		c, w := ginStripeCtx(`{"plan":"`+kp.PID+`"}`, u)
		api_stripe_checkout(c)
		code, _ := decodeCode(t, w)
		assert.Equal(t, int(ErrorInvalidArgument), code) // getPlanByPID 品牌隔离 → plan 不可见
	})

	t.Run("AlreadySubscribed_Rejected409", func(t *testing.T) {
		setStripeTestConfig(t, "sk_test_x", "whsec_x")
		u := createStripeTestUser(t, BrandOverleap)
		p := createStripeTestPlan(t)
		sub := &Subscription{UserID: u.ID, Provider: "stripe",
			ProviderSubscriptionID: "sub_live_" + stripeUniq(), ProductID: p.StripePriceID,
			CurrentPeriodEnd: time.Now().Unix() + 86400, AutoRenew: true, Status: "active"}
		require.NoError(t, db.Get().Create(sub).Error)
		c, w := ginStripeCtx(`{"plan":"`+p.PID+`"}`, u)
		api_stripe_checkout(c)
		code, _ := decodeCode(t, w)
		assert.Equal(t, int(ErrorConflict), code)
	})

	t.Run("HappyPath_ReturnsURL_ParamsCorrect", func(t *testing.T) {
		setStripeTestConfig(t, "sk_test_x", "whsec_x")
		var captured *stripe.CheckoutSessionParams
		var capturedKey string
		stubSession(t, &captured, &capturedKey)
		u := createStripeTestUser(t, BrandOverleap)
		p := createStripeTestPlan(t)

		c, w := ginStripeCtx(`{"plan":"`+p.PID+`"}`, u)
		api_stripe_checkout(c)
		code, data := decodeCode(t, w)
		require.Equal(t, 0, code)
		assert.Equal(t, "https://checkout.stripe.com/c/pay/cs_test_123", data["url"])

		assert.Equal(t, "sk_test_x", capturedKey) // key 逐调用传入，不写 stripe.Key 全局
		require.NotNil(t, captured)
		assert.Equal(t, string(stripe.CheckoutSessionModeSubscription), *captured.Mode)
		require.Len(t, captured.LineItems, 1)
		assert.Equal(t, p.StripePriceID, *captured.LineItems[0].Price)
		assert.Equal(t, u.UUID, *captured.ClientReferenceID)
		require.NotNil(t, captured.SubscriptionData)
		assert.Equal(t, u.UUID, captured.SubscriptionData.Metadata["user_uuid"])
		assert.Equal(t, p.PID, captured.SubscriptionData.Metadata["plan_pid"])
		assert.Equal(t, "overleap", captured.SubscriptionData.Metadata["brand"])
	})

	// 到期后重新订阅：必须复用既有 Customer，否则 Stripe 侧生成第二个 Customer
	// → 账单历史割裂，portal 只看得到最新那个。
	t.Run("ReturningCustomer_ReusesCustomerID_NotEmail", func(t *testing.T) {
		setStripeTestConfig(t, "sk_test_x", "whsec_x")
		var captured *stripe.CheckoutSessionParams
		stubSession(t, &captured, nil)
		u := createStripeTestUser(t, BrandOverleap)
		p := createStripeTestPlan(t)
		withStripeTestEmail(t, u, "returning_"+stripeUniq()+"@overleap.io")

		// 上一段订阅：已过期（否则撞防重叠门），但 customer id 仍在
		custID := "cus_returning_" + stripeUniq()
		old := &Subscription{UserID: u.ID, Provider: "stripe",
			ProviderSubscriptionID: "sub_old_" + stripeUniq(), ProviderCustomerID: custID,
			ProductID: p.StripePriceID, CurrentPeriodEnd: time.Now().Unix() - 86400,
			AutoRenew: false, Status: "expired"}
		require.NoError(t, db.Get().Create(old).Error)

		c, w := ginStripeCtx(`{"plan":"`+p.PID+`"}`, u)
		api_stripe_checkout(c)
		code, _ := decodeCode(t, w)
		require.Equal(t, 0, code)

		require.NotNil(t, captured)
		require.NotNil(t, captured.Customer)
		assert.Equal(t, custID, *captured.Customer)
		assert.Nil(t, captured.CustomerEmail) // 与 Customer 互斥，同传 Stripe 报 400
	})

	t.Run("FirstTimeCustomer_NoCustomerID_PrefillsEmail", func(t *testing.T) {
		setStripeTestConfig(t, "sk_test_x", "whsec_x")
		var captured *stripe.CheckoutSessionParams
		stubSession(t, &captured, nil)
		u := createStripeTestUser(t, BrandOverleap)
		p := createStripeTestPlan(t)
		email := "firsttime_" + stripeUniq() + "@overleap.io"
		withStripeTestEmail(t, u, email)

		c, w := ginStripeCtx(`{"plan":"`+p.PID+`"}`, u)
		api_stripe_checkout(c)
		code, _ := decodeCode(t, w)
		require.Equal(t, 0, code)

		require.NotNil(t, captured)
		assert.Nil(t, captured.Customer) // 无存量 customer → 让 Stripe 新建
		require.NotNil(t, captured.CustomerEmail)
		assert.Equal(t, email, *captured.CustomerEmail)
	})
}

func TestStripePortalHandler(t *testing.T) {
	skipIfNoConfig(t)
	require.NoError(t, Migrate())
	setStripeTestConfig(t, "sk_test_x", "whsec_x")

	orig := stripeNewPortalSession
	t.Cleanup(func() { stripeNewPortalSession = orig })
	var capturedCustomer, capturedKey string
	stripeNewPortalSession = func(key string, params *stripe.BillingPortalSessionParams) (*stripe.BillingPortalSession, error) {
		capturedCustomer = *params.Customer
		capturedKey = key
		return &stripe.BillingPortalSession{URL: "https://billing.stripe.com/p/session/test_123"}, nil
	}

	t.Run("NoStripeSubscription_404", func(t *testing.T) {
		u := createStripeTestUser(t, BrandOverleap)
		c, w := ginStripeCtx(`{}`, u)
		api_stripe_portal(c)
		code, _ := decodeCode(t, w)
		assert.Equal(t, int(ErrorNotFound), code)
	})

	t.Run("HappyPath", func(t *testing.T) {
		u := createStripeTestUser(t, BrandOverleap)
		sub := &Subscription{UserID: u.ID, Provider: "stripe",
			ProviderSubscriptionID: "sub_p_" + stripeUniq(), ProviderCustomerID: "cus_p_" + stripeUniq(),
			ProductID: "price_x", CurrentPeriodEnd: time.Now().Unix() + 86400, AutoRenew: true, Status: "active"}
		require.NoError(t, db.Get().Create(sub).Error)

		c, w := ginStripeCtx(`{}`, u)
		api_stripe_portal(c)
		code, data := decodeCode(t, w)
		require.Equal(t, 0, code)
		assert.Equal(t, "https://billing.stripe.com/p/session/test_123", data["url"])
		assert.Equal(t, sub.ProviderCustomerID, capturedCustomer)
		assert.Equal(t, "sk_test_x", capturedKey)
	})
}

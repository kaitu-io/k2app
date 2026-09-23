package center

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// nextpayTestPlan 建一个 kaitu 基础套餐，测试结束硬删。
func nextpayTestPlan(t *testing.T) *Plan {
	t.Helper()
	plan := &Plan{PID: fmt.Sprintf("np%d", time.Now().UnixNano()), Label: "一年", Price: 4990, OriginPrice: 4990,
		Month: 12, Tier: "basic", IsActive: BoolPtr(true), Brand: string(BrandKaitu)}
	require.NoError(t, db.Get().Create(plan).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(plan) })
	return plan
}

// setNextpayReady 让 configNextpay().Ready() 为真，结束后还原。
func setNextpayReady(t *testing.T) {
	t.Helper()
	for _, k := range []string{"nextpay.access_key", "nextpay.webhook_secret"} {
		orig := viper.Get(k)
		t.Cleanup(func() { viper.Set(k, orig) })
	}
	viper.Set("nextpay.access_key", "test-ak")
	viper.Set("nextpay.webhook_secret", "test-ws")
}

func nextpayOrderRouter(user *User) *gin.Engine {
	r := gin.New()
	r.POST("/api/orders", func(c *gin.Context) {
		c.Set("authContext", &authContext{UserID: user.ID, User: user})
	}, api_create_order)
	return r
}

func postCreateOrder(t *testing.T, user *User, planPID string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"plan": planPID, "preview": false})
	req, _ := http.NewRequest(http.MethodPost, "/api/orders", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	nextpayOrderRouter(user).ServeHTTP(w, req)
	return w
}

func TestCreateOrder_Nextpay_ReturnsStripeURLAndPersists(t *testing.T) {
	skipIfNoDB(t)
	require.NoError(t, Migrate())
	gin.SetMode(gin.TestMode)
	setNextpayReady(t)
	plan := nextpayTestPlan(t)
	user := CreateTestUser(t)

	orig := createNextpayCheckoutFn
	t.Cleanup(func() { createNextpayCheckoutFn = orig })
	var gotOrder *Order
	createNextpayCheckoutFn = func(ctx context.Context, u *User, o *Order, p *Plan) (*nextpayCheckout, error) {
		gotOrder = o
		assert.Equal(t, user.ID, u.ID)
		assert.Equal(t, plan.PID, p.PID)
		return &nextpayCheckout{NextpayOrderID: "np-ord-1", CheckoutURL: "https://checkout.stripe.com/c/pay/cs_1"}, nil
	}

	w := postCreateOrder(t, user, plan.PID)

	var resp struct {
		Code int `json:"code"`
		Data struct {
			PayUrl string `json:"payUrl"`
			Order  struct {
				UUID string `json:"uuid"`
			} `json:"order"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code, w.Body.String())
	assert.Equal(t, "https://checkout.stripe.com/c/pay/cs_1", resp.Data.PayUrl, "payUrl 必须直达 Stripe")
	require.NotNil(t, gotOrder)

	var saved Order
	require.NoError(t, db.Get().Where("uuid = ?", resp.Data.Order.UUID).First(&saved).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&saved) })
	assert.Equal(t, OrderChannelNextpay, saved.Channel)
	assert.Equal(t, "np-ord-1", saved.NextpayOrderID)
	assert.Equal(t, payRedirectURL(saved.UUID), saved.GetPayUrl(), "Meta.payUrl 是耐久链接（代付邮件用）")
	url, at := saved.GetCheckout()
	assert.Equal(t, "https://checkout.stripe.com/c/pay/cs_1", url)
	assert.InDelta(t, time.Now().Unix(), at, 5)
}

func TestCreateOrder_Nextpay_CheckoutFailure_SystemError(t *testing.T) {
	skipIfNoDB(t)
	require.NoError(t, Migrate())
	gin.SetMode(gin.TestMode)
	setNextpayReady(t)
	plan := nextpayTestPlan(t)
	user := CreateTestUser(t)
	t.Cleanup(func() { db.Get().Unscoped().Where("user_id = ?", user.ID).Delete(&Order{}) })

	orig := createNextpayCheckoutFn
	t.Cleanup(func() { createNextpayCheckoutFn = orig })
	createNextpayCheckoutFn = func(context.Context, *User, *Order, *Plan) (*nextpayCheckout, error) {
		return nil, errors.New("nextpay down")
	}

	w := postCreateOrder(t, user, plan.PID)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, float64(ErrorSystemError), resp["code"])
}

func TestCreateOrder_Nextpay_NotReady_405001(t *testing.T) {
	skipIfNoDB(t)
	require.NoError(t, Migrate())
	gin.SetMode(gin.TestMode)
	for _, k := range []string{"nextpay.access_key", "nextpay.webhook_secret"} {
		orig := viper.Get(k)
		t.Cleanup(func() { viper.Set(k, orig) })
		viper.Set(k, "")
	}
	plan := nextpayTestPlan(t)
	user := CreateTestUser(t)

	w := postCreateOrder(t, user, plan.PID)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, float64(ErrorPaymentChannelUnavailable), resp["code"])
}

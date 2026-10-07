package center

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	stripe "github.com/stripe/stripe-go/v82"
	db "github.com/wordgate/qtoolkit/db"
)

// #29 审批注册与显示名：漏注册 = SubmitApproval 直接报错，整个后台操作不可用。
func TestStripeWithdrawalApprovalRegistered(t *testing.T) {
	registerApprovalCallbacks()
	_, ok := getApprovalCallback("stripe_withdrawal")
	assert.True(t, ok)
	assert.Equal(t, "Stripe 撤回退款", actionDisplayName("stripe_withdrawal"))
}

func withdrawalAdminRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(injectSuperAdmin(999, "superadmin-withdraw-test"))
	r.GET("/app/users/:uuid/stripe-withdrawal", api_admin_stripe_withdrawal_quote)
	r.POST("/app/users/:uuid/stripe-withdrawal", api_admin_stripe_withdrawal)
	r.POST("/app/stripe-withdrawals/:request_id/abandon", api_admin_stripe_withdrawal_abandon)
	return r
}

type withdrawalResp struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func doWithdrawalAdmin(t *testing.T, r *gin.Engine, method, path string, body any) withdrawalResp {
	t.Helper()
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rd)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var resp withdrawalResp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), w.Body.String())
	return resp
}

func TestAdminStripeWithdrawalEndpoints(t *testing.T) {
	skipIfNoConfig(t)
	require.NoError(t, Migrate())
	setStripeTestConfig(t, "sk_test_x", stripeTestWebhookSecret)
	registerApprovalCallbacks()
	r := withdrawalAdminRouter()
	t.Cleanup(func() {
		db.Get().Unscoped().Where("requestor_uuid = ?", "superadmin-withdraw-test").Delete(&AdminApproval{})
	})

	t.Run("QuoteIsReadOnly", func(t *testing.T) {
		f, w := installStripeFakes(t), installWithdrawFakes(t)
		now := time.Now().Unix()
		fx := seedWithdrawFixture(t, f, w, now-2*tDay, 365, 7900, stripe.InvoiceBillingReasonSubscriptionCreate, true)
		resp := doWithdrawalAdmin(t, r, http.MethodGet, fmt.Sprintf("/app/users/%s/stripe-withdrawal?notice_at=%d", fx.u.UUID, now-60), nil)
		require.Equal(t, 0, resp.Code, resp.Message)
		var plan withdrawalPlan
		require.NoError(t, json.Unmarshal(resp.Data, &plan))
		require.Len(t, plan.Items, 1)
		assert.Equal(t, int64(7900*363/365), plan.Items[0].Amount)
		assert.Empty(t, withdrawRows(t, fx.u.ID))
		assert.Equal(t, 0, w.createCount())
	})

	t.Run("QuoteIneligibleShowsDetails", func(t *testing.T) {
		f, w := installStripeFakes(t), installWithdrawFakes(t)
		now := time.Now().Unix()
		fx := seedWithdrawFixture(t, f, w, now-2*tDay, 30, 999, stripe.InvoiceBillingReasonSubscriptionCycle, true)
		resp := doWithdrawalAdmin(t, r, http.MethodGet, fmt.Sprintf("/app/users/%s/stripe-withdrawal?notice_at=%d", fx.u.UUID, now-60), nil)
		assert.Equal(t, int(ErrorInvalidOperation), resp.Code)
		assert.Contains(t, resp.Message, "monthly renewal")
	})

	t.Run("ExecuteThenResumeByRequestID", func(t *testing.T) {
		f, w := installStripeFakes(t), installWithdrawFakes(t)
		now := time.Now().Unix()
		fx := seedWithdrawFixture(t, f, w, now-2*tDay, 365, 7900, stripe.InvoiceBillingReasonSubscriptionCreate, true)
		f.cancelErr = errors.New("stripe down")
		path := "/app/users/" + fx.u.UUID + "/stripe-withdrawal"
		resp := doWithdrawalAdmin(t, r, http.MethodPost, path, map[string]any{"noticeAt": now - 60, "mode": "withdrawal", "reason": "user email"})
		require.NotEqual(t, 0, resp.Code, "first attempt fails at cancel")
		reqID := withdrawRows(t, fx.u.ID)[0].RequestID

		// 续跑只靠 request_id：参数故意与原请求不同（客服重填了时间），不能被当成新请求拒掉
		resp = doWithdrawalAdmin(t, r, http.MethodPost, path, map[string]any{"noticeAt": now - 999, "mode": "withdrawal", "requestId": reqID})
		require.Equal(t, 0, resp.Code, resp.Message)
		var res withdrawalResult
		require.NoError(t, json.Unmarshal(resp.Data, &res))
		assert.Equal(t, reqID, res.RequestID)
		require.Len(t, res.Rows, 1)
		assert.Equal(t, withdrawStatusDone, res.Rows[0].Status)
		assert.Equal(t, 1, w.createCount())
		var n int64
		db.Get().Model(&AdminApproval{}).Where("requestor_uuid = ? AND action = ?", "superadmin-withdraw-test", "stripe_withdrawal").Count(&n)
		assert.GreaterOrEqual(t, n, int64(1))
	})

	t.Run("BadParamsRejectedBeforeApproval", func(t *testing.T) {
		f, w := installStripeFakes(t), installWithdrawFakes(t)
		now := time.Now().Unix()
		fx := seedWithdrawFixture(t, f, w, now-2*tDay, 365, 7900, stripe.InvoiceBillingReasonSubscriptionCreate, true)
		var before int64
		db.Get().Model(&AdminApproval{}).Count(&before)
		resp := doWithdrawalAdmin(t, r, http.MethodPost, "/app/users/"+fx.u.UUID+"/stripe-withdrawal",
			map[string]any{"noticeAt": now + 3600, "mode": "withdrawal"})
		assert.Equal(t, int(ErrorInvalidArgument), resp.Code)
		var after int64
		db.Get().Model(&AdminApproval{}).Count(&after)
		assert.Equal(t, before, after)
	})

	t.Run("Abandon", func(t *testing.T) {
		f, w := installStripeFakes(t), installWithdrawFakes(t)
		now := time.Now().Unix()
		fx := seedWithdrawFixture(t, f, w, now-2*tDay, 365, 7900, stripe.InvoiceBillingReasonSubscriptionCreate, true)
		w.findErr = errors.New("stuck")
		_ = doWithdrawalAdmin(t, r, http.MethodPost, "/app/users/"+fx.u.UUID+"/stripe-withdrawal", map[string]any{"noticeAt": now - 60, "mode": "withdrawal"})
		reqID := withdrawRows(t, fx.u.ID)[0].RequestID
		w.findErr = nil
		resp := doWithdrawalAdmin(t, r, http.MethodPost, "/app/stripe-withdrawals/"+reqID+"/abandon", map[string]any{"reason": "stuck in test"})
		require.Equal(t, 0, resp.Code, resp.Message)
		assert.Equal(t, withdrawStatusAbandoned, withdrawRows(t, fx.u.ID)[0].Status)
	})

	t.Run("KaituUserRejected", func(t *testing.T) {
		u := createStripeTestUser(t, BrandKaitu)
		resp := doWithdrawalAdmin(t, r, http.MethodGet, fmt.Sprintf("/app/users/%s/stripe-withdrawal?notice_at=%d", u.UUID, time.Now().Unix()-60), nil)
		assert.Equal(t, int(ErrorNotSupported), resp.Code)
	})
}

// 预览在 staff 组（客服可报价），执行 / 作废只给超管——经生产路由验证。
func TestStripeWithdrawalRouteRoles(t *testing.T) {
	skipIfNoConfig(t)
	require.NoError(t, Migrate())
	testInitConfig()
	gin.SetMode(gin.TestMode)
	r := SetupRouter()

	support, key := createBrandIsoAccessKeyUser(t, BrandKaitu, false)
	require.NoError(t, db.Get().Model(&User{}).Where("id = ?", support.ID).Update("roles", RoleUser|RoleSupport).Error)
	target := createStripeTestUser(t, BrandOverleap)

	call := func(method, path string) int {
		req := httptest.NewRequest(method, path, bytes.NewReader([]byte(`{"noticeAt":1,"mode":"withdrawal","reason":"xx"}`)))
		req.Host = "kaitu.io"
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Access-Key", key)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		var body struct {
			Code int `json:"code"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body), w.Body.String())
		return body.Code
	}
	quote := call(http.MethodGet, "/app/users/"+target.UUID+"/stripe-withdrawal?notice_at=1")
	assert.NotEqual(t, int(ErrorForbidden), quote, "support can quote")
	assert.NotEqual(t, int(ErrorNotLogin), quote)
	assert.Equal(t, int(ErrorForbidden), call(http.MethodPost, "/app/users/"+target.UUID+"/stripe-withdrawal"), "support cannot execute")
	assert.Equal(t, int(ErrorForbidden), call(http.MethodPost, "/app/stripe-withdrawals/wdr_x/abandon"), "support cannot abandon")
}

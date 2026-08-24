package center

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// TestCreateOrder_RejectsForUserUUIDsField verifies that POST /api/orders rejects
// requests carrying the deprecated `forUserUUIDs` field with code 422002. The
// rejection runs before any auth/db access, so no fixtures are required.
//
// Spec: docs/superpowers/specs/2026-04-20-proxy-purchase-users.md (Task 5).
func TestCreateOrder_RejectsForUserUUIDsField(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/orders", api_create_order)

	body := map[string]any{
		"plan":         "pro_month",
		"forUserUUIDs": []string{"uuid1", "uuid2"},
	}
	bodyBytes, err := json.Marshal(body)
	require.NoError(t, err)

	req, _ := http.NewRequest(http.MethodPost, "/api/orders", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "HTTP code 总是 200，错误在 JSON code 字段")
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, float64(422002), resp["code"], "should return PROXY_PURCHASE_DEPRECATED")
	msg, _ := resp["message"].(string)
	assert.Contains(t, msg, "代付款", "error message should mention 代付款")
}

// TestCreateOrder_RejectsForMyselfFalse verifies that an explicit `forMyself=false`
// is also treated as a deprecated proxy-purchase request.
func TestCreateOrder_RejectsForMyselfFalse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/orders", api_create_order)

	forMyselfFalse := false
	body := map[string]any{
		"plan":      "pro_month",
		"forMyself": &forMyselfFalse,
	}
	bodyBytes, err := json.Marshal(body)
	require.NoError(t, err)

	req, _ := http.NewRequest(http.MethodPost, "/api/orders", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, float64(422002), resp["code"])
}

// TestCreateOrder_OldClientForUsersField_ReturnsFriendlyError simulates a
// pre-tier-rename webapp/web client that still sends the legacy `forUsers`
// field (new field is `forUserUUIDs`). Without a `forUsers` alias on
// CreateOrderRequest, the JSON decoder would silently drop the unknown field
// and the order would slip through — so we keep a `ForUsers` alias on the
// struct and extend the rejection check to cover both names. This test locks
// in that contract.
//
// Spec: docs/superpowers/specs/2026-04-20-proxy-purchase-users.md (Task 21).
func TestCreateOrder_OldClientForUsersField_ReturnsFriendlyError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/orders", api_create_order)

	body := map[string]any{
		"plan":      "pro_month",
		"forMyself": true,
		"forUsers":  []string{"some-uuid"},
	}
	bodyBytes, err := json.Marshal(body)
	require.NoError(t, err)

	req, _ := http.NewRequest(http.MethodPost, "/api/orders", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "HTTP code 总是 200，错误在 JSON code 字段")
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, float64(422002), resp["code"], "legacy forUsers field must return PROXY_PURCHASE_DEPRECATED")

	msg, ok := resp["message"].(string)
	require.True(t, ok, "message should be a string")
	assert.Contains(t, msg, "代付款", "error message should mention 代付款 for support triage")
}

// TestCreateOrder_RejectsForUserUUIDsEvenWithForMyselfTrue verifies that mixing
// `forUserUUIDs` with `forMyself=true` (the legacy "buy for self + others" combo)
// is still rejected — any presence of forUserUUIDs is enough to deprecate the request.
func TestCreateOrder_RejectsForUserUUIDsEvenWithForMyselfTrue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/orders", api_create_order)

	forMyselfTrue := true
	body := map[string]any{
		"plan":         "pro_month",
		"forUserUUIDs": []string{"uuid1"},
		"forMyself":    &forMyselfTrue,
	}
	bodyBytes, err := json.Marshal(body)
	require.NoError(t, err)

	req, _ := http.NewRequest(http.MethodPost, "/api/orders", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, float64(422002), resp["code"])
}

// validatePurchase tests (Task 6): tier validation in purchase flow.

func TestValidatePurchase_FirstTimeAnyTierAllowed(t *testing.T) {
	user := &User{ID: 1, Tier: TierBasic, IsFirstOrderDone: nil}
	plan := &Plan{ID: 100, Tier: TierFamily}
	err := validatePurchase(user, plan)
	assert.NoError(t, err)
}

func TestValidatePurchase_FirstTimeIsFirstOrderDoneFalse(t *testing.T) {
	fal := false
	user := &User{ID: 1, Tier: TierBasic, IsFirstOrderDone: &fal}
	plan := &Plan{ID: 100, Tier: TierFamily}
	err := validatePurchase(user, plan)
	assert.NoError(t, err, "explicit IsFirstOrderDone=false also counts as first-time")
}

func TestValidatePurchase_SubsequentSameTierAllowed(t *testing.T) {
	tru := true
	user := &User{ID: 1, Tier: TierFamily, IsFirstOrderDone: &tru}
	plan := &Plan{ID: 100, Tier: TierFamily}
	err := validatePurchase(user, plan)
	assert.NoError(t, err)
}

func TestValidatePurchase_SubsequentDifferentTierRejected(t *testing.T) {
	tru := true
	user := &User{ID: 1, Tier: TierBasic, IsFirstOrderDone: &tru}
	plan := &Plan{ID: 100, Tier: TierFamily}
	err := validatePurchase(user, plan)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tier", "error message should mention tier")
}

// 互斥门(spec 2026-08-22):任何 provider 的活跃续订订阅存在时,WordGate 下单
// (含 preview)一律 409——与 api_stripe_checkout 的既有防双扣门同判据同错码。
func TestCreateOrder_ActiveSubscription_Rejected(t *testing.T) {
	skipIfNoDB(t)
	require.NoError(t, Migrate())
	gin.SetMode(gin.TestMode)

	uniq := time.Now().UnixNano()
	plan := &Plan{PID: fmt.Sprintf("tmux%d", uniq), Label: "X", Price: 1000, OriginPrice: 1000,
		Month: 1, Tier: "basic", IsActive: BoolPtr(true), Brand: string(BrandKaitu)}
	require.NoError(t, db.Get().Create(plan).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(plan) })

	user := CreateTestUser(t)
	sub := &Subscription{UserID: user.ID, Provider: "apple",
		ProviderSubscriptionID: fmt.Sprintf("OTXM-%d", uniq),
		Status:                 "active", CurrentPeriodEnd: time.Now().Unix() + 30*86400}
	require.NoError(t, db.Get().Create(sub).Error)
	t.Cleanup(func() { db.Get().Where("user_id = ?", user.ID).Delete(&Subscription{}) })

	r := gin.New()
	r.POST("/api/orders", func(c *gin.Context) {
		c.Set("authContext", &authContext{UserID: user.ID, User: user})
	}, api_create_order)

	for _, preview := range []bool{true, false} {
		body, _ := json.Marshal(map[string]any{"plan": plan.PID, "preview": preview})
		req, _ := http.NewRequest(http.MethodPost, "/api/orders", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		var resp map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, float64(ErrorConflict), resp["code"], "preview=%v 也必须拦截", preview)
	}
}

// 互斥门豁免(final review F1):专属节点(ProductPrivateNode)套餐与
// Subscription/User.ExpiredAt 零耦合(model_private_node.go),独立计费、不延长
// 会员期限——双付论证对它不成立。持活跃会员订阅的用户下单专属节点套餐不得被
// 409 拦截。
func TestCreateOrder_ActiveSubscription_PrivateNodeExempt(t *testing.T) {
	skipIfNoDB(t)
	require.NoError(t, Migrate())
	gin.SetMode(gin.TestMode)

	uniq := time.Now().UnixNano()
	plan := &Plan{PID: fmt.Sprintf("tpnex%d", uniq), Label: "专属节点", Price: 9900, OriginPrice: 9900,
		Month: 1, Tier: "basic", Product: ProductPrivateNode, IsActive: BoolPtr(true), Brand: string(BrandKaitu)}
	require.NoError(t, db.Get().Create(plan).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(plan) })

	user := CreateTestUser(t)
	sub := &Subscription{UserID: user.ID, Provider: "apple",
		ProviderSubscriptionID: fmt.Sprintf("OTXM-PNEX-%d", uniq),
		Status:                 "active", CurrentPeriodEnd: time.Now().Unix() + 30*86400}
	require.NoError(t, db.Get().Create(sub).Error)
	t.Cleanup(func() { db.Get().Where("user_id = ?", user.ID).Delete(&Subscription{}) })

	r := gin.New()
	r.POST("/api/orders", func(c *gin.Context) {
		c.Set("authContext", &authContext{UserID: user.ID, User: user})
	}, api_create_order)

	// preview=true: 只需确认互斥门未拦截(不是 409),预览路径不落库、不依赖支付渠道。
	body, _ := json.Marshal(map[string]any{"plan": plan.PID, "preview": true})
	req, _ := http.NewRequest(http.MethodPost, "/api/orders", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.NotEqual(t, float64(ErrorConflict), resp["code"], "专属节点套餐必须豁免互斥门")
	assert.Equal(t, float64(0), resp["code"], "预览应成功返回订单信息")
}

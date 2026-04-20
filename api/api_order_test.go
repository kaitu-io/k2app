package center

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

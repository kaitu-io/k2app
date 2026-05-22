package center

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =====================================================================
// Router Quota API Tests
// =====================================================================

// TestRouterQuotaHandlerExists is a compile-time smoke check that the handler
// is defined and has the correct gin.HandlerFunc signature.
func TestRouterQuotaHandlerExists(t *testing.T) {
	var _ gin.HandlerFunc = api_router_quota
}

// TestRouterQuota_GatedByRouterRequired verifies that a basic-tier user (no router
// entitlement) is rejected by RouterRequired before reaching the handler.
//
// Uses direct authContext injection before RouterRequired so this test exercises
// the gate without a real DB or full auth chain. The handler itself (api_router_quota)
// calls ReqUser + Quota() — those are tested separately via the DB-bound integration
// tests in Task 11.
func TestRouterQuota_GatedByRouterRequired(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())

	// Inject a basic-tier user (MaxRouterDevice == 0 → no router entitlement).
	// AuthRequired and ProRequired are skipped — we only need to verify
	// RouterRequired blocks the request.
	r.GET("/api/router/quota",
		func(c *gin.Context) {
			c.Set("authContext", &authContext{
				UserID: 1,
				User:   &User{ID: 1, Tier: TierBasic},
			})
			c.Next()
		},
		RouterRequired(),
		api_router_quota,
	)

	w := httptest.NewRecorder()
	req, err := http.NewRequest("GET", "/api/router/quota", nil)
	require.NoError(t, err)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Code int `json:"code"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	// Accept either the current ErrorPaymentRequired (402) or the upcoming
	// ErrorPlanNoRouter (402001) — Task 10 switches the emit code.
	assert.Contains(t, []int{int(ErrorPaymentRequired), int(ErrorPlanNoRouter)}, resp.Code,
		"expected 402 or 402001 for basic-tier user, got %d", resp.Code)
}

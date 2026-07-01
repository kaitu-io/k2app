package center

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

// 无线用户经 RouterRequired → ErrorPlanNoRouter（线路门控，与 tier 无关）。
func TestRouterQuota_NoLineGated(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	u := routerReqTestUser(t, TierFamily, time.Now().Add(30*24*time.Hour).Unix()) // family 但无线
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.GET("/api/router/quota",
		func(c *gin.Context) { c.Set("authContext", &authContext{UserID: u.ID, User: u}); c.Next() },
		RouterRequired(), api_router_quota)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/router/quota", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Code int `json:"code"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, int(ErrorPlanNoRouter), resp.Code)
}

// 持线用户(即便 basic+共享过期) → 200 + 一账号一路由器 + 无限 LAN。
func TestRouterQuota_LineOwnerUnlimitedLan(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	u := routerReqTestUser(t, TierBasic, time.Now().Add(-24*time.Hour).Unix())
	createActivePrivateNodeSub(t, u.ID)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.GET("/api/router/quota",
		func(c *gin.Context) { c.Set("authContext", &authContext{UserID: u.ID, User: u}); c.Next() },
		RouterRequired(), api_router_quota)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/router/quota", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Code int                 `json:"code"`
		Data routerQuotaResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 0, resp.Code)
	assert.Equal(t, 1, resp.Data.MaxRouterDevice)
	assert.Equal(t, -1, resp.Data.MaxLanClient)
}

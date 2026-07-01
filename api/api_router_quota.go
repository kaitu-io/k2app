package center

import "github.com/gin-gonic/gin"

// routerQuotaResponse is the response shape for GET /api/router/quota.
type routerQuotaResponse struct {
	MaxRouterDevice int `json:"maxRouterDevice"`
	MaxLanClient    int `json:"maxLanClient"`
}

// GET /api/router/quota — returns the router's quota for k2r to enforce its LAN
// allowlist size locally. Router access is one-account-one-router with unlimited
// LAN clients (bounded by the line's traffic quota, not a device count), derived
// from active-line ownership — independent of app tier.
//
// Auth chain (route.go): AuthRequired → EnforceDeviceClass → RouterRequired
// (active private line). No tier or shared-membership dependency.
func api_router_quota(c *gin.Context) {
	resp := routerQuotaResponse{
		MaxRouterDevice: 1,
		MaxLanClient:    -1,
	}
	Success(c, &resp)
}

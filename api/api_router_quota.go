package center

import "github.com/gin-gonic/gin"

// routerQuotaResponse is the response shape for GET /api/router/quota.
type routerQuotaResponse struct {
	MaxRouterDevice int `json:"maxRouterDevice"`
	MaxLanClient    int `json:"maxLanClient"`
}

// GET /api/router/quota — returns the user's router-related quota for k2r to
// enforce its LAN allowlist size locally.
//
// Auth chain (in route.go): AuthRequired → EnforceDeviceClass → ProRequired → RouterRequired.
// Together these guarantee: token valid → device class matches header → membership
// active → plan tier includes router entitlement (MaxRouterDevice > 0).
func api_router_quota(c *gin.Context) {
	user := ReqUser(c)
	q := user.Quota()
	resp := routerQuotaResponse{
		MaxRouterDevice: q.MaxRouterDevice,
		MaxLanClient:    q.MaxLanClient,
	}
	Success(c, &resp)
}

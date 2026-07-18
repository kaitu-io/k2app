package center

import "github.com/gin-gonic/gin"

// api_router_control_key mints or returns the caller's account-level router
// control key (idempotent). All devices on the account share the same key —
// k2r converges on it via /api/subs (control_key_hash); the app authenticates
// to k2r's /api/core with the plaintext.
//
// Route: POST /api/user/router-control-key (AuthRequired)
func api_router_control_key(c *gin.Context) {
	user := ReqUser(c)
	if user == nil {
		Error(c, ErrorNotLogin, "authentication failed")
		return
	}
	key, err := EnsureRouterControlKey(c.Request.Context(), user.ID)
	if err != nil {
		Error(c, ErrorSystemError, "ensure router control key failed")
		return
	}
	WriteAuditLog(c, "router_control_key_ensure", "user", user.UUID, nil)
	Success(c, &gin.H{"controlKey": key})
}

// api_router_control_key_reset unconditionally rotates the caller's router
// control key. k2r picks up the new hash on its next /api/subs refresh; any
// app already holding the old plaintext gets 401 from k2r and must re-fetch.
//
// Route: POST /api/user/router-control-key/reset (AuthRequired)
func api_router_control_key_reset(c *gin.Context) {
	user := ReqUser(c)
	if user == nil {
		Error(c, ErrorNotLogin, "authentication failed")
		return
	}
	key, err := ResetRouterControlKey(c.Request.Context(), user.ID)
	if err != nil {
		Error(c, ErrorSystemError, "reset router control key failed")
		return
	}
	WriteAuditLog(c, "router_control_key_reset", "user", user.UUID, nil)
	Success(c, &gin.H{"controlKey": key})
}

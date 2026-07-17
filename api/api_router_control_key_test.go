package center

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// routerControlKeyAPIContext builds a gin context with the given user injected
// as the authenticated principal (bypassing the JWT middleware chain), mirroring
// gatewayCredentialContext in api_gateway_credential_test.go.
func routerControlKeyAPIContext(t *testing.T, user *User, path string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, path, nil)
	c.Set("authContext", &authContext{UserID: user.ID, User: user})
	return c, w
}

func TestAPIRouterControlKeyIdempotent(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	user := routerControlKeyTestUser(t)

	c, w := routerControlKeyAPIContext(t, user, "/api/user/router-control-key")
	api_router_control_key(c)
	code, data := parseJobResponse(t, w.Body.Bytes())
	if code != 0 {
		t.Fatalf("code = %v, body=%s", code, w.Body.String())
	}
	k1, _ := data["controlKey"].(string)
	if k1 == "" {
		t.Fatal("missing controlKey in response")
	}

	c, w = routerControlKeyAPIContext(t, user, "/api/user/router-control-key")
	api_router_control_key(c)
	_, data = parseJobResponse(t, w.Body.Bytes())
	if k2, _ := data["controlKey"].(string); k2 != k1 {
		t.Fatalf("idempotent violated: %q != %q", k2, k1)
	}
}

func TestAPIRouterControlKeyReset(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	user := routerControlKeyTestUser(t)

	c, w := routerControlKeyAPIContext(t, user, "/api/user/router-control-key")
	api_router_control_key(c)
	_, data := parseJobResponse(t, w.Body.Bytes())
	k1, _ := data["controlKey"].(string)

	c, w = routerControlKeyAPIContext(t, user, "/api/user/router-control-key/reset")
	api_router_control_key_reset(c)
	code, data := parseJobResponse(t, w.Body.Bytes())
	if code != 0 {
		t.Fatalf("reset code = %v, body=%s", code, w.Body.String())
	}
	if k2, _ := data["controlKey"].(string); k2 == k1 || k2 == "" {
		t.Fatalf("reset must rotate: old=%q new=%q", k1, k2)
	}
}

func TestAPIRouterControlKeyRequiresLogin(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/user/router-control-key", nil)
	api_router_control_key(c)
	code, _ := parseJobResponse(t, w.Body.Bytes())
	if code == 0 {
		t.Fatal("must reject unauthenticated request")
	}
}

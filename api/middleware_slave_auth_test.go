package center

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// Final-review fix C1: SlaveAuthRequired()'s node-level Basic-Auth failures
// (bad NodeSecret, unregistered/stale IPv4, malformed header) must NOT reuse
// ErrorNotLogin (401) -- k2/wire/auth_remote.go's NewRemoteValidator treats a
// 401 response body from /slave/device-check-auth as an explicit DEVICE
// credential rejection. A misconfigured *node* (wrong secret, IP changed
// after re-provisioning) must not masquerade as "your device token is
// invalid" to every end user routed through it. It must land in
// NewRemoteValidator's `default:` branch (any code outside {0,401,402,403})
// -> Code:-1 "cannot determine", not an explicit device-credential reject.
//
// This test drives the *actual* SlaveAuthRequired() middleware (not just the
// downstream handler) mounted exactly as route.go wires
// POST /slave/device-check-auth, so both the node-level and device-level
// failure paths are reachable through the same endpoint and are shown to
// diverge correctly.

// buildSlaveDeviceCheckAuthRouter mirrors route.go's exact wiring for
// POST /slave/device-check-auth: SlaveAuthRequired() then the handler.
func buildSlaveDeviceCheckAuthRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/slave/device-check-auth", SlaveAuthRequired(), api_slave_device_check_auth)
	return r
}

func postDeviceCheckAuth(r *gin.Engine, basicUser, basicPass string, setBasicAuth bool, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/slave/device-check-auth", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if setBasicAuth {
		req.SetBasicAuth(basicUser, basicPass)
	}
	r.ServeHTTP(w, req)
	return w
}

// TestSlaveAuthRequired_NodeLevelFailures_ReturnSystemErrorNot401 covers all
// three node-auth-failure call sites in SlaveAuthRequired(): missing/malformed
// Basic-Auth header, unknown node IPv4, and secret mismatch. All three must
// come back as ErrorSystemError (500 in the JSON body), always with HTTP 200
// per this codebase's response convention, and critically NOT ErrorNotLogin
// (401) -- the code NewRemoteValidator reads as an explicit device reject.
func TestSlaveAuthRequired_NodeLevelFailures_ReturnSystemErrorNot401(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	knownIP := "10.99.30.1"
	db.Get().Unscoped().Where("ipv4 = ?", knownIP).Delete(&SlaveNode{})
	node := &SlaveNode{Ipv4: knownIP, SecretToken: "correct-secret", Country: "JP", Region: "japan", Name: "node-authfail-" + knownIP, Class: NodeClassShared}
	require.NoError(t, db.Get().Create(node).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(node) })

	r := buildSlaveDeviceCheckAuthRouter()
	body := `{"udid":"whatever","token":"whatever"}`

	t.Run("MissingBasicAuth", func(t *testing.T) {
		w := postDeviceCheckAuth(r, "", "", false, body)
		require.Equal(t, 200, w.Code, "Center convention: HTTP status is always 200")
		resp, err := ParseResponse(w)
		require.NoError(t, err)
		require.EqualValues(t, ErrorSystemError, ErrorCode(resp.Code), "missing basic auth must be ErrorSystemError, not ErrorNotLogin: %s", resp.Message)
	})

	t.Run("UnknownNodeIPv4", func(t *testing.T) {
		w := postDeviceCheckAuth(r, "203.0.113.250", "irrelevant", true, body)
		require.Equal(t, 200, w.Code)
		resp, err := ParseResponse(w)
		require.NoError(t, err)
		require.EqualValues(t, ErrorSystemError, ErrorCode(resp.Code), "unknown node ipv4 must be ErrorSystemError, not ErrorNotLogin: %s", resp.Message)
	})

	t.Run("SecretMismatch", func(t *testing.T) {
		w := postDeviceCheckAuth(r, knownIP, "wrong-secret", true, body)
		require.Equal(t, 200, w.Code)
		resp, err := ParseResponse(w)
		require.NoError(t, err)
		require.EqualValues(t, ErrorSystemError, ErrorCode(resp.Code), "secret mismatch must be ErrorSystemError, not ErrorNotLogin: %s", resp.Message)
	})
}

// TestSlaveAuthRequired_DeviceLevelFailure_StillReturns401 proves the fix is
// scoped correctly: a VALID node identity (SlaveAuthRequired succeeds) whose
// DEVICE-level token is genuinely bad must still come back ErrorNotLogin
// (401) -- unchanged. This is the case NewRemoteValidator's explicit device
// rejection is supposed to catch, and it must not regress.
func TestSlaveAuthRequired_DeviceLevelFailure_StillReturns401(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	now := time.Now().Unix()

	ip := "10.99.30.2"
	db.Get().Unscoped().Where("ipv4 = ?", ip).Delete(&SlaveNode{})
	node := &SlaveNode{Ipv4: ip, SecretToken: "correct-secret-2", Country: "JP", Region: "japan", Name: "node-devfail-" + ip, Class: NodeClassShared}
	require.NoError(t, db.Get().Create(node).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(node) })

	user := CreateTestUser(t)
	user.ExpiredAt = now + 86400
	require.NoError(t, db.Get().Save(user).Error)
	device := CreateTestDevice(t, user.ID, "udid-slaveauth-devfail-"+time.Now().Format("150405.000000"))
	require.NoError(t, db.Get().Model(&Device{}).Where("id = ?", device.ID).
		Update("tunnel_issue_at", now).Error)
	device.TunnelIssueAt = now

	// Valid tunnel token, then bump the anchor so the token is stale -- a
	// genuine device-credential rejection (mirrors
	// TestSlaveDeviceCheckAuth_TunnelToken/TunnelToken_AnchorBumped_401, but
	// driven through the real SlaveAuthRequired() middleware this time).
	tok := generateTestTunnelToken(t, device, time.Hour)
	require.NoError(t, db.Get().Model(&Device{}).Where("id = ?", device.ID).
		Update("tunnel_issue_at", now+1).Error)
	t.Cleanup(func() {
		db.Get().Model(&Device{}).Where("id = ?", device.ID).Update("tunnel_issue_at", now)
	})

	r := buildSlaveDeviceCheckAuthRouter()
	body := `{"udid":"` + device.UDID + `","token":"` + tok + `"}`
	w := postDeviceCheckAuth(r, ip, "correct-secret-2", true, body)
	require.Equal(t, 200, w.Code)
	resp, err := ParseResponse(w)
	require.NoError(t, err)
	require.EqualValues(t, ErrorNotLogin, ErrorCode(resp.Code), "valid node + stale device token must still be ErrorNotLogin (401), unchanged by the node-auth fix: %s", resp.Message)
}

package center

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// basicAuthValue base64-encodes "udid:token" for the Authorization header.
func basicAuthValue(udid, token string) string {
	return base64.StdEncoding.EncodeToString([]byte(udid + ":" + token))
}

// =====================================================================
// Final whole-branch review fixes (Phase 0 tunnel-credential):
//   finding #5 — /api/subs tunnel-token path skipped device app-info refresh
//   finding #6 — /api/subs had no blocked-user gate
// =====================================================================

// TestApiSubs_TunnelToken_RefreshesDeviceAppInfo: a /api/subs request
// authenticated via tunnel token, carrying a valid X-K2-Client header, must
// update the device row's app-info fields — mirrors what handleJWTAuth
// already does for free on the access-token path. Router devices (Task 5)
// mint tunnel-token credentials from creation and never hit any other
// authenticated Center endpoint, so without this fix their app-info columns
// would never populate.
func TestApiSubs_TunnelToken_RefreshesDeviceAppInfo(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	gin.SetMode(gin.TestMode)

	_, device, _ := subsTunnelFixture(t, "appinfo")
	anchor := time.Now().Unix()
	require.NoError(t, db.Get().Model(&Device{}).Where("id = ?", device.ID).
		Update("tunnel_issue_at", anchor).Error)
	device.TunnelIssueAt = anchor

	// Sanity: fixture device starts with no app-info populated.
	var before Device
	require.NoError(t, db.Get().First(&before, device.ID).Error)
	require.Empty(t, before.AppVersion, "fixture device must start with empty app-info")

	tok := generateTestTunnelToken(t, device, time.Hour)

	r := gin.New()
	r.GET("/api/subs", api_subs)
	req, _ := http.NewRequest("GET", "/api/subs", nil)
	req.Header.Set("Authorization", "Basic "+basicAuthValue(device.UDID, tok))
	req.Header.Set("X-K2-Client", "kaitu-service/0.5.1 (macos; arm64; macOS 14.5; MacBookPro18,1)")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

	var after Device
	require.NoError(t, db.Get().First(&after, device.ID).Error)
	require.Equal(t, "0.5.1", after.AppVersion)
	require.Equal(t, "macos", after.AppPlatform)
	require.Equal(t, "arm64", after.AppArch)
	require.Equal(t, "macOS 14.5", after.OSVersion)
	require.Equal(t, "MacBookPro18,1", after.DeviceModel)
}

// TestApiSubs_AccessToken_StillRefreshesDeviceAppInfo: no-regression check —
// the pre-existing access-token path continues to refresh app-info via
// handleJWTAuth's own internal call (this fix must not double-refresh or
// break that path).
func TestApiSubs_AccessToken_StillRefreshesDeviceAppInfo(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	gin.SetMode(gin.TestMode)

	user, device, _ := subsTunnelFixture(t, "appinfo-access")
	access := GenerateTestToken(user.ID, device.UDID, time.Hour)
	require.NoError(t, db.Get().Model(&Device{}).Where("id = ?", device.ID).
		Update("token_issue_at", tokenIssueAtOf(t, access)).Error)

	r := gin.New()
	r.GET("/api/subs", api_subs)
	req, _ := http.NewRequest("GET", "/api/subs", nil)
	req.Header.Set("Authorization", "Basic "+basicAuthValue(device.UDID, access))
	req.Header.Set("X-K2-Client", "kaitu-service/0.5.2 (windows; amd64; Windows 11 23H2)")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

	var after Device
	require.NoError(t, db.Get().First(&after, device.ID).Error)
	require.Equal(t, "0.5.2", after.AppVersion)
	require.Equal(t, "windows", after.AppPlatform)
}

// TestApiSubs_BlockedUser_TunnelToken_403: a blocked account authenticating
// via tunnel token must be rejected — before this fix /api/subs never
// checked isUserBlocked on any credential path (Task 2 only added the check
// to /slave/device-check-auth).
func TestApiSubs_BlockedUser_TunnelToken_403(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	gin.SetMode(gin.TestMode)

	user, device, _ := subsTunnelFixture(t, "blocked-tt")
	anchor := time.Now().Unix()
	require.NoError(t, db.Get().Model(&Device{}).Where("id = ?", device.ID).
		Update("tunnel_issue_at", anchor).Error)
	device.TunnelIssueAt = anchor

	blocked := true
	user.IsBlocked = &blocked
	require.NoError(t, db.Get().Save(user).Error)

	tok := generateTestTunnelToken(t, device, time.Hour)

	w := subsGet(t, device.UDID, tok)
	require.Equal(t, http.StatusForbidden, w.Code, "blocked user must get 403, body=%s", w.Body.String())
	require.Contains(t, w.Body.String(), "account blocked")
}

// TestApiSubs_BlockedUser_AccessToken_403: same gate on the legacy
// access-token credential path (transition-period dual accept).
func TestApiSubs_BlockedUser_AccessToken_403(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	gin.SetMode(gin.TestMode)

	user, device, _ := subsTunnelFixture(t, "blocked-at")
	access := GenerateTestToken(user.ID, device.UDID, time.Hour)
	require.NoError(t, db.Get().Model(&Device{}).Where("id = ?", device.ID).
		Update("token_issue_at", tokenIssueAtOf(t, access)).Error)

	blocked := true
	user.IsBlocked = &blocked
	require.NoError(t, db.Get().Save(user).Error)

	w := subsGet(t, device.UDID, access)
	require.Equal(t, http.StatusForbidden, w.Code, "blocked user must get 403, body=%s", w.Body.String())
	require.Contains(t, w.Body.String(), "account blocked")
}

// TestApiSubs_NotBlockedUser_StillWorks: no-regression sanity — an
// unblocked user's request is unaffected by the new gate.
func TestApiSubs_NotBlockedUser_StillWorks(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	gin.SetMode(gin.TestMode)

	_, device, domain := subsTunnelFixture(t, "notblocked")
	anchor := time.Now().Unix()
	require.NoError(t, db.Get().Model(&Device{}).Where("id = ?", device.ID).
		Update("tunnel_issue_at", anchor).Error)
	device.TunnelIssueAt = anchor

	tok := generateTestTunnelToken(t, device, time.Hour)
	w := subsGet(t, device.UDID, tok)
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	require.Contains(t, w.Body.String(), domain)
}

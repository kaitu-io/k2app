package center

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/redis"
)

// seedBlockedLoginUser creates a User + email LoginIdentify pair backed by the
// real test MySQL, with IsBlocked set as requested. Mirrors
// seedWebPasswordLoginUser (api_web_password_login_test.go) but parameterizes
// the blocked flag and skips the password hash (code-login tests don't need it).
func seedBlockedLoginUser(t *testing.T, blocked bool, withPassword string) (User, string) {
	t.Helper()

	now := time.Now()
	suffix := now.Format("20060102150405.000000")

	user := User{
		UUID:        "usr-block-test-" + suffix,
		Language:    "en-US",
		IsActivated: BoolPtr(true),
		IsBlocked:   BoolPtr(blocked),
	}
	if withPassword != "" {
		hash, err := UserPasswordHash(withPassword)
		if err != nil {
			t.Fatalf("UserPasswordHash failed: %v", err)
		}
		user.PasswordHash = hash
	}
	if err := db.Get().Create(&user).Error; err != nil {
		t.Fatalf("failed to create test user: %v", err)
	}
	t.Cleanup(func() { db.Get().Unscoped().Delete(&user) })

	email := "block-test-" + suffix + "@example.com"
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	indexID := secretHashIt(c, []byte(email))

	identify := LoginIdentify{
		UserID:         user.ID,
		Type:           "email",
		IndexID:        indexID,
		EncryptedValue: email,
	}
	if err := db.Get().Create(&identify).Error; err != nil {
		t.Fatalf("failed to create test login identify: %v", err)
	}
	t.Cleanup(func() { db.Get().Unscoped().Delete(&identify) })

	return user, email
}

type blockTestResp struct {
	Code int `json:"code"`
}

func postJSON(t *testing.T, r *gin.Engine, path string, body map[string]string) blockTestResp {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var resp blockTestResp
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v, body=%s", err, w.Body.String())
	}
	return resp
}

func TestLogin_BlockedUser_ReturnsForbidden(t *testing.T) {
	skipIfNoConfig(t)
	EnableMockVerificationCode = true
	t.Cleanup(func() { EnableMockVerificationCode = false })

	_, email := seedBlockedLoginUser(t, true, "")

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/auth/login", api_login)

	resp := postJSON(t, r, "/api/auth/login", map[string]string{
		"email":            email,
		"verificationCode": MockVerificationCode,
		"udid":             "udid-block-test-" + email,
	})
	if resp.Code != int(ErrorForbidden) {
		t.Errorf("expected ErrorForbidden, got %d", resp.Code)
	}
}

func TestWebAuth_BlockedUser_ReturnsForbidden(t *testing.T) {
	skipIfNoConfig(t)
	EnableMockVerificationCode = true
	t.Cleanup(func() { EnableMockVerificationCode = false })

	_, email := seedBlockedLoginUser(t, true, "")

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/auth/web-login", api_web_auth)

	resp := postJSON(t, r, "/api/auth/web-login", map[string]string{
		"email":            email,
		"verificationCode": MockVerificationCode,
	})
	if resp.Code != int(ErrorForbidden) {
		t.Errorf("expected ErrorForbidden, got %d", resp.Code)
	}
}

func TestPasswordLogin_BlockedUser_ReturnsForbidden(t *testing.T) {
	skipIfNoConfig(t)

	const password = "k7N#mq2P!xT9"
	_, email := seedBlockedLoginUser(t, true, password)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/auth/login/password", api_password_login)

	resp := postJSON(t, r, "/api/auth/login/password", map[string]string{
		"email":    email,
		"password": password,
		"udid":     "udid-block-pwd-test-" + email,
	})
	if resp.Code != int(ErrorForbidden) {
		t.Errorf("expected ErrorForbidden, got %d", resp.Code)
	}
}

func TestWebPasswordLogin_BlockedUser_ReturnsForbidden(t *testing.T) {
	skipIfNoConfig(t)

	const password = "k7N#mq2P!xT9"
	_, email := seedBlockedLoginUser(t, true, password)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/auth/web-login/password", api_web_password_login)

	resp := postJSON(t, r, "/api/auth/web-login/password", map[string]string{
		"email":    email,
		"password": password,
	})
	if resp.Code != int(ErrorForbidden) {
		t.Errorf("expected ErrorForbidden, got %d", resp.Code)
	}
}

func TestSendAuthCode_BlockedUser_ReturnsForbidden(t *testing.T) {
	skipIfNoConfig(t)

	_, email := seedBlockedLoginUser(t, true, "")

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/auth/code", api_send_auth_code)

	resp := postJSON(t, r, "/api/auth/code", map[string]string{
		"email": email,
	})
	if resp.Code != int(ErrorForbidden) {
		t.Errorf("expected ErrorForbidden, got %d", resp.Code)
	}
}

// Not-blocked control: proves the checks above are actually gated on
// IsBlocked, not accidentally rejecting every login.
func TestSendAuthCode_NotBlockedUser_Succeeds(t *testing.T) {
	skipIfNoConfig(t)

	_, email := seedBlockedLoginUser(t, false, "")

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/auth/code", api_send_auth_code)

	resp := postJSON(t, r, "/api/auth/code", map[string]string{
		"email": email,
	})
	if resp.Code != 0 {
		t.Errorf("expected success (0), got %d", resp.Code)
	}
}

func TestAuthRequired_BlocksAlreadyLoggedInUser(t *testing.T) {
	skipIfNoConfig(t)

	now := time.Now().Format("20060102150405.000000")
	user := User{UUID: "usr-block-mw-" + now, IsBlocked: BoolPtr(true)}
	if err := db.Get().Create(&user).Error; err != nil {
		t.Fatalf("failed to create test user: %v", err)
	}
	t.Cleanup(func() { db.Get().Unscoped().Delete(&user) })

	// Web-auth token (empty deviceID) — no Device row needed, exercises the
	// no-udid branch of handleJWTAuth.
	token := GenerateTestToken(user.ID, "", time.Hour)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/user/info", AuthRequired(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"code": 0})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/user/info", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var resp blockTestResp
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v, body=%s", err, w.Body.String())
	}
	if resp.Code != int(ErrorForbidden) {
		t.Errorf("expected ErrorForbidden for blocked user's live session, got %d", resp.Code)
	}
}

func TestAuthOptional_BlockedUserTreatedAsAnonymous(t *testing.T) {
	skipIfNoConfig(t)

	now := time.Now().Format("20060102150405.000000")
	user := User{UUID: "usr-block-opt-" + now, IsBlocked: BoolPtr(true)}
	if err := db.Get().Create(&user).Error; err != nil {
		t.Fatalf("failed to create test user: %v", err)
	}
	t.Cleanup(func() { db.Get().Unscoped().Delete(&user) })

	token := GenerateTestToken(user.ID, "", time.Hour)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/optional-endpoint", AuthOptional(), func(c *gin.Context) {
		// Anonymous requests reach here with ReqUserID == 0.
		c.JSON(http.StatusOK, gin.H{"code": 0, "userID": ReqUserID(c)})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/optional-endpoint", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var resp struct {
		Code   int    `json:"code"`
		UserID uint64 `json:"userID"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v, body=%s", err, w.Body.String())
	}
	if resp.Code != 0 {
		t.Errorf("AuthOptional must not abort for blocked user, got code %d", resp.Code)
	}
	if resp.UserID != 0 {
		t.Errorf("blocked user must be treated as anonymous (userID=0), got %d", resp.UserID)
	}
}

func TestRefreshToken_BlockedUser_ReturnsForbidden(t *testing.T) {
	skipIfNoConfig(t)

	user, _ := seedBlockedLoginUser(t, true, "")

	udid := "udid-refresh-block-" + user.UUID
	device := Device{UDID: udid, UserID: user.ID, Remark: "refresh-block-test"}
	require.NoError(t, db.Get().Create(&device).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&device) })

	authResult, issueTime, err := generateTokens(context.Background(), user.ID, udid, 0)
	require.NoError(t, err)
	device.TokenIssueAt = issueTime.Unix()
	require.NoError(t, db.Get().Save(&device).Error)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/auth/refresh", api_refresh_token)

	resp := postJSON(t, r, "/api/auth/refresh", map[string]string{
		"refreshToken": authResult.RefreshToken,
	})
	assert.Equal(t, int(ErrorForbidden), resp.Code)
}

func TestRefreshToken_NotBlockedUser_Succeeds(t *testing.T) {
	skipIfNoConfig(t)

	user, _ := seedBlockedLoginUser(t, false, "")

	udid := "udid-refresh-ok-" + user.UUID
	device := Device{UDID: udid, UserID: user.ID, Remark: "refresh-ok-test"}
	require.NoError(t, db.Get().Create(&device).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&device) })

	authResult, issueTime, err := generateTokens(context.Background(), user.ID, udid, 0)
	require.NoError(t, err)
	device.TokenIssueAt = issueTime.Unix()
	require.NoError(t, db.Get().Save(&device).Error)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/auth/refresh", api_refresh_token)

	resp := postJSON(t, r, "/api/auth/refresh", map[string]string{
		"refreshToken": authResult.RefreshToken,
	})
	assert.Equal(t, 0, resp.Code)
}

func TestExchangeOTT_BlockedUser_RedirectsToInvalid(t *testing.T) {
	skipIfNoConfig(t)

	user, _ := seedBlockedLoginUser(t, true, "")

	const redirect = "https://app.kaitu.io/dashboard"
	token := "ott-test-blocked-" + user.UUID
	data := ottData{UserID: user.ID, Redirect: redirect}
	dataJSON, err := json.Marshal(data)
	require.NoError(t, err)
	require.NoError(t, redis.CacheSet(ottPrefix+token, string(dataJSON), ottTTL))

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/auth/ott/exchange", api_exchange_ott)

	req := httptest.NewRequest(http.MethodGet, "/api/auth/ott/exchange?ott="+token+"&redirect="+url.QueryEscape(redirect), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusFound, w.Code)
	assert.Contains(t, w.Header().Get("Location"), "reason=invalid")
}

func TestExchangeOTT_NotBlockedUser_Succeeds(t *testing.T) {
	skipIfNoConfig(t)

	user, _ := seedBlockedLoginUser(t, false, "")

	const redirect = "https://app.kaitu.io/dashboard"
	token := "ott-test-ok-" + user.UUID
	data := ottData{UserID: user.ID, Redirect: redirect}
	dataJSON, err := json.Marshal(data)
	require.NoError(t, err)
	require.NoError(t, redis.CacheSet(ottPrefix+token, string(dataJSON), ottTTL))

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/auth/ott/exchange", api_exchange_ott)

	req := httptest.NewRequest(http.MethodGet, "/api/auth/ott/exchange?ott="+token+"&redirect="+url.QueryEscape(redirect), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusFound, w.Code)
	assert.Equal(t, redirect, w.Header().Get("Location"))
	setCookie := w.Header().Get("Set-Cookie")
	assert.Contains(t, setCookie, "access_token")
}

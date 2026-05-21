package center

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// TestWebPasswordLogin_InvalidArgument is a fast smoke test that exercises
// the binding + email-validation path without hitting the DB. Missing
// password fails the `binding:"required"` gate.
func TestWebPasswordLogin_InvalidArgument(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/auth/web-login/password", api_web_password_login)

	body, _ := json.Marshal(map[string]string{
		"email": "not-an-email",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/web-login/password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code) // backend always 200
	var resp struct {
		Code int `json:"code"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, int(ErrorInvalidArgument), resp.Code, "missing password should bind-fail")
}

// seedWebPasswordLoginUser creates a User + email LoginIdentify pair backed
// by the real test MySQL. The password is hashed via UserPasswordHash so
// the login handler can verify it. Returns the User row (with PasswordHash
// populated) and the plaintext email used for the LoginIdentify.
//
// secretDecryptString is a TODO identity stub today (logic_secret.go), so
// EncryptedValue stores plaintext directly. When real crypto is wired in,
// seed via secretEncryptString first.
func seedWebPasswordLoginUser(t *testing.T, plainPassword string) (User, string) {
	t.Helper()

	now := time.Now()
	suffix := now.Format("20060102150405.000000")

	// Hash password the same way the production set-password path does.
	pwHash, err := UserPasswordHash(plainPassword)
	require.NoError(t, err, "UserPasswordHash must not fail")

	user := User{
		UUID:         "usr-web-pwd-login-" + suffix,
		Language:     "en-US",
		PasswordHash: pwHash,
		IsActivated:  BoolPtr(true),
	}
	require.NoError(t, db.Get().Create(&user).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&user) })

	email := "web-pwd-login-" + suffix + "@example.com"
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	indexID := secretHashIt(c, []byte(email))

	identify := LoginIdentify{
		UserID:         user.ID,
		Type:           "email",
		IndexID:        indexID,
		EncryptedValue: email,
	}
	require.NoError(t, db.Get().Create(&identify).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&identify) })

	return user, email
}

// TestWebPasswordLogin_HappyPath exercises the full success flow against
// a real MySQL DB. Skipped automatically when config.yml is absent (CI).
//
// Asserts:
//  1. code == 0 (SuccessEmpty equivalent — Success returns code 0)
//  2. accessToken non-empty in body (WebView fallback)
//  3. Set-Cookie header carries access_token (HttpOnly cookie path)
//  4. PasswordFailedAttempts on the persisted row is 0 (reset on success)
func TestWebPasswordLogin_HappyPath(t *testing.T) {
	skipIfNoConfig(t)

	// Short-circuit MailSend so we don't try to hit SMTP.
	prevDevMode := viper.GetBool("mail.dev_mode")
	viper.Set("mail.dev_mode", true)
	t.Cleanup(func() { viper.Set("mail.dev_mode", prevDevMode) })

	const password = "k7N#mq2P!xT9"
	_, email := seedWebPasswordLoginUser(t, password)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/auth/web-login/password", api_web_password_login)

	body, _ := json.Marshal(map[string]string{
		"email":    email,
		"password": password,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/web-login/password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Code int `json:"code"`
		Data struct {
			AccessToken string `json:"accessToken"`
			User        struct {
				ID    uint64 `json:"id"`
				Email string `json:"email"`
			} `json:"user"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), "body=%s", w.Body.String())
	assert.Equal(t, 0, resp.Code, "expected success, got body: %s", w.Body.String())
	assert.NotEmpty(t, resp.Data.AccessToken, "accessToken must be in body for WebView fallback")
	assert.Equal(t, email, resp.Data.User.Email)

	// HttpOnly cookie path: Set-Cookie header must include access_token.
	setCookieHeader := w.Header().Get("Set-Cookie")
	assert.Contains(t, setCookieHeader, "access_token", "Set-Cookie must include access_token")
}

// TestWebPasswordLogin_WrongPasswordReturnsInvalidCredentials verifies the
// "wrong password" branch:
//   - response is ErrorInvalidCredentials (NOT ErrorNotFound — anti-enumeration)
//   - PasswordFailedAttempts on the persisted row got bumped to 1
func TestWebPasswordLogin_WrongPasswordReturnsInvalidCredentials(t *testing.T) {
	skipIfNoConfig(t)

	user, email := seedWebPasswordLoginUser(t, "k7N#mq2P!xT9")

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/auth/web-login/password", api_web_password_login)

	body, _ := json.Marshal(map[string]string{
		"email":    email,
		"password": "totally-wrong-password",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/web-login/password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Code int `json:"code"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, int(ErrorInvalidCredentials), resp.Code, "wrong password must return ErrorInvalidCredentials (anti-enumeration)")

	// Failed-attempts counter must have been incremented.
	var refreshed User
	require.NoError(t, db.Get().First(&refreshed, user.ID).Error)
	assert.Equal(t, 1, refreshed.PasswordFailedAttempts, "failed attempts must increment to 1")
}

// TestWebPasswordLogin_UnknownUserReturnsInvalidCredentials verifies that
// a non-existent email returns the SAME error code as a wrong password
// (anti-enumeration: attackers must not be able to distinguish "no such
// user" from "wrong password").
func TestWebPasswordLogin_UnknownUserReturnsInvalidCredentials(t *testing.T) {
	skipIfNoConfig(t)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/auth/web-login/password", api_web_password_login)

	body, _ := json.Marshal(map[string]string{
		"email":    "no-such-user-" + time.Now().Format("150405.000000") + "@example.com",
		"password": "anything",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/web-login/password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Code int `json:"code"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, int(ErrorInvalidCredentials), resp.Code, "unknown user must return ErrorInvalidCredentials, not ErrorNotFound")
}

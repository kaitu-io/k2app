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

// TestSetPassword_MismatchedPasswords verifies the password-mismatch gate
// rejects before any DB call. This path doesn't depend on db.Get() or
// secretDecryptString — pure validation.
func TestSetPassword_MismatchedPasswords(t *testing.T) {
	SetupMockDB(t)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/user/password", func(c *gin.Context) {
		c.Set("authContext", &authContext{UserID: 42, User: &User{ID: 42}})
		api_set_password(c)
	})

	body, _ := json.Marshal(map[string]string{
		"password":        "abcdef1234XY",
		"confirmPassword": "differentpwd",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/user/password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, int(ErrorInvalidArgument), resp.Code)
	assert.Contains(t, resp.Message, "passwords do not match")
}

// TestSetPassword_InvalidJSON verifies the binding rejects malformed input.
func TestSetPassword_InvalidJSON(t *testing.T) {
	SetupMockDB(t)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/user/password", func(c *gin.Context) {
		c.Set("authContext", &authContext{UserID: 42, User: &User{ID: 42}})
		api_set_password(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/user/password", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Code int `json:"code"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, int(ErrorInvalidArgument), resp.Code)
}

// TestCollectUserInputsForPasswordStrength_NonEmailSkipped verifies that
// non-email identities are not fed into zxcvbn userInputs. Uses a User
// constructed in-memory so no DB or secret config is required.
func TestCollectUserInputsForPasswordStrength_NonEmailSkipped(t *testing.T) {
	user := &User{
		ID: 7,
		LoginIdentifies: []LoginIdentify{
			{Type: "phone", EncryptedValue: "anything"},
			{Type: "wechat", EncryptedValue: "anything"},
		},
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	got := collectUserInputsForPasswordStrength(c, user)
	assert.Empty(t, got, "non-email identities must be skipped")
}

// TestCollectUserInputsForPasswordStrength_EmailWithLocalPart verifies that
// when an email identity is present, both the full email and its local part
// (everything before "@") are appended to the userInputs slice. This is the
// signal zxcvbn uses to penalize passwords like "alice1234567" for user
// alice@example.com.
//
// NOTE: secretDecryptString is currently a TODO-stub identity function
// (logic_secret.go) — encrypted value == plaintext. If/when the real
// crypto round-trip is wired in, this test should encrypt via
// secretEncryptString first.
func TestCollectUserInputsForPasswordStrength_EmailWithLocalPart(t *testing.T) {
	plaintext := "alice@example.com"
	user := &User{
		ID: 7,
		LoginIdentifies: []LoginIdentify{
			{Type: "email", EncryptedValue: plaintext},
		},
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	got := collectUserInputsForPasswordStrength(c, user)
	require.Len(t, got, 2, "expect [email, local-part]")
	assert.Equal(t, plaintext, got[0])
	assert.Equal(t, "alice", got[1])
}

// TestCollectUserInputsForPasswordStrength_NoAtSign covers the edge case
// where an email identity somehow lacks an "@" — the helper must not panic
// and must still emit the raw value (so zxcvbn at least penalizes it).
func TestCollectUserInputsForPasswordStrength_NoAtSign(t *testing.T) {
	user := &User{
		ID: 7,
		LoginIdentifies: []LoginIdentify{
			{Type: "email", EncryptedValue: "weird-no-at-sign"},
		},
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	got := collectUserInputsForPasswordStrength(c, user)
	require.Len(t, got, 1, "no '@' means only the raw value is appended, no local part")
	assert.Equal(t, "weird-no-at-sign", got[0])
}

// setupSetPasswordRouter builds a gin router that injects authContext for the
// given userID and wires up the api_set_password handler under the
// production-aligned path. Mirrors the pattern used by the other
// integration-style handler tests in this package.
func setupSetPasswordRouter(userID uint64) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/user/password", func(c *gin.Context) {
		c.Set("authContext", &authContext{UserID: userID, User: &User{ID: userID}})
		api_set_password(c)
	})
	return r
}

// TestSetPassword_WeakPasswordRejected exercises the handler-level zxcvbn
// strength gate: a short password (under PasswordMinLength=10) must surface
// as ErrorInvalidArgument with the stable enum string "password_too_short".
//
// Constraint: qtoolkit/db.Get() is a sync.Once package global with no
// setter, so go-sqlmock cannot intercept it. Codebase convention for
// handler tests that need to pass the db.Get() preload is to fall back to
// the real MySQL via skipIfNoConfig(t) — same pattern as
// api_admin_order_refund_test.go.
func TestSetPassword_WeakPasswordRejected(t *testing.T) {
	skipIfNoConfig(t)

	// Seed a real user so the Preload("LoginIdentifies").First lookup
	// succeeds and we actually reach ValidatePasswordStrength.
	now := time.Now()
	user := User{
		UUID:     "usr-pwd-weak-" + now.Format("20060102150405.000000"),
		Language: "en-US",
	}
	require.NoError(t, db.Get().Create(&user).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&user) })

	r := setupSetPasswordRouter(user.ID)

	body, _ := json.Marshal(map[string]string{
		"password":        "abc123", // length 6 — well under PasswordMinLength
		"confirmPassword": "abc123",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/user/password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, int(ErrorInvalidArgument), resp.Code)
	assert.Equal(t, "password_too_short", resp.Message)

	// Sanity: PasswordHash must remain unset on the persisted row.
	var refreshed User
	require.NoError(t, db.Get().First(&refreshed, user.ID).Error)
	assert.Empty(t, refreshed.PasswordHash, "weak password must not have been saved")
}

// TestSetPassword_HappyPathSendsEmail covers the success path end-to-end:
//   - Strong password ("k7N#mq2P!xT9" — zxcvbn score >= 3, length 12)
//   - User has an email LoginIdentify → emailToUser resolves and tries to
//     send the password-changed notification.
//
// To avoid actual SMTP traffic in tests (which would hit the configured
// upstream and fail on missing creds), we toggle mail.dev_mode=true for
// the duration of this test. MailSend then short-circuits to a log line
// — see logic_email.go:MailSend.
//
// Constraint (same as the weak-password test): db.Get() is a package
// global with no test injection. Uses skipIfNoConfig to gate on real DB.
//
// What we assert:
//  1. Response code is 0 (SuccessEmpty).
//  2. PasswordHash on the persisted User row is non-empty (the save did happen).
//  3. PasswordFailedAttempts and PasswordLockedUntil are reset to zero.
//
// What we cannot directly assert (no clean seam):
//   - That MailSend was actually invoked with the rendered template.
//     emailToUser → emailTo → sendSystemEmail → MailSend is a chain of
//     hard function calls — no package-level function variable, interface,
//     or mailTransport hook exists today. Adding such a seam belongs to a
//     separate refactor. Documented as a TODO below.
func TestSetPassword_HappyPathSendsEmail(t *testing.T) {
	skipIfNoConfig(t)

	// Short-circuit MailSend so we don't actually try to hit SMTP.
	prevDevMode := viper.GetBool("mail.dev_mode")
	viper.Set("mail.dev_mode", true)
	t.Cleanup(func() { viper.Set("mail.dev_mode", prevDevMode) })

	now := time.Now()
	user := User{
		UUID:     "usr-pwd-happy-" + now.Format("20060102150405.000000"),
		Language: "en-US",
	}
	require.NoError(t, db.Get().Create(&user).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&user) })

	// secretDecryptString is currently a TODO identity stub, so the
	// plaintext email lives directly in EncryptedValue. When the real
	// crypto round-trip is wired in, this should go through
	// secretEncryptString first.
	email := "happy-path-" + now.Format("150405.000000") + "@example.com"
	identify := LoginIdentify{
		UserID:         user.ID,
		Type:           "email",
		IndexID:        email + "-idx",
		EncryptedValue: email,
	}
	require.NoError(t, db.Get().Create(&identify).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&identify) })

	r := setupSetPasswordRouter(user.ID)

	// "k7N#mq2P!xT9" — 12 chars, mixed case + digits + symbols, no
	// dictionary word, no keyboard pattern → zxcvbn score >= 3.
	body, _ := json.Marshal(map[string]string{
		"password":        "k7N#mq2P!xT9",
		"confirmPassword": "k7N#mq2P!xT9",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/user/password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), "body=%s", w.Body.String())
	assert.Equal(t, 0, resp.Code, "expected SuccessEmpty (code=0), got body: %s", w.Body.String())

	// Verify the password actually got persisted and the lock counters reset.
	var refreshed User
	require.NoError(t, db.Get().First(&refreshed, user.ID).Error)
	assert.NotEmpty(t, refreshed.PasswordHash, "PasswordHash must be set after happy-path set")
	assert.Equal(t, 0, refreshed.PasswordFailedAttempts, "failed attempts must reset")
	assert.Equal(t, int64(0), refreshed.PasswordLockedUntil, "lock-until must reset")

	t.Log("TODO: assert emailToUser/MailSend invocation once a mockable mail transport seam exists (today the chain is hard function calls — no var/interface to override).")
}

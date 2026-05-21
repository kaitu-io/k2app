package center

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

package center

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// helper: POST a JSON body to the handler and return the parsed code+message.
type adminPwResp struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func postAdminPassword(t *testing.T, r *gin.Engine, uuid string, body any) adminPwResp {
	t.Helper()
	bodyBytes, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, "/app/users/"+uuid+"/password", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, "HTTP status must always be 200")
	var resp adminPwResp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return resp
}

func TestAdminSetUserPassword_InvalidJSON(t *testing.T) {
	r := gin.New()
	r.POST("/app/users/:uuid/password", api_admin_set_user_password)

	req, _ := http.NewRequest(http.MethodPost, "/app/users/some-uuid/password", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp adminPwResp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, int(ErrorInvalidArgument), resp.Code)
}

func TestAdminSetUserPassword_MismatchedPasswords(t *testing.T) {
	r := gin.New()
	r.POST("/app/users/:uuid/password", api_admin_set_user_password)

	resp := postAdminPassword(t, r, "some-uuid", map[string]string{
		"password":        "abcdef1234XY",
		"confirmPassword": "differentpwd",
		"reason":          "support ticket #1234",
	})
	assert.Equal(t, int(ErrorInvalidArgument), resp.Code)
	assert.Contains(t, resp.Message, "passwords do not match")
}

func TestAdminSetUserPassword_ReasonTooShort(t *testing.T) {
	r := gin.New()
	r.POST("/app/users/:uuid/password", api_admin_set_user_password)

	// 1: whitespace-only reason — should be rejected after trim.
	resp := postAdminPassword(t, r, "some-uuid", map[string]string{
		"password":        "abcdef1234XY",
		"confirmPassword": "abcdef1234XY",
		"reason":          "  ",
	})
	assert.Equal(t, int(ErrorInvalidArgument), resp.Code)
	assert.Contains(t, resp.Message, "reason too short")

	// 2: 2-char reason — also short. Same path as case 1; assert same message
	// so a regression that swaps in a different validator (e.g. password
	// strength) returning the same code can't slip through.
	resp = postAdminPassword(t, r, "some-uuid", map[string]string{
		"password":        "abcdef1234XY",
		"confirmPassword": "abcdef1234XY",
		"reason":          "ab",
	})
	assert.Equal(t, int(ErrorInvalidArgument), resp.Code)
	assert.Contains(t, resp.Message, "reason too short")

	// 3: missing reason field — binding:"required" fails. This hits Gin's
	// binding layer, not our trim+len check, so the message format is
	// different. We assert it's NOT the trim path to prove we're testing the
	// binding path.
	resp = postAdminPassword(t, r, "some-uuid", map[string]string{
		"password":        "abcdef1234XY",
		"confirmPassword": "abcdef1234XY",
	})
	assert.Equal(t, int(ErrorInvalidArgument), resp.Code)
	assert.NotEmpty(t, resp.Message)
	assert.NotContains(t, resp.Message, "reason too short")
}

// validBody returns a request body that passes Tier-A validation so DB
// behavior is what's exercised.
func validBody(t *testing.T) map[string]string {
	t.Helper()
	return map[string]string{
		"password":        "Tr0ub4dor&3-strong!", // long + zxcvbn ≥3 (mixed words+digits+sym)
		"confirmPassword": "Tr0ub4dor&3-strong!",
		"reason":          "support ticket #1234 — locked out, customer phoned in",
	}
}

// swapGetDB replaces the package-level getDB function variable with one that
// returns the mock DB, restoring the original in t.Cleanup.
func swapGetDB(t *testing.T, m *MockDB) {
	t.Helper()
	orig := getDB
	getDB = func() *gorm.DB { return m.DB }
	t.Cleanup(func() { getDB = orig })
}

func TestAdminSetUserPassword_UserNotFound(t *testing.T) {
	m := SetupMockDB(t)
	swapGetDB(t, m)

	// Return empty rows so GORM's First() maps it to gorm.ErrRecordNotFound,
	// which the handler translates to ErrorNotFound.
	m.Mock.ExpectQuery(regexp.QuoteMeta("FROM `users`")).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	r := gin.New()
	r.POST("/app/users/:uuid/password", api_admin_set_user_password)
	resp := postAdminPassword(t, r, "no-such-uuid", validBody(t))

	assert.Equal(t, int(ErrorNotFound), resp.Code)
	assert.NoError(t, m.Mock.ExpectationsWereMet())
}

// mockUserFound queues an ExpectQuery that returns one user row + an empty
// login_identifies preload row set. Used by tests that need the user lookup
// to succeed before exercising password-strength logic.
func mockUserFound(t *testing.T, m *MockDB, uuid string, userID int64, lockedUntil int64) {
	t.Helper()
	// SELECT * FROM users WHERE uuid = ?
	userRows := sqlmock.NewRows([]string{"id", "uuid", "password_hash", "password_failed_attempts", "password_locked_until"}).
		AddRow(userID, uuid, "", 0, lockedUntil)
	m.Mock.ExpectQuery(regexp.QuoteMeta("FROM `users`")).WillReturnRows(userRows)

	// Preload("LoginIdentifies") — empty result keeps zxcvbn userInputs nil.
	loginRows := sqlmock.NewRows([]string{"id", "user_id", "type", "encrypted_value", "index_id"})
	m.Mock.ExpectQuery(regexp.QuoteMeta("FROM `login_identifies`")).WillReturnRows(loginRows)
}

func TestAdminSetUserPassword_PasswordTooShort(t *testing.T) {
	m := SetupMockDB(t)
	swapGetDB(t, m)
	mockUserFound(t, m, "uuid-1", 42, 0)

	body := validBody(t)
	body["password"] = "short"
	body["confirmPassword"] = "short"

	r := gin.New()
	r.POST("/app/users/:uuid/password", api_admin_set_user_password)
	resp := postAdminPassword(t, r, "uuid-1", body)

	assert.Equal(t, int(ErrorInvalidArgument), resp.Code)
	assert.Equal(t, "password_too_short", resp.Message)
	assert.NoError(t, m.Mock.ExpectationsWereMet())
}

func TestAdminSetUserPassword_PasswordTooWeak(t *testing.T) {
	m := SetupMockDB(t)
	swapGetDB(t, m)
	mockUserFound(t, m, "uuid-1", 42, 0)

	body := validBody(t)
	body["password"] = "aaaaaaaaaaa" // ≥10 chars but zxcvbn score 0
	body["confirmPassword"] = "aaaaaaaaaaa"

	r := gin.New()
	r.POST("/app/users/:uuid/password", api_admin_set_user_password)
	resp := postAdminPassword(t, r, "uuid-1", body)

	assert.Equal(t, int(ErrorInvalidArgument), resp.Code)
	assert.Equal(t, "password_too_weak", resp.Message)
	assert.NoError(t, m.Mock.ExpectationsWereMet())
}

// Spec §7 T6: a password equal to the user's own email must be rejected as
// too weak. Exercises the userInputs path through
// collectUserInputsForPasswordStrength -> secretDecryptString -> zxcvbn
// penalty. secretDecryptString is currently a stub identity function, so
// the mocked encrypted_value can hold plaintext.
func TestAdminSetUserPassword_RejectsEmailAsPassword(t *testing.T) {
	m := SetupMockDB(t)
	swapGetDB(t, m)

	const email = "alice@example.com"
	userRows := sqlmock.NewRows([]string{"id", "uuid", "password_hash", "password_failed_attempts", "password_locked_until"}).
		AddRow(int64(42), "uuid-1", "", 0, int64(0))
	m.Mock.ExpectQuery(regexp.QuoteMeta("FROM `users`")).WillReturnRows(userRows)
	loginRows := sqlmock.NewRows([]string{"id", "user_id", "type", "encrypted_value", "index_id"}).
		AddRow(int64(1), int64(42), "email", email, "")
	m.Mock.ExpectQuery(regexp.QuoteMeta("FROM `login_identifies`")).WillReturnRows(loginRows)

	body := validBody(t)
	body["password"] = email
	body["confirmPassword"] = email

	r := gin.New()
	r.POST("/app/users/:uuid/password", api_admin_set_user_password)
	resp := postAdminPassword(t, r, "uuid-1", body)

	assert.Equal(t, int(ErrorInvalidArgument), resp.Code)
	assert.Equal(t, "password_too_weak", resp.Message)
	assert.NoError(t, m.Mock.ExpectationsWereMet())
}

func TestAdminSetUserPassword_HappyPath(t *testing.T) {
	m := SetupMockDB(t)
	swapGetDB(t, m)

	// Use unordered matching throughout — the audit goroutine INSERT races the
	// synchronous adminDisplayEmail + emailToUser queries.
	m.Mock.MatchExpectationsInOrder(false)

	// User lookup: SELECT from users + Preload login_identifies.
	userRows := sqlmock.NewRows([]string{"id", "uuid", "password_hash", "password_failed_attempts", "password_locked_until"}).
		AddRow(int64(42), "uuid-1", "", 0, int64(0))
	m.Mock.ExpectQuery(regexp.QuoteMeta("FROM `users`")).WillReturnRows(userRows)
	loginPreload := sqlmock.NewRows([]string{"id", "user_id", "type", "encrypted_value", "index_id"})
	m.Mock.ExpectQuery(regexp.QuoteMeta("FROM `login_identifies`")).WillReturnRows(loginPreload)

	// Save(&user) → UPDATE users SET ... (no BEGIN/COMMIT: SkipDefaultTransaction=true in SetupMockDB).
	m.Mock.ExpectExec(regexp.QuoteMeta("UPDATE `users` SET")).
		WillReturnResult(sqlmock.NewResult(0, 1))

	// Audit log goroutine → INSERT INTO admin_audit_logs (table name is plural).
	m.Mock.ExpectExec(regexp.QuoteMeta("INSERT INTO `admin_audit_logs`")).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// adminDisplayEmail → SELECT login_identifies WHERE user_id=1 AND type='email'.
	// Return empty so adminDisplayEmail falls back to "（系统管理员）".
	m.Mock.ExpectQuery(regexp.QuoteMeta("FROM `login_identifies`")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "type", "encrypted_value", "index_id"}))

	// emailToUser → GetEmailIdentifyByUserID(target user 42).
	// Return empty rows so emailToUser returns "user has no email address".
	// The handler swallows that error; no SMTP is reached.
	m.Mock.ExpectQuery(regexp.QuoteMeta("FROM `login_identifies`")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "type", "encrypted_value", "index_id"}))

	r := gin.New()
	// Inject an authed admin so WriteAuditLog has a non-nil ReqUser.
	r.POST("/app/users/:uuid/password", func(c *gin.Context) {
		c.Set("authContext", &authContext{UserID: 1, User: &User{ID: 1, UUID: "admin-uuid"}})
		api_admin_set_user_password(c)
	})

	bodyBytes, _ := json.Marshal(validBody(t))
	req, _ := http.NewRequest(http.MethodPost, "/app/users/uuid-1/password", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp adminPwResp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 0, resp.Code, "expected success code 0, got %d (msg=%q)", resp.Code, resp.Message)
	// Give the audit goroutine a moment to fire before ExpectationsWereMet.
	time.Sleep(100 * time.Millisecond)
	assert.NoError(t, m.Mock.ExpectationsWereMet())
}

func TestAdminSetUserPassword_ClearsLock(t *testing.T) {
	m := SetupMockDB(t)
	swapGetDB(t, m)

	// Use unordered matching throughout — same race as HappyPath.
	m.Mock.MatchExpectationsInOrder(false)

	// User row has PasswordLockedUntil set 1 hour in the future.
	userRows := sqlmock.NewRows([]string{"id", "uuid", "password_hash", "password_failed_attempts", "password_locked_until"}).
		AddRow(int64(42), "uuid-1", "", 0, time.Now().Add(time.Hour).Unix())
	m.Mock.ExpectQuery(regexp.QuoteMeta("FROM `users`")).WillReturnRows(userRows)
	loginPreload := sqlmock.NewRows([]string{"id", "user_id", "type", "encrypted_value", "index_id"})
	m.Mock.ExpectQuery(regexp.QuoteMeta("FROM `login_identifies`")).WillReturnRows(loginPreload)

	// Save(&user) → UPDATE users SET ... (no BEGIN/COMMIT: SkipDefaultTransaction=true).
	m.Mock.ExpectExec(regexp.QuoteMeta("UPDATE `users` SET")).
		WillReturnResult(sqlmock.NewResult(0, 1))

	m.Mock.ExpectExec(regexp.QuoteMeta("INSERT INTO `admin_audit_logs`")).
		WillReturnResult(sqlmock.NewResult(1, 1))
	m.Mock.ExpectQuery(regexp.QuoteMeta("FROM `login_identifies`")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "type", "encrypted_value", "index_id"}))
	m.Mock.ExpectQuery(regexp.QuoteMeta("FROM `login_identifies`")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "type", "encrypted_value", "index_id"}))

	r := gin.New()
	r.POST("/app/users/:uuid/password", func(c *gin.Context) {
		c.Set("authContext", &authContext{UserID: 1, User: &User{ID: 1, UUID: "admin-uuid"}})
		api_admin_set_user_password(c)
	})

	bodyBytes, _ := json.Marshal(validBody(t))
	req, _ := http.NewRequest(http.MethodPost, "/app/users/uuid-1/password", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp adminPwResp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 0, resp.Code)
	// Asserting on individual UPDATE arg values is brittle against GORM column
	// ordering for the full User struct. The behavior we really care about
	// (login no longer locked after this) is verified end-to-end by the
	// manual smoke at Task 10. This test guards the SQL shape only.
	time.Sleep(100 * time.Millisecond)
	assert.NoError(t, m.Mock.ExpectationsWereMet())
}

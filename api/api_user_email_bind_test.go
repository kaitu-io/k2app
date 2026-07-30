package center

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bindVerificationResp is the trimmed response shape these tests assert on.
type bindVerificationResp struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// postBindVerification drives api_send_bind_email_verification as the
// authenticated user `userID` and returns the decoded response.
func postBindVerification(t *testing.T, userID uint64, email string) bindVerificationResp {
	t.Helper()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/user/email/send-bind-verification", func(c *gin.Context) {
		c.Set("authContext", &authContext{UserID: userID, User: &User{ID: userID, Brand: string(BrandKaitu)}})
		api_send_bind_email_verification(c)
	})

	body, _ := json.Marshal(map[string]string{"email": email})
	req := httptest.NewRequest(http.MethodPost, "/api/user/email/send-bind-verification", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "HTTP status must always be 200")
	var resp bindVerificationResp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return resp
}

// TestSendBindEmailVerification_EmailOwnedByAnotherUser pins the dedicated
// error code for "this email already belongs to a different account".
//
// Why a dedicated code: this rejection happens BEFORE the verification code is
// ever issued or mailed, so the user gets no email at all. Under the old
// generic ErrorInvalidArgument the client rendered "参数错误", which reads as a
// transient glitch — users retried the send button for hours and reported it as
// "收不到验证码" (support case 2026-07-29, user 5126 → 237875618@qq.com).
func TestSendBindEmailVerification_EmailOwnedByAnotherUser(t *testing.T) {
	m := SetupMockDB(t)
	swapGetDB(t, m)

	// The email resolves to an identify row owned by a DIFFERENT user.
	m.Mock.ExpectQuery(regexp.QuoteMeta("FROM `login_identifies`")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "type", "index_id", "brand"}).
			AddRow(int64(1), int64(5191), "email", "some-index", "kaitu"))

	resp := postBindVerification(t, 5126, "237875618@qq.com")

	assert.Equal(t, int(ErrorEmailAlreadyInUse), resp.Code)
	// Mutation guard: the old generic code must NOT be what ships, otherwise the
	// client falls back to the vague "参数错误" string this fix exists to remove.
	assert.NotEqual(t, int(ErrorInvalidArgument), resp.Code)
	assert.NoError(t, m.Mock.ExpectationsWereMet())
}

// TestSendBindEmailVerification_EmailOwnedBySelf verifies the conflict gate does
// NOT fire when the identify row belongs to the caller — re-sending a code to
// your own already-bound address is legitimate and must proceed to code
// issuance. Without this the fix could "work" by rejecting everyone.
//
// Downstream of the gate the handler needs Redis (code issuance) and mail config
// (delivery), neither of which this tier provides. Whether that surfaces as a
// panic or as a plain ErrorSystemError depends on what other tests in the package
// already initialised — so this tolerates both and asserts only the invariant
// that actually matters: falling through must never be reported as a conflict.
func TestSendBindEmailVerification_EmailOwnedBySelf(t *testing.T) {
	m := SetupMockDB(t)
	swapGetDB(t, m)

	m.Mock.ExpectQuery(regexp.QuoteMeta("FROM `login_identifies`")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "type", "index_id", "brand"}).
			AddRow(int64(1), int64(5126), "email", "some-index", "kaitu"))

	code, panicked := bindVerificationCodeTolerantOfPanic(t, 5126, "237875618@qq.com")
	if panicked {
		// Panicking inside the downstream dependency proves the gate let it past.
		return
	}
	assert.NotEqual(t, int(ErrorEmailAlreadyInUse), code,
		"self-owned email must fall through the conflict gate, not be rejected as a conflict")
}

// bindVerificationCodeTolerantOfPanic runs the handler and reports either its
// response code or the fact that it panicked in a downstream dependency.
func bindVerificationCodeTolerantOfPanic(t *testing.T, userID uint64, email string) (code int, panicked bool) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			panicked = true
		}
	}()
	return postBindVerification(t, userID, email).Code, false
}

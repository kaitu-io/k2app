package center

import (
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
)

// jsonUnmarshalBody parses a recorded response body into v. Small local
// helper because these tests POST empty bodies (block/unblock take no
// request payload), unlike postAdminPassword in
// api_admin_user_password_test.go which always sends a JSON body.
func jsonUnmarshalBody(t *testing.T, w *httptest.ResponseRecorder, v any) error {
	t.Helper()
	return json.Unmarshal(w.Body.Bytes(), v)
}

func postAdminBlockAction(t *testing.T, r *gin.Engine, path string) adminPwResp {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, path, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, "HTTP status must always be 200")
	var resp adminPwResp
	require.NoError(t, jsonUnmarshalBody(t, w, &resp))
	return resp
}

func TestAdminBlockUser_NotFound(t *testing.T) {
	m := SetupMockDB(t)
	swapGetDB(t, m)

	m.Mock.ExpectQuery(regexp.QuoteMeta("FROM `users`")).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	r := gin.New()
	r.POST("/app/users/:uuid/block", api_admin_block_user)

	resp := postAdminBlockAction(t, r, "/app/users/no-such-uuid/block")
	assert.Equal(t, int(ErrorNotFound), resp.Code)
	assert.NoError(t, m.Mock.ExpectationsWereMet())
}

func TestAdminBlockUser_HappyPath(t *testing.T) {
	m := SetupMockDB(t)
	swapGetDB(t, m)
	m.Mock.MatchExpectationsInOrder(false)

	userRows := sqlmock.NewRows([]string{"id", "uuid", "is_blocked"}).
		AddRow(int64(42), "uuid-1", false)
	m.Mock.ExpectQuery(regexp.QuoteMeta("FROM `users`")).WillReturnRows(userRows)

	m.Mock.ExpectExec(regexp.QuoteMeta("UPDATE `users` SET")).
		WillReturnResult(sqlmock.NewResult(0, 1))

	m.Mock.ExpectExec(regexp.QuoteMeta("INSERT INTO `admin_audit_logs`")).
		WillReturnResult(sqlmock.NewResult(1, 1))

	r := gin.New()
	r.POST("/app/users/:uuid/block", func(c *gin.Context) {
		c.Set("authContext", &authContext{UserID: 1, User: &User{ID: 1, UUID: "admin-uuid"}})
		api_admin_block_user(c)
	})

	resp := postAdminBlockAction(t, r, "/app/users/uuid-1/block")
	assert.Equal(t, 0, resp.Code)
	time.Sleep(100 * time.Millisecond)
	assert.NoError(t, m.Mock.ExpectationsWereMet())
}

func TestAdminUnblockUser_HappyPath(t *testing.T) {
	m := SetupMockDB(t)
	swapGetDB(t, m)
	m.Mock.MatchExpectationsInOrder(false)

	userRows := sqlmock.NewRows([]string{"id", "uuid", "is_blocked"}).
		AddRow(int64(42), "uuid-1", true)
	m.Mock.ExpectQuery(regexp.QuoteMeta("FROM `users`")).WillReturnRows(userRows)

	m.Mock.ExpectExec(regexp.QuoteMeta("UPDATE `users` SET")).
		WillReturnResult(sqlmock.NewResult(0, 1))

	m.Mock.ExpectExec(regexp.QuoteMeta("INSERT INTO `admin_audit_logs`")).
		WillReturnResult(sqlmock.NewResult(1, 1))

	r := gin.New()
	r.POST("/app/users/:uuid/unblock", func(c *gin.Context) {
		c.Set("authContext", &authContext{UserID: 1, User: &User{ID: 1, UUID: "admin-uuid"}})
		api_admin_unblock_user(c)
	})

	resp := postAdminBlockAction(t, r, "/app/users/uuid-1/unblock")
	assert.Equal(t, 0, resp.Code)
	time.Sleep(100 * time.Millisecond)
	assert.NoError(t, m.Mock.ExpectationsWereMet())
}

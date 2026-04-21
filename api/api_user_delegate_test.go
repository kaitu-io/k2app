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

// TestGetDelegate_NotSet verifies the handler returns null data when the user has
// no DelegateID set. This path does not touch the DB, so no mock is required here
// beyond SetupMockDB for consistency.
func TestGetDelegate_NotSet(t *testing.T) {
	SetupMockDB(t)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/user/delegate", func(c *gin.Context) {
		c.Set("authContext", &authContext{UserID: 42, User: &User{ID: 42}})
		api_get_delegate(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/user/delegate", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Code int         `json:"code"`
		Data interface{} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 0, resp.Code)
	assert.Nil(t, resp.Data)
}

// TestGetDelegate_Set_DBQuery verifies the DB query used by the handler when a
// DelegateID is set. Because qtoolkit/db.Get() reads from a package-scoped global
// that cannot be swapped for the sqlmock instance, we exercise the same query
// against m.DB directly (codebase convention — see beta_channel_test.go).
func TestGetDelegate_Set_DBQuery(t *testing.T) {
	m := SetupMockDB(t)

	m.Mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `login_identifies` WHERE (user_id = ? AND type = ?) AND `login_identifies`.`deleted_at` IS NULL ORDER BY `login_identifies`.`id` LIMIT ?")).
		WithArgs(uint64(99), "email", 1).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "created_at", "updated_at", "deleted_at",
			"user_id", "type", "index_id", "encrypted_value",
		}).AddRow(
			1, time.Now(), time.Now(), nil,
			99, "email", "hash", "alice@example.com",
		))

	var li LoginIdentify
	err := m.DB.Where("user_id = ? AND type = ?", uint64(99), "email").First(&li).Error
	require.NoError(t, err)
	assert.Equal(t, "alice@example.com", li.EncryptedValue)
	assert.NoError(t, m.Mock.ExpectationsWereMet())
}

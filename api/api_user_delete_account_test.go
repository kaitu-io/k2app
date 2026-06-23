package center

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// setupDeleteAccountRouter builds a minimal router that injects an auth context
// for the given user and wires only the delete-account handler. Real auth
// middleware is bypassed — the handler only reads ReqUserID(c).
func setupDeleteAccountRouter(t *testing.T, user *User) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("authContext", &authContext{UserID: user.ID, User: user})
		c.Next()
	})
	r.DELETE("/api/user/delete-account", api_delete_user_account)
	return r
}

// TestDeleteUserAccount_FreesEmailAndDevices pins the "delete frees the email"
// contract: after a user deletes their account, the login_identifies row must be
// hard-deleted (freeing the (type,index_id) unique index so the email can be
// re-registered) and devices removed. The user row itself is soft-deleted.
//
// Regression for the orphaned-identify trap: previously delete-account only
// soft-deleted the user and left login_identifies alive, so re-login resolved
// the identify to a soft-deleted user and api_login's tx.First(&user) returned
// "record not found" -> 500 "server error" forever.
func TestDeleteUserAccount_FreesEmailAndDevices(t *testing.T) {
	skipIfNoDB(t)

	// Arrange: a real user + email login identity + device.
	user := &User{
		UUID:     generateId("test-del"),
		Language: "zh-CN",
	}
	require.NoError(t, db.Get().Create(user).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&User{}, user.ID) })

	indexID := generateId("test-idx") // unique, satisfies idx_type_index_global
	identify := &LoginIdentify{
		UserID:         user.ID,
		Type:           "email",
		IndexID:        indexID,
		EncryptedValue: "deleted-account-test@example.com",
	}
	require.NoError(t, db.Get().Create(identify).Error)
	t.Cleanup(func() { db.Get().Where("user_id = ?", user.ID).Delete(&LoginIdentify{}) })

	device := &Device{
		UDID:         generateId("test-udid"),
		UserID:       user.ID,
		Remark:       "Test Device",
		TokenIssueAt: time.Now().Unix(),
	}
	require.NoError(t, db.Get().Create(device).Error)
	t.Cleanup(func() { db.Get().Where("user_id = ?", user.ID).Delete(&Device{}) })

	// Act: delete the account.
	r := setupDeleteAccountRouter(t, user)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodDelete, "/api/user/delete-account", nil)
	r.ServeHTTP(w, req)

	// Assert: request succeeded.
	assert.Equal(t, http.StatusOK, w.Code)
	resp, err := ParseResponse(w)
	require.NoError(t, err)
	assert.Equal(t, 0, resp.Code, "delete-account should succeed (message: %s)", resp.Message)

	// Assert: email freed — login_identifies row is hard-deleted.
	var identifyCount int64
	require.NoError(t, db.Get().Model(&LoginIdentify{}).
		Where("user_id = ?", user.ID).Count(&identifyCount).Error)
	assert.Equal(t, int64(0), identifyCount, "login_identifies must be removed to free the email")

	// Assert: the (type,index_id) slot is reusable — a fresh insert must not collide.
	reclaim := &LoginIdentify{
		UserID:         user.ID, // any user; we only test the unique-index slot is free
		Type:           "email",
		IndexID:        indexID,
		EncryptedValue: "deleted-account-test@example.com",
	}
	assert.NoError(t, db.Get().Create(reclaim).Error,
		"index_id slot must be free for re-registration after delete")
	db.Get().Where("id = ?", reclaim.ID).Delete(&LoginIdentify{})

	// Assert: devices removed.
	var deviceCount int64
	require.NoError(t, db.Get().Model(&Device{}).
		Where("user_id = ?", user.ID).Count(&deviceCount).Error)
	assert.Equal(t, int64(0), deviceCount, "devices must be removed on account deletion")

	// Assert: user row is soft-deleted (not findable normally, present Unscoped).
	var live User
	err = db.Get().First(&live, user.ID).Error
	assert.Error(t, err, "user should not be findable after soft delete")

	var dead User
	require.NoError(t, db.Get().Unscoped().First(&dead, user.ID).Error)
	assert.True(t, dead.DeletedAt.Valid, "user row should carry deleted_at")
}

package center

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func setupRouterTestContext(user *User) (*httptest.ResponseRecorder, *gin.Context) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/test", nil)
	if user != nil {
		c.Set("authContext", &authContext{User: user, UserID: user.ID})
	}
	return w, c
}

func TestRouterRequired_HasAccess(t *testing.T) {
	_, c := setupRouterTestContext(&User{
		ID:              1,
		ExpiredAt:       time.Now().Add(30 * 24 * time.Hour).Unix(),
		MaxRouterDevice: 1,
	})
	RouterRequired()(c)
	assert.False(t, c.IsAborted())
}

func TestRouterRequired_UnlimitedAccess(t *testing.T) {
	_, c := setupRouterTestContext(&User{
		ID:              1,
		ExpiredAt:       time.Now().Add(30 * 24 * time.Hour).Unix(),
		MaxRouterDevice: -1, // unlimited
	})
	RouterRequired()(c)
	assert.False(t, c.IsAborted())
}

func TestRouterRequired_NoAccess(t *testing.T) {
	w, c := setupRouterTestContext(&User{
		ID:              1,
		ExpiredAt:       time.Now().Add(30 * 24 * time.Hour).Unix(),
		MaxRouterDevice: 0, // no router
	})
	RouterRequired()(c)
	assert.True(t, c.IsAborted())
	assert.Contains(t, w.Body.String(), "402")
}

func TestRouterRequired_Expired(t *testing.T) {
	_, c := setupRouterTestContext(&User{
		ID:              1,
		ExpiredAt:       time.Now().Add(-24 * time.Hour).Unix(),
		MaxRouterDevice: 1,
	})
	RouterRequired()(c)
	assert.True(t, c.IsAborted())
}

func TestRouterRequired_NoUser(t *testing.T) {
	_, c := setupRouterTestContext(nil)
	RouterRequired()(c)
	assert.True(t, c.IsAborted())
}

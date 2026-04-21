package center

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNotifyDelegate_NotLoggedIn(t *testing.T) {
	SetupMockDB(t)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/user/orders/:uuid/notify-delegate", func(c *gin.Context) {
		api_order_notify_delegate(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/user/orders/order-uuid/notify-delegate", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Code int `json:"code"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, int(ErrorNotLogin), resp.Code)
}

func TestNotifyDelegate_NoDelegate(t *testing.T) {
	SetupMockDB(t)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/user/orders/:uuid/notify-delegate", func(c *gin.Context) {
		c.Set("authContext", &authContext{UserID: 42, User: &User{ID: 42, DelegateID: nil}})
		api_order_notify_delegate(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/user/orders/order-uuid/notify-delegate", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Code int `json:"code"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, int(ErrorInvalidArgument), resp.Code)
}

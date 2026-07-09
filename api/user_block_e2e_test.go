package center

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
)

// TestUserBlock_FullCycle drives the real HTTP handlers (not mocks) through
// the entire feature: an admin blocks a user, the user's send-code and
// send-code+login attempts are rejected, the admin detail endpoint reflects
// the flag, then the admin unblocks and login succeeds again.
func TestUserBlock_FullCycle(t *testing.T) {
	skipIfNoConfig(t)
	EnableMockVerificationCode = true
	t.Cleanup(func() { EnableMockVerificationCode = false })

	now := time.Now().Format("20060102150405.000000")
	user := User{UUID: "usr-e2e-block-" + now, IsActivated: BoolPtr(true)}
	if err := db.Get().Create(&user).Error; err != nil {
		t.Fatalf("failed to create test user: %v", err)
	}
	t.Cleanup(func() { db.Get().Unscoped().Delete(&user) })

	email := "e2e-block-" + now + "@example.com"
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	indexID := secretHashIt(c, []byte(email))
	identify := LoginIdentify{UserID: user.ID, Type: "email", IndexID: indexID, EncryptedValue: email}
	if err := db.Get().Create(&identify).Error; err != nil {
		t.Fatalf("failed to create test login identify: %v", err)
	}
	t.Cleanup(func() { db.Get().Unscoped().Delete(&identify) })

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/auth/code", api_send_auth_code)
	r.POST("/api/auth/web-login", api_web_auth)
	r.POST("/app/users/:uuid/block", func(c *gin.Context) {
		c.Set("authContext", &authContext{UserID: 1, User: &User{ID: 1, UUID: "admin-uuid"}})
		api_admin_block_user(c)
	})
	r.POST("/app/users/:uuid/unblock", func(c *gin.Context) {
		c.Set("authContext", &authContext{UserID: 1, User: &User{ID: 1, UUID: "admin-uuid"}})
		api_admin_unblock_user(c)
	})
	r.GET("/app/users/:uuid", api_admin_get_user_detail)

	t.Run("login works before blocking", func(t *testing.T) {
		resp := postJSON(t, r, "/api/auth/web-login", map[string]string{
			"email":            email,
			"verificationCode": MockVerificationCode,
		})
		if resp.Code != 0 {
			t.Fatalf("expected success before blocking, got code %d", resp.Code)
		}
	})

	t.Run("admin blocks the user", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/app/users/"+user.UUID+"/block", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		var resp blockTestResp
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal: %v, body=%s", err, w.Body.String())
		}
		if resp.Code != 0 {
			t.Fatalf("expected block to succeed, got code %d", resp.Code)
		}
	})

	t.Run("send-code rejected after blocking", func(t *testing.T) {
		resp := postJSON(t, r, "/api/auth/code", map[string]string{"email": email})
		if resp.Code != int(ErrorForbidden) {
			t.Errorf("expected ErrorForbidden, got %d", resp.Code)
		}
	})

	t.Run("login rejected after blocking", func(t *testing.T) {
		resp := postJSON(t, r, "/api/auth/web-login", map[string]string{
			"email":            email,
			"verificationCode": MockVerificationCode,
		})
		if resp.Code != int(ErrorForbidden) {
			t.Errorf("expected ErrorForbidden, got %d", resp.Code)
		}
	})

	t.Run("admin detail endpoint reflects isBlocked", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/app/users/"+user.UUID, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		var resp struct {
			Code int `json:"code"`
			Data struct {
				IsBlocked bool `json:"isBlocked"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal: %v, body=%s", err, w.Body.String())
		}
		if !resp.Data.IsBlocked {
			t.Error("expected isBlocked=true in admin detail response")
		}
	})

	t.Run("admin unblocks the user", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/app/users/"+user.UUID+"/unblock", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		var resp blockTestResp
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal: %v, body=%s", err, w.Body.String())
		}
		if resp.Code != 0 {
			t.Fatalf("expected unblock to succeed, got code %d", resp.Code)
		}
	})

	t.Run("login works again after unblocking", func(t *testing.T) {
		resp := postJSON(t, r, "/api/auth/web-login", map[string]string{
			"email":            email,
			"verificationCode": MockVerificationCode,
		})
		if resp.Code != 0 {
			t.Errorf("expected success after unblocking, got code %d", resp.Code)
		}
	})
}

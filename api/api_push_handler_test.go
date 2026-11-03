package center

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// =====================================================================
// /api/push/* 与登出停用 token（spec 2026-11-03-ios-push-design §5.2 / §6.3）
// =====================================================================

// pushTestRouter 挂上被测 handler，用 authContext 注入登录用户（绕开 JWT）。
func pushTestRouter(u *User, udid string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("authContext", &authContext{UserID: u.ID, UDID: udid, User: u})
		c.Next()
	})
	r.POST("/api/push/token", api_register_push_token)
	r.DELETE("/api/push/token", api_unregister_push_token)
	r.GET("/api/push/preferences", api_get_push_preferences)
	r.PUT("/api/push/preferences", api_update_push_preferences)
	r.POST("/api/auth/logout", api_logout)
	return r
}

type apiResp struct {
	Code int             `json:"code"`
	Data json.RawMessage `json:"data"`
}

func doJSON(t *testing.T, r *gin.Engine, method, path string, body any) apiResp {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var out apiResp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out), w.Body.String())
	return out
}

func seedBrandUser(t *testing.T, brand Brand) (User, Device) {
	t.Helper()
	uniq := time.Now().UnixNano()
	u := User{UUID: fmt.Sprintf("usr-pushh-%d", uniq), Brand: string(brand)}
	require.NoError(t, db.Get().Create(&u).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&User{}, u.ID) })
	d := Device{UDID: fmt.Sprintf("udid-pushh-%d", uniq), UserID: u.ID, TokenIssueAt: time.Now().Unix(), TokenLastUsedAt: time.Now().Unix()}
	require.NoError(t, db.Get().Create(&d).Error)
	t.Cleanup(func() { db.Get().Where("udid = ?", d.UDID).Delete(&Device{}) })
	t.Cleanup(func() { db.Get().Unscoped().Where("device_udid = ?", d.UDID).Delete(&PushToken{}) })
	return u, d
}

func iosTokenBody(token, topic string) map[string]any {
	return map[string]any{
		"platform": "ios", "provider": "apns", "token": token, "app_flavor": "app_store",
		"topic": topic, "app_version": "0.4.15",
	}
}

func tokenOfDevice(t *testing.T, udid string) PushToken {
	t.Helper()
	var tok PushToken
	require.NoError(t, db.Get().Where("device_udid = ? AND provider = ?", udid, PushProviderAPNs).First(&tok).Error)
	return tok
}

func TestAPI_RegisterPushToken_Brand(t *testing.T) {
	skipIfNoConfig(t)

	t.Run("topic of the user's brand is accepted", func(t *testing.T) {
		u, d := seedBrandUser(t, BrandOverleap)
		r := pushTestRouter(&u, d.UDID)
		resp := doJSON(t, r, "POST", "/api/push/token", iosTokenBody("tok-ol-1", "io.overleap"))
		require.Equal(t, 0, resp.Code)
		tok := tokenOfDevice(t, d.UDID)
		assert.Equal(t, "io.overleap", tok.Topic)
		assert.Equal(t, u.ID, tok.UserID)
		assert.Equal(t, PushTokenStatusActive, tok.Status)
	})

	t.Run("topic of the other brand is rejected", func(t *testing.T) {
		u, d := seedBrandUser(t, BrandKaitu)
		r := pushTestRouter(&u, d.UDID)
		resp := doJSON(t, r, "POST", "/api/push/token", iosTokenBody("tok-x", "io.overleap"))
		assert.Equal(t, int(ErrorInvalidArgument), resp.Code)
		var n int64
		db.Get().Model(&PushToken{}).Where("device_udid = ?", d.UDID).Count(&n)
		assert.Zero(t, n)
	})

	t.Run("empty topic is filled from the brand", func(t *testing.T) {
		u, d := seedBrandUser(t, BrandKaitu)
		r := pushTestRouter(&u, d.UDID)
		resp := doJSON(t, r, "POST", "/api/push/token", iosTokenBody("tok-k-1", ""))
		require.Equal(t, 0, resp.Code)
		assert.Equal(t, "com.allnationconnect.anc.wgios", tokenOfDevice(t, d.UDID).Topic)
	})

	t.Run("re-register on the same device after account switch rewrites user and reactivates", func(t *testing.T) {
		a, d := seedBrandUser(t, BrandKaitu)
		require.Equal(t, 0, doJSON(t, pushTestRouter(&a, d.UDID), "POST", "/api/push/token", iosTokenBody("tok-sw-1", "")).Code)
		require.NoError(t, db.Get().Model(&PushToken{}).Where("device_udid = ?", d.UDID).Update("status", PushTokenStatusInactive).Error)

		b, _ := seedBrandUser(t, BrandKaitu)
		require.NoError(t, db.Get().Model(&Device{}).Where("udid = ?", d.UDID).Update("user_id", b.ID).Error)
		require.Equal(t, 0, doJSON(t, pushTestRouter(&b, d.UDID), "POST", "/api/push/token", iosTokenBody("tok-sw-2", "")).Code)

		tok := tokenOfDevice(t, d.UDID)
		assert.Equal(t, b.ID, tok.UserID)
		assert.Equal(t, "tok-sw-2", tok.Token)
		assert.Equal(t, PushTokenStatusActive, tok.Status)
	})

	t.Run("request without a device is rejected", func(t *testing.T) {
		u, _ := seedBrandUser(t, BrandKaitu)
		resp := doJSON(t, pushTestRouter(&u, ""), "POST", "/api/push/token", iosTokenBody("tok-nodev", ""))
		assert.Equal(t, int(ErrorInvalidArgument), resp.Code)
	})
}

func TestAPI_PushPreferences(t *testing.T) {
	skipIfNoConfig(t)
	u, d := seedBrandUser(t, BrandKaitu)
	r := pushTestRouter(&u, d.UDID)

	resp := doJSON(t, r, "GET", "/api/push/preferences", nil)
	require.Equal(t, 0, resp.Code)
	assert.JSONEq(t, `{"marketingOptIn":false}`, string(resp.Data))

	before := time.Now().Unix()
	require.Equal(t, 0, doJSON(t, r, "PUT", "/api/push/preferences", map[string]any{"marketingOptIn": true}).Code)
	var got User
	require.NoError(t, db.Get().First(&got, u.ID).Error)
	assert.True(t, got.PushMarketingOptIn)
	assert.GreaterOrEqual(t, got.PushMarketingOptInAt, before)

	resp = doJSON(t, r, "GET", "/api/push/preferences", nil)
	assert.JSONEq(t, `{"marketingOptIn":true}`, string(resp.Data))

	require.Equal(t, 0, doJSON(t, r, "PUT", "/api/push/preferences", map[string]any{"marketingOptIn": false}).Code)
	require.NoError(t, db.Get().First(&got, u.ID).Error)
	assert.False(t, got.PushMarketingOptIn, "false 这个零值也必须落库")

	assert.Equal(t, int(ErrorInvalidArgument), doJSON(t, r, "PUT", "/api/push/preferences", map[string]any{}).Code,
		"缺字段不能被当成 false")
}

func TestLogout_DeactivatesPushToken(t *testing.T) {
	skipIfNoConfig(t)

	t.Run("device account (no identity) keeps device row but token goes inactive", func(t *testing.T) {
		u, d := seedBrandUser(t, BrandKaitu)
		r := pushTestRouter(&u, d.UDID)
		require.Equal(t, 0, doJSON(t, r, "POST", "/api/push/token", iosTokenBody("tok-lo-1", "")).Code)
		require.Equal(t, 0, doJSON(t, r, "POST", "/api/auth/logout", nil).Code)
		assert.Equal(t, PushTokenStatusInactive, tokenOfDevice(t, d.UDID).Status)
	})

	t.Run("account with identity: token goes inactive", func(t *testing.T) {
		u, d := seedBrandUser(t, BrandKaitu)
		ident := LoginIdentify{UserID: u.ID, Type: "email", IndexID: fmt.Sprintf("idx-push-%d", u.ID)}
		require.NoError(t, db.Get().Create(&ident).Error)
		t.Cleanup(func() { db.Get().Unscoped().Delete(&LoginIdentify{}, ident.ID) })
		r := pushTestRouter(&u, d.UDID)
		require.Equal(t, 0, doJSON(t, r, "POST", "/api/push/token", iosTokenBody("tok-lo-2", "")).Code)
		require.Equal(t, 0, doJSON(t, r, "POST", "/api/auth/logout", nil).Code)
		assert.Equal(t, PushTokenStatusInactive, tokenOfDevice(t, d.UDID).Status)
	})
}

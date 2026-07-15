package center

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/redis"
)

func TestIsAllowedRedirectPerBrand(t *testing.T) {
	// kaitu 用户：只许 kaitu.io 域
	assert.True(t, isAllowedRedirect("https://kaitu.io/account", BrandKaitu))
	assert.True(t, isAllowedRedirect("https://www.kaitu.io/x", BrandKaitu))
	assert.False(t, isAllowedRedirect("https://overleap.io/account", BrandKaitu))
	// overleap 用户：只许 overleap.io 域
	assert.True(t, isAllowedRedirect("https://overleap.io/account", BrandOverleap))
	assert.True(t, isAllowedRedirect("https://www.overleap.io/x", BrandOverleap))
	assert.False(t, isAllowedRedirect("https://kaitu.io/account", BrandOverleap))
	// 通用拒绝
	assert.False(t, isAllowedRedirect("http://kaitu.io/x", BrandKaitu))      // 非 https
	assert.False(t, isAllowedRedirect("https://evilkaitu.io/x", BrandKaitu)) // 后缀伪造
	assert.False(t, isAllowedRedirect("https://kaitu.io.evil.com/x", BrandKaitu))
}

func TestCORSAllowsBothBrandOrigins(t *testing.T) {
	origins := corsAllowedOrigins()
	assert.True(t, origins["https://www.kaitu.io"])
	assert.True(t, origins["https://kaitu.io"])
	assert.True(t, origins["https://www.overleap.io"])
	assert.True(t, origins["https://overleap.io"])
	assert.True(t, origins["http://localhost:3000"])
	assert.False(t, origins["https://evil.com"])
}

// TestIssueOTT_NilUserInAuthContext_RejectedNoPanic covers the device-JWT auth
// path where authContext.User comes from a GORM Preload("User") and is nil when
// the user row is missing/soft-deleted: the handler must reject, not panic.
func TestIssueOTT_NilUserInAuthContext_RejectedNoPanic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New() // no Recovery middleware: a panic in the handler fails the test
	r.POST("/api/user/ott", func(c *gin.Context) {
		c.Set("authContext", &authContext{
			UserID: 42,
			UDID:   "udid-nil-user-test",
			Device: &Device{UDID: "udid-nil-user-test", UserID: 42},
			User:   nil,
		})
	}, api_issue_ott)

	req := httptest.NewRequest(http.MethodPost, "/api/user/ott",
		strings.NewReader(`{"redirect":"https://kaitu.io/account"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Code int `json:"code"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, int(ErrorNotLogin), resp.Code)
}

// TestExchangeOTT_CrossBrandRedirect_Rejected: an overleap user's OTT whose
// redirect points at kaitu.io must be rejected at exchange time (and vice
// versa the whitelist is brand-scoped, not a global union).
func TestExchangeOTT_CrossBrandRedirect_Rejected(t *testing.T) {
	skipIfNoConfig(t)

	suffix := time.Now().Format("20060102150405.000000")
	user := User{
		UUID:        "usr-ott-xbrand-" + suffix,
		Language:    "en-US",
		IsActivated: BoolPtr(true),
		Brand:       string(BrandOverleap),
	}
	require.NoError(t, db.Get().Create(&user).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&user) })

	// Redirect targets the kaitu domain, but the OTT belongs to an overleap user.
	const redirect = "https://www.kaitu.io/dashboard"
	token := "ott-test-xbrand-" + user.UUID
	data := ottData{UserID: user.ID, Redirect: redirect}
	dataJSON, err := json.Marshal(data)
	require.NoError(t, err)
	require.NoError(t, redis.CacheSet(ottPrefix+token, string(dataJSON), ottTTL))

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/auth/ott/exchange", api_exchange_ott)

	req := httptest.NewRequest(http.MethodGet,
		"/api/auth/ott/exchange?ott="+token+"&redirect="+url.QueryEscape(redirect), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusFound, w.Code)
	assert.Contains(t, w.Header().Get("Location"), "reason=invalid")
	assert.Empty(t, w.Header().Get("Set-Cookie"))
}

// TestExchangeOTT_SameBrandRedirect_Succeeds: control case — the same overleap
// OTT with an overleap.io redirect goes through and sets the session cookie.
func TestExchangeOTT_SameBrandRedirect_Succeeds(t *testing.T) {
	skipIfNoConfig(t)

	suffix := time.Now().Format("20060102150405.000000")
	user := User{
		UUID:        "usr-ott-sbrand-" + suffix,
		Language:    "en-US",
		IsActivated: BoolPtr(true),
		Brand:       string(BrandOverleap),
	}
	require.NoError(t, db.Get().Create(&user).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&user) })

	const redirect = "https://www.overleap.io/dashboard"
	token := "ott-test-sbrand-" + user.UUID
	data := ottData{UserID: user.ID, Redirect: redirect}
	dataJSON, err := json.Marshal(data)
	require.NoError(t, err)
	require.NoError(t, redis.CacheSet(ottPrefix+token, string(dataJSON), ottTTL))

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/auth/ott/exchange", api_exchange_ott)

	req := httptest.NewRequest(http.MethodGet,
		"/api/auth/ott/exchange?ott="+token+"&redirect="+url.QueryEscape(redirect), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusFound, w.Code)
	assert.Equal(t, redirect, w.Header().Get("Location"))
	assert.Contains(t, w.Header().Get("Set-Cookie"), "access_token")
}

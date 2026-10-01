package center

import (
	"fmt"
	"github.com/golang-jwt/jwt/v5"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

const pxDesktopUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

func pxRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api")
	api.Use(BrandResolver())
	api.GET("/px", api_funnel_px)
	api.GET("/px/optout", api_funnel_px_optout)
	return r
}

// pxMarker 返回一个唯一 plan 标记；事件按它归属与清理（GPC 行没有 anon_id）。
func pxMarker(t *testing.T) string {
	t.Helper()
	funnelPxLimiter.reset()
	funnelPxGlobal.reset()
	t.Cleanup(funnelPxLimiter.reset)
	t.Cleanup(funnelPxGlobal.reset)
	m := fmt.Sprintf("tpx%d", time.Now().UnixNano())
	t.Cleanup(func() { db.Get().Where("plan = ?", m).Delete(&FunnelEvent{}) })
	return m
}

func pxRows(t *testing.T, m string) []FunnelEvent {
	t.Helper()
	funnelFlushForTest()
	var rows []FunnelEvent
	require.NoError(t, db.Get().Where("plan = ?", m).Find(&rows).Error)
	return rows
}

func pxReq(m, event string) *TestRequest {
	return NewTestRequest("GET", "/api/px?e="+event+"&p="+m+"&u=/en-GB/pricing").WithHeader("User-Agent", pxDesktopUA)
}

func assertGIF(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	assert.Equal(t, 200, w.Code)
	assert.Equal(t, "image/gif", w.Header().Get("Content-Type"))
	assert.Equal(t, 43, w.Body.Len())
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
}

func sidFromSetCookie(w *httptest.ResponseRecorder) string {
	for _, c := range w.Result().Cookies() {
		if c.Name == CookieFunnelSid {
			return c.Value
		}
	}
	return ""
}

func TestPx_SetsSidAndRecords(t *testing.T) {
	skipIfNoConfig(t)
	m := pxMarker(t)
	w := pxReq(m, "pricing_view").Execute(pxRouter())
	assertGIF(t, w)
	sc := strings.Join(w.Header().Values("Set-Cookie"), ";")
	sid := sidFromSetCookie(w)
	assert.Len(t, sid, 22)
	assert.Contains(t, sc, "HttpOnly")
	assert.Contains(t, sc, "SameSite=Lax")
	assert.Contains(t, sc, "Max-Age=34560000")
	assert.Contains(t, sc, "Path=/")
	assert.NotContains(t, sc, "Domain")
	assert.NotContains(t, sc, "Secure")
	rows := pxRows(t, m)
	require.Len(t, rows, 1)
	assert.Equal(t, "/pricing", rows[0].Path)
	assert.Equal(t, "web", rows[0].Surface)
	assert.Equal(t, sid, rows[0].AnonID)
	assert.Equal(t, "desktop", rows[0].Device)
	assert.Equal(t, "windows", rows[0].OS)
	assert.Equal(t, "kaitu", rows[0].Brand)
}

func TestPx_ReusesExistingSid(t *testing.T) {
	skipIfNoConfig(t)
	m := pxMarker(t)
	sid := newFunnelSid()
	w := pxReq(m, "pricing_view").WithCookie("sid", sid).Execute(pxRouter())
	assertGIF(t, w)
	assert.Empty(t, w.Header().Values("Set-Cookie"))
	rows := pxRows(t, m)
	require.Len(t, rows, 1)
	assert.Equal(t, sid, rows[0].AnonID)
}

func TestPx_GPC_NoCookieButCounts(t *testing.T) {
	skipIfNoConfig(t)
	m := pxMarker(t)
	w := pxReq(m, "pricing_view").WithHeader("Sec-GPC", "1").Execute(pxRouter())
	assertGIF(t, w)
	assert.Empty(t, w.Header().Values("Set-Cookie"))
	rows := pxRows(t, m)
	require.Len(t, rows, 1)
	assert.Equal(t, "", rows[0].AnonID)
}

func TestPx_OptOut_NotRecorded(t *testing.T) {
	skipIfNoConfig(t)
	m := pxMarker(t)
	w := pxReq(m, "pricing_view").WithCookie("sid", "optout").Execute(pxRouter())
	assertGIF(t, w)
	assert.Empty(t, w.Header().Values("Set-Cookie"))
	assert.Len(t, pxRows(t, m), 0)
}

func TestPx_NotRecordedCases(t *testing.T) {
	skipIfNoConfig(t)
	cases := []struct{ name, event, ua string }{
		{"bot", "pricing_view", "Googlebot/2.1"},
		{"empty ua", "pricing_view", ""},
		{"unknown event", "nope_view", pxDesktopUA},
		{"fact event", "purchase", pxDesktopUA},
		{"app only event", "paywall_view", pxDesktopUA},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := pxMarker(t)
			w := NewTestRequest("GET", "/api/px?e="+c.event+"&p="+m).WithHeader("User-Agent", c.ua).Execute(pxRouter())
			assertGIF(t, w)
			assert.Len(t, pxRows(t, m), 0)
		})
	}
}

func TestPx_RateLimited_StillGif(t *testing.T) {
	skipIfNoConfig(t)
	m := pxMarker(t)
	r := pxRouter()
	for i := 0; i < 121; i++ {
		w := pxReq(m, "pricing_view").WithHeader("Sec-GPC", "1").Execute(r)
		assertGIF(t, w)
	}
	assert.Len(t, pxRows(t, m), 120)
}

func TestPx_LoggedInCookie_LinksIdentity(t *testing.T) {
	skipIfNoConfig(t)
	m := pxMarker(t)
	user := CreateTestUser(t)
	tok := GenerateTestToken(user.ID, "", time.Hour)
	sid := newFunnelSid()
	t.Cleanup(func() { db.Get().Where("anon_id = ?", sid).Delete(&FunnelIdentity{}) })
	w := pxReq(m, "pricing_view").WithCookie(CookieAccessToken, tok).WithCookie("sid", sid).Execute(pxRouter())
	assertGIF(t, w)
	rows := pxRows(t, m)
	require.Len(t, rows, 1)
	assert.Equal(t, user.ID, rows[0].UserID)
	var n int64
	require.NoError(t, db.Get().Model(&FunnelIdentity{}).Where("kind = 'sid' AND anon_id = ? AND user_id = ?", sid, user.ID).Count(&n).Error)
	assert.Equal(t, int64(1), n)
}

func TestPx_BrandMismatchUser_TreatedAnonymous(t *testing.T) {
	skipIfNoConfig(t)
	m := pxMarker(t)
	user := CreateTestUser(t) // brand = kaitu
	tok := GenerateTestToken(user.ID, "", time.Hour)
	sid := newFunnelSid()
	t.Cleanup(func() { db.Get().Where("anon_id = ?", sid).Delete(&FunnelIdentity{}) })
	w := pxReq(m, "pricing_view").WithCookie(CookieAccessToken, tok).WithCookie("sid", sid).
		WithHeader("X-K2-Brand", "overleap").Execute(pxRouter())
	assertGIF(t, w)
	rows := pxRows(t, m)
	require.Len(t, rows, 1)
	assert.Equal(t, uint64(0), rows[0].UserID)
	assert.Equal(t, "overleap", rows[0].Brand)
	var n int64
	require.NoError(t, db.Get().Model(&FunnelIdentity{}).Where("anon_id = ?", sid).Count(&n).Error)
	assert.Equal(t, int64(0), n)
}

func TestPx_Disabled(t *testing.T) {
	skipIfNoConfig(t)
	m := pxMarker(t)
	viper.Set("funnel.enabled", false)
	t.Cleanup(func() { viper.Set("funnel.enabled", true) })
	w := pxReq(m, "pricing_view").Execute(pxRouter())
	assertGIF(t, w)
	assert.Empty(t, w.Header().Values("Set-Cookie"))
	viper.Set("funnel.enabled", true)
	assert.Len(t, pxRows(t, m), 0)
}

func TestPxOptOut_SetsCookieAndRedirects(t *testing.T) {
	// 无 DB：只有 cookie 与重定向
	r := pxRouter()
	same := NewTestRequest("GET", "http://kaitu.test/api/px/optout").WithHeader("Referer", "http://kaitu.test/en-GB/privacy").Execute(r)
	assert.Equal(t, 302, same.Code)
	assert.Equal(t, "/en-GB/privacy", same.Header().Get("Location"))
	assert.Equal(t, "optout", sidFromSetCookie(same))

	foreign := NewTestRequest("GET", "http://kaitu.test/api/px/optout").WithHeader("Referer", "https://evil.example/en-GB/privacy").Execute(r)
	assert.Equal(t, 302, foreign.Code)
	assert.Equal(t, "/", foreign.Header().Get("Location"))

	evil := NewTestRequest("GET", "http://kaitu.test/api/px/optout").WithHeader("Referer", "http://kaitu.test//evil.example/x").Execute(r)
	assert.Equal(t, "/", evil.Header().Get("Location"))

	none := NewTestRequest("GET", "http://kaitu.test/api/px/optout").Execute(r)
	assert.Equal(t, "/", none.Header().Get("Location"))
}

func TestPx_SecureCookieBehindHTTPS(t *testing.T) {
	skipIfNoConfig(t)
	m := pxMarker(t)
	w := pxReq(m, "pricing_view").WithHeader("X-Forwarded-Proto", "https").Execute(pxRouter())
	assert.Contains(t, strings.Join(w.Header().Values("Set-Cookie"), ";"), "Secure")
	pxRows(t, m)
}

func TestPx_GPC_IgnoresExistingSid(t *testing.T) {
	skipIfNoConfig(t)
	m := pxMarker(t)
	user := CreateTestUser(t)
	tok := GenerateTestToken(user.ID, "", time.Hour)
	sid := newFunnelSid()
	t.Cleanup(func() { db.Get().Where("anon_id = ?", sid).Delete(&FunnelIdentity{}) })
	w := pxReq(m, "pricing_view").WithHeader("Sec-GPC", "1").WithCookie("sid", sid).
		WithCookie(CookieAccessToken, tok).Execute(pxRouter())
	assertGIF(t, w)
	assert.Empty(t, w.Header().Values("Set-Cookie"))
	rows := pxRows(t, m)
	require.Len(t, rows, 1)
	assert.Equal(t, "", rows[0].AnonID)
	// GPC = 完全不归因：登录态也不记用户，事件只作无主计数。
	assert.Equal(t, uint64(0), rows[0].UserID)
	var n int64
	require.NoError(t, db.Get().Model(&FunnelIdentity{}).Where("anon_id = ? OR user_id = ?", sid, user.ID).Count(&n).Error)
	assert.Equal(t, int64(0), n)
}

// GPC + 登录（无 sid cookie，Bearer 凭据）：同样不记用户。
func TestPx_GPC_LoggedIn_NoUserAttribution(t *testing.T) {
	skipIfNoConfig(t)
	m := pxMarker(t)
	user := CreateTestUser(t)
	tok := GenerateTestToken(user.ID, "", time.Hour)
	w := pxReq(m, "pricing_view").WithHeader("Sec-GPC", "1").WithBearerToken(tok).Execute(pxRouter())
	assertGIF(t, w)
	assert.Empty(t, w.Header().Values("Set-Cookie"))
	rows := pxRows(t, m)
	require.Len(t, rows, 1)
	assert.Equal(t, "", rows[0].AnonID)
	assert.Equal(t, uint64(0), rows[0].UserID)
}

func TestPx_NearExpiryToken_NoAuthSideEffects(t *testing.T) {
	skipIfNoConfig(t)
	m := pxMarker(t)
	user := CreateTestUser(t)
	tok := GenerateTestToken(user.ID, "", 24*time.Hour) // < 7d renewal threshold
	w := pxReq(m, "pricing_view").WithCookie(CookieAccessToken, tok).Execute(pxRouter())
	assertGIF(t, w)
	for _, c := range w.Result().Cookies() {
		assert.NotEqual(t, CookieAccessToken, c.Name)
		assert.NotEqual(t, CookieCSRFToken, c.Name)
	}
	rows := pxRows(t, m)
	require.Len(t, rows, 1)
	assert.Equal(t, user.ID, rows[0].UserID)
	t.Cleanup(func() { db.Get().Where("user_id = ?", user.ID).Delete(&FunnelIdentity{}) })
}

func TestPx_BadCredentials_Anonymous(t *testing.T) {
	skipIfNoConfig(t)
	cases := map[string]func(*TestRequest) *TestRequest{
		"garbage cookie": func(r *TestRequest) *TestRequest { return r.WithCookie(CookieAccessToken, "not.a.jwt") },
		"basic auth":     func(r *TestRequest) *TestRequest { return r.WithHeader("Authorization", "Basic xyz") },
		"garbage bearer": func(r *TestRequest) *TestRequest { return r.WithBearerToken("zzz") },
	}
	for name, mod := range cases {
		t.Run(name, func(t *testing.T) {
			m := pxMarker(t)
			w := mod(pxReq(m, "pricing_view")).Execute(pxRouter())
			assertGIF(t, w)
			rows := pxRows(t, m)
			require.Len(t, rows, 1)
			assert.Equal(t, uint64(0), rows[0].UserID)
		})
	}
}

func TestPx_BearerWebToken_Resolves(t *testing.T) {
	skipIfNoConfig(t)
	m := pxMarker(t)
	user := CreateTestUser(t)
	t.Cleanup(func() { db.Get().Where("user_id = ?", user.ID).Delete(&FunnelIdentity{}) })
	w := pxReq(m, "pricing_view").WithBearerToken(GenerateTestToken(user.ID, "", time.Hour)).Execute(pxRouter())
	assertGIF(t, w)
	rows := pxRows(t, m)
	require.Len(t, rows, 1)
	assert.Equal(t, user.ID, rows[0].UserID)
}

func TestPx_RefererFallback_BehindRewrite(t *testing.T) {
	skipIfNoConfig(t)
	m := pxMarker(t)
	// Host 是 API 域名，品牌经头到达，Referer 是品牌站点域名。
	w := NewTestRequest("GET", "http://api.internal.test/api/px?e=pricing_view&p="+m).
		WithHeader("User-Agent", pxDesktopUA).WithHeader("X-K2-Brand", "overleap").
		WithHeader("Referer", "https://overleap.io/ja/pricing").Execute(pxRouter())
	assertGIF(t, w)
	rows := pxRows(t, m)
	require.Len(t, rows, 1)
	assert.Equal(t, "/pricing", rows[0].Path)

	m2 := pxMarker(t)
	NewTestRequest("GET", "http://api.internal.test/api/px?e=pricing_view&p="+m2).
		WithHeader("User-Agent", pxDesktopUA).WithHeader("X-K2-Brand", "overleap").
		WithHeader("Referer", "https://evil.example/ja/pricing").Execute(pxRouter())
	rows = pxRows(t, m2)
	require.Len(t, rows, 1)
	assert.Equal(t, "", rows[0].Path)
}

func TestPxOptOut_BehindRewrite(t *testing.T) {
	pxMarker(t)
	r := pxRouter()
	w := NewTestRequest("GET", "http://api.internal.test/api/px/optout").
		WithHeader("X-K2-Brand", "overleap").WithHeader("Referer", "https://www.overleap.io:443/en-GB/privacy").Execute(r)
	assert.Equal(t, "/en-GB/privacy", w.Header().Get("Location"))
	f := NewTestRequest("GET", "http://api.internal.test/api/px/optout").
		WithHeader("X-K2-Brand", "overleap").WithHeader("Referer", "https://kaitu.io/en-GB/privacy").Execute(r)
	assert.Equal(t, "/", f.Header().Get("Location"))
}

func TestFunnelLimiter_WindowReset(t *testing.T) {
	l := newFunnelIPLimiter(2)
	assert.True(t, l.Allow("a"))
	assert.True(t, l.Allow("a"))
	assert.False(t, l.Allow("a"))
	l.buckets["a"].resetAt = time.Now().Add(-time.Second)
	assert.True(t, l.Allow("a"))
}

func TestFunnelLimiter_CapOverflowSharesBucket(t *testing.T) {
	l := newFunnelIPLimiter(2)
	for i := 0; i < funnelLimiterMaxKeys; i++ {
		l.buckets[fmt.Sprintf("k%d", i)] = &ruleMissBucket{resetAt: time.Now().Add(time.Minute), count: 1}
	}
	l.lastSweep = time.Now()
	assert.True(t, l.Allow("new1"))
	assert.True(t, l.Allow("new2"))
	assert.False(t, l.Allow("new3")) // 溢出桶共享限额
	assert.Equal(t, funnelLimiterMaxKeys+1, len(l.buckets))
	assert.True(t, l.Allow("k0")) // 既有 key 不受影响
}

func TestFunnelLimiter_SweepsExpiredOncePerWindow(t *testing.T) {
	l := newFunnelIPLimiter(2)
	l.buckets["old"] = &ruleMissBucket{resetAt: time.Now().Add(-time.Second), count: 1}
	l.lastSweep = time.Now()
	l.Allow("x")
	assert.Contains(t, l.buckets, "old") // 窗口内不清扫
	l.lastSweep = time.Now().Add(-2 * time.Minute)
	l.Allow("y")
	assert.NotContains(t, l.buckets, "old")
}

func TestFunnelPxGlobalCeiling(t *testing.T) {
	funnelPxLimiter.reset()
	funnelPxGlobal.reset()
	t.Cleanup(funnelPxLimiter.reset)
	t.Cleanup(funnelPxGlobal.reset)
	for i := 0; i < funnelPxGlobalPerMin; i++ {
		require.True(t, funnelPxAllow(fmt.Sprintf("ip%d", i)))
	}
	assert.False(t, funnelPxAllow("another"))
}

func pxExpectAnonymous(t *testing.T, m string, w *httptest.ResponseRecorder) {
	t.Helper()
	assertGIF(t, w)
	rows := pxRows(t, m)
	require.Len(t, rows, 1)
	assert.Equal(t, uint64(0), rows[0].UserID)
}

func TestPx_BlockedUser_Anonymous(t *testing.T) {
	skipIfNoConfig(t)
	m := pxMarker(t)
	user := CreateTestUser(t)
	require.NoError(t, db.Get().Model(&User{}).Where("id = ?", user.ID).Update("is_blocked", true).Error)
	sid := newFunnelSid()
	t.Cleanup(func() { db.Get().Where("anon_id = ?", sid).Delete(&FunnelIdentity{}) })
	w := pxReq(m, "pricing_view").WithCookie("sid", sid).
		WithCookie(CookieAccessToken, GenerateTestToken(user.ID, "", time.Hour)).Execute(pxRouter())
	pxExpectAnonymous(t, m, w)
	var n int64
	require.NoError(t, db.Get().Model(&FunnelIdentity{}).Where("anon_id = ?", sid).Count(&n).Error)
	assert.Equal(t, int64(0), n)
}

func TestPx_StaleDeviceToken_Anonymous(t *testing.T) {
	skipIfNoConfig(t)
	m := pxMarker(t)
	user := CreateTestUser(t)
	udid := fmt.Sprintf("px-dev-%d", time.Now().UnixNano())
	dev := CreateTestDevice(t, user.ID, udid)
	tok := GenerateTestToken(user.ID, udid, time.Hour)
	// 有效时应归因；吊销（TokenIssueAt 变化）后匿名。
	require.NoError(t, db.Get().Model(&Device{}).Where("id = ?", dev.ID).Update("token_issue_at", testTokenIssueAt+1).Error)
	w := pxReq(m, "pricing_view").WithCookie(CookieAccessToken, tok).Execute(pxRouter())
	pxExpectAnonymous(t, m, w)
}

func TestPx_NonAccessToken_Anonymous(t *testing.T) {
	skipIfNoConfig(t)
	m := pxMarker(t)
	user := CreateTestUser(t)
	claims := TokenClaims{UserID: user.ID, Exp: time.Now().Add(time.Hour).Unix(), Type: TokenTypeRefresh, TokenIssueAt: testTokenIssueAt}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(configJwt(nil).Secret))
	require.NoError(t, err)
	w := pxReq(m, "pricing_view").WithCookie(CookieAccessToken, tok).Execute(pxRouter())
	pxExpectAnonymous(t, m, w)
}

func TestPx_AccessKeyPresent_Anonymous(t *testing.T) {
	skipIfNoConfig(t)
	m := pxMarker(t)
	user := CreateTestUser(t)
	w := pxReq(m, "pricing_view").WithHeader("X-Access-Key", "k").
		WithBearerToken(GenerateTestToken(user.ID, "", time.Hour)).Execute(pxRouter())
	pxExpectAnonymous(t, m, w)
}

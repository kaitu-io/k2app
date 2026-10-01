package center

import (
	"fmt"
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
	t.Cleanup(funnelPxLimiter.reset)
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

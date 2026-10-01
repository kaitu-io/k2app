package center

import (
	"bytes"
	"encoding/json"
	"net/http"
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

func funnelIdentityRows(t *testing.T, anonID string) []FunnelIdentity {
	t.Helper()
	funnelFlushForTest()
	var rows []FunnelIdentity
	require.NoError(t, db.Get().Where("kind = ? AND anon_id = ?", "sid", anonID).Find(&rows).Error)
	return rows
}

func cleanupFunnelIdentity(t *testing.T, anonID string) {
	t.Cleanup(func() {
		db.Get().Unscoped().Where("kind = ? AND anon_id = ?", "sid", anonID).Delete(&FunnelIdentity{})
	})
}

// 一张能直接走到"缓存 checkout 复用"出口的订单，避免触碰 NextPay。
func payRedirectFixture(t *testing.T) *Order {
	t.Helper()
	skipIfNoDB(t)
	require.NoError(t, Migrate())
	gin.SetMode(gin.TestMode)
	setNextpayReady(t)
	plan := nextpayTestPlan(t)
	user := CreateTestUser(t)
	o := newUnpaidNextpayOrder(t, user, plan)
	require.NoError(t, o.SetOrderCheckout(payRedirectURL(o.UUID), "https://checkout.stripe.com/c/pay/cached", time.Now().Unix()-3600))
	require.NoError(t, db.Get().Save(o).Error)
	return o
}

func payRedirectWithCookie(t *testing.T, o *Order, cookie string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/orders/"+o.UUID+"/pay", nil)
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	payRedirectRouter().ServeHTTP(w, req)
	require.Equal(t, http.StatusFound, w.Code, w.Body.String())
	return w
}

func TestPayRedirect_LinksSidToOrderUser(t *testing.T) {
	o := payRedirectFixture(t)
	sid := newFunnelSid()
	cleanupFunnelIdentity(t, sid)

	payRedirectWithCookie(t, o, "sid="+sid)

	rows := funnelIdentityRows(t, sid)
	require.Len(t, rows, 1)
	assert.Equal(t, o.UserID, rows[0].UserID)
	assert.Equal(t, string(BrandKaitu), rows[0].Brand)
}

func TestPayRedirect_NoSid_SetsOneAndLinks(t *testing.T) {
	o := payRedirectFixture(t)
	w := payRedirectWithCookie(t, o, "")

	var sid string
	for _, ck := range w.Result().Cookies() {
		if ck.Name == CookieFunnelSid {
			sid = ck.Value
		}
	}
	require.True(t, validFunnelSid(sid), "Set-Cookie sid missing or invalid: %v", w.Header()["Set-Cookie"])
	cleanupFunnelIdentity(t, sid)

	rows := funnelIdentityRows(t, sid)
	require.Len(t, rows, 1)
	assert.Equal(t, o.UserID, rows[0].UserID)
}

func TestPayRedirect_OptOut_NoLink(t *testing.T) {
	o := payRedirectFixture(t)
	w := payRedirectWithCookie(t, o, "sid=optout")

	for _, ck := range w.Result().Cookies() {
		assert.NotEqual(t, CookieFunnelSid, ck.Name, "opt-out must not receive a new sid")
	}
	var n int64
	funnelFlushForTest()
	require.NoError(t, db.Get().Model(&FunnelIdentity{}).Where("user_id = ?", o.UserID).Count(&n).Error)
	assert.Zero(t, n)
}

// 登录：走 api_web_auth（mock 验证码）与 api_web_password_login 两条成功路径。
func TestWebLogin_LinksSid(t *testing.T) {
	skipIfNoConfig(t)
	EnableMockVerificationCode = true
	t.Cleanup(func() { EnableMockVerificationCode = false })
	prev := viper.GetBool("mail.dev_mode")
	viper.Set("mail.dev_mode", true)
	t.Cleanup(func() { viper.Set("mail.dev_mode", prev) })

	gpc := false
	post := func(r *gin.Engine, path string, body map[string]string, cookie string) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		if cookie != "" {
			req.Header.Set("Cookie", cookie)
		}
		if gpc {
			req.Header.Set("Sec-GPC", "1")
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	assertOK := func(w *httptest.ResponseRecorder) {
		var resp struct {
			Code int `json:"code"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), w.Body.String())
		require.Equal(t, 0, resp.Code, w.Body.String())
	}
	hasSidCookie := func(w *httptest.ResponseRecorder) bool {
		for _, h := range w.Header().Values("Set-Cookie") {
			if strings.HasPrefix(h, CookieFunnelSid+"=") {
				return true
			}
		}
		return false
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/code", api_web_auth)
	r.POST("/pwd", api_web_password_login)

	t.Run("code login with sid links, never sets cookie", func(t *testing.T) {
		user, email := seedBlockedLoginUser(t, false, "")
		sid := newFunnelSid()
		cleanupFunnelIdentity(t, sid)
		w := post(r, "/code", map[string]string{"email": email, "verificationCode": MockVerificationCode}, "sid="+sid)
		assertOK(w)
		assert.False(t, hasSidCookie(w))
		rows := funnelIdentityRows(t, sid)
		require.Len(t, rows, 1)
		assert.Equal(t, user.ID, rows[0].UserID)
	})

	t.Run("code login without sid links nothing and sets no sid", func(t *testing.T) {
		user, email := seedBlockedLoginUser(t, false, "")
		w := post(r, "/code", map[string]string{"email": email, "verificationCode": MockVerificationCode}, "")
		assertOK(w)
		assert.False(t, hasSidCookie(w))
		funnelFlushForTest()
		var n int64
		require.NoError(t, db.Get().Model(&FunnelIdentity{}).Where("user_id = ?", user.ID).Count(&n).Error)
		assert.Zero(t, n)
	})

	t.Run("code login with optout links nothing", func(t *testing.T) {
		user, email := seedBlockedLoginUser(t, false, "")
		w := post(r, "/code", map[string]string{"email": email, "verificationCode": MockVerificationCode}, "sid=optout")
		assertOK(w)
		funnelFlushForTest()
		var n int64
		require.NoError(t, db.Get().Model(&FunnelIdentity{}).Where("user_id = ?", user.ID).Count(&n).Error)
		assert.Zero(t, n)
	})

	t.Run("password login with sid links", func(t *testing.T) {
		const pw = "k7N#mq2P!xT9"
		user, email := seedBlockedLoginUser(t, false, pw)
		sid := newFunnelSid()
		cleanupFunnelIdentity(t, sid)
		w := post(r, "/pwd", map[string]string{"email": email, "password": pw}, "sid="+sid)
		assertOK(w)
		rows := funnelIdentityRows(t, sid)
		require.Len(t, rows, 1)
		assert.Equal(t, user.ID, rows[0].UserID)
	})

	t.Run("GPC code login with valid sid links nothing", func(t *testing.T) {
		gpc = true
		t.Cleanup(func() { gpc = false })
		user, email := seedBlockedLoginUser(t, false, "")
		sid := newFunnelSid()
		cleanupFunnelIdentity(t, sid)
		w := post(r, "/code", map[string]string{"email": email, "verificationCode": MockVerificationCode}, "sid="+sid)
		assertOK(w)
		assert.False(t, hasSidCookie(w))
		assert.Empty(t, funnelIdentityRows(t, sid))
		var n int64
		require.NoError(t, db.Get().Model(&FunnelIdentity{}).Where("user_id = ?", user.ID).Count(&n).Error)
		assert.Zero(t, n)
	})

	t.Run("GPC password login with valid sid links nothing", func(t *testing.T) {
		gpc = true
		t.Cleanup(func() { gpc = false })
		const pw = "k7N#mq2P!xT9"
		_, email := seedBlockedLoginUser(t, false, pw)
		sid := newFunnelSid()
		cleanupFunnelIdentity(t, sid)
		w := post(r, "/pwd", map[string]string{"email": email, "password": pw}, "sid="+sid)
		assertOK(w)
		assert.Empty(t, funnelIdentityRows(t, sid))
	})
}

func payRedirectGPC(t *testing.T, o *Order, cookie string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/orders/"+o.UUID+"/pay", nil)
	req.Header.Set("Sec-GPC", "1")
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	payRedirectRouter().ServeHTTP(w, req)
	require.Equal(t, http.StatusFound, w.Code, w.Body.String())
	return w
}

func TestPayRedirect_GPC_NoCookie_NoSidNoLink(t *testing.T) {
	o := payRedirectFixture(t)
	w := payRedirectGPC(t, o, "")
	for _, ck := range w.Result().Cookies() {
		assert.NotEqual(t, CookieFunnelSid, ck.Name)
	}
	funnelFlushForTest()
	var n int64
	require.NoError(t, db.Get().Model(&FunnelIdentity{}).Where("user_id = ?", o.UserID).Count(&n).Error)
	assert.Zero(t, n)
}

func TestPayRedirect_GPC_ExistingSid_NoLink(t *testing.T) {
	o := payRedirectFixture(t)
	sid := newFunnelSid()
	cleanupFunnelIdentity(t, sid)
	payRedirectGPC(t, o, "sid="+sid)
	assert.Empty(t, funnelIdentityRows(t, sid))
}

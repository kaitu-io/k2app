package center

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// POST /api/auth/social/web-login 集成测试：真实路由 + 真库（0 SKIP）。

type socialWebLoginResp struct {
	Code int                  `json:"code"`
	Data DataWebLoginResponse `json:"data"`
}

func postSocialWebLogin(t *testing.T, host string, brand Brand, body map[string]string) (*httptest.ResponseRecorder, socialWebLoginResp) {
	t.Helper()
	r := SetupTestRouter()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/social/web-login", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if host != "" {
		req.Host = host
	} else {
		req.Header.Set("X-K2-Brand", string(brand))
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var resp socialWebLoginResp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), "body=%s", w.Body.String())
	return w, resp
}

func TestSocialWebLogin_SetsCookies(t *testing.T) {
	skipIfNoConfig(t)
	key := newTestRSAKey(t)
	installTestSocialKeys(t, key)
	enableTestSocialAuth(t, BrandKaitu)
	tr := newSocialTrack(t)
	sfx := socialTestSuffix()

	email := "swl-g-" + sfx + "@example.com"
	u := tr.seedUser(BrandKaitu, email)
	sub := tr.sub(SocialProviderGoogle, "swl-"+sfx)

	cl := googleTestClaims(BrandKaitu.Config().Social.GoogleWebClientID)
	cl["sub"] = sub
	cl["email"] = email
	w, resp := postSocialWebLogin(t, "", BrandKaitu, map[string]string{
		"provider": SocialProviderGoogle,
		"idToken":  signTestIDToken(t, key, cl),
		"nonce":    testSocialRawNonce,
	})
	require.Equal(t, 0, resp.Code, "body=%s", w.Body.String())
	assert.Equal(t, u.ID, resp.Data.User.ID)
	assert.Equal(t, email, resp.Data.User.Email)
	assert.NotEmpty(t, resp.Data.AccessToken)

	cookies := map[string]string{}
	for _, ck := range w.Result().Cookies() {
		cookies[ck.Name] = ck.Value
	}
	assert.NotEmpty(t, cookies[CookieAccessToken], "access_token cookie")
	assert.NotEmpty(t, cookies[CookieCSRFToken], "csrf_token cookie")
}

func TestSocialWebLogin_CrossBrandHost(t *testing.T) {
	skipIfNoConfig(t)
	key := newTestRSAKey(t)
	installTestSocialKeys(t, key)
	enableTestSocialAuth(t, BrandKaitu)
	enableTestSocialAuth(t, BrandOverleap)
	sfx := socialTestSuffix()

	// kaitu 的 aud，打到 overleap 的 Host → aud 不在 overleap 列表 → 拒绝。
	cl := googleTestClaims(BrandKaitu.Config().Social.GoogleWebClientID)
	cl["sub"] = "swl-x-" + sfx
	cl["email"] = "swl-x-" + sfx + "@example.com"
	w, resp := postSocialWebLogin(t, "www.overleap.io", "", map[string]string{
		"provider": SocialProviderGoogle,
		"idToken":  signTestIDToken(t, key, cl),
		"nonce":    testSocialRawNonce,
	})
	assert.Equal(t, int(ErrorSocialTokenInvalid), resp.Code, "body=%s", w.Body.String())
	for _, ck := range w.Result().Cookies() {
		assert.NotEqual(t, CookieAccessToken, ck.Name)
	}
}

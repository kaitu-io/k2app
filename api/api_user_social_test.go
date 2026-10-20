package center

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// /api/user/social 绑定 / 解绑 / 列表集成测试：经 SetupTestRouter() 打真实路由、真库（0 SKIP）。
// 网络调用（Apple revoke）用桩替换，不打外网。

type userSocialEnv struct {
	t     *testing.T
	key   *rsa.PrivateKey
	tr    *socialTrack
	sfx   string
	user  *User
	token string
}

func newUserSocialEnv(t *testing.T) *userSocialEnv {
	t.Helper()
	skipIfNoConfig(t)
	key := newTestRSAKey(t)
	installTestSocialKeys(t, key)
	enableTestSocialAuth(t, BrandKaitu)
	tr := newSocialTrack(t)
	sfx := socialTestSuffix()
	u := tr.seedUser(BrandKaitu, "")
	udid := "udid-us-" + sfx
	CreateTestDevice(t, u.ID, udid)
	return &userSocialEnv{t: t, key: key, tr: tr, sfx: sfx, user: u, token: GenerateTestToken(u.ID, udid, time.Hour)}
}

func (e *userSocialEnv) googleToken(sub, email string) string {
	cl := googleTestClaims(BrandKaitu.Config().Social.GoogleWebClientID)
	cl["sub"] = sub
	cl["email"] = email
	return signTestIDToken(e.t, e.key, cl)
}

func (e *userSocialEnv) bindBody(provider, idToken string) map[string]string {
	return map[string]string{"provider": provider, "idToken": idToken, "nonce": testSocialRawNonce}
}

func (e *userSocialEnv) do(method, path string, body any) *TestResponse {
	e.t.Helper()
	w := NewTestRequest(method, path).WithBody(body).WithBearerToken(e.token).Execute(SetupTestRouter())
	resp, err := ParseResponse(w)
	require.NoError(e.t, err, "body=%s", w.Body.String())
	return resp
}

func stubAppleRevoke(t *testing.T, fn func(ctx context.Context, cfg socialAuthConfig, clientID, refreshToken string) error) {
	t.Helper()
	orig := appleRevokeToken
	appleRevokeToken = fn
	t.Cleanup(func() { appleRevokeToken = orig })
}

func TestUserSocial_ListMasksEmail(t *testing.T) {
	e := newUserSocialEnv(t)
	email := e.tr.email("us-list-" + e.sfx + "@example.com")
	sub := "g-list-" + e.sfx

	resp := e.do("POST", "/api/user/social/bind", e.bindBody(SocialProviderGoogle, e.googleToken(sub, email)))
	require.Equal(t, 0, resp.Code, resp.Message)

	list := e.do("GET", "/api/user/social", nil)
	require.Equal(t, 0, list.Code)
	var data struct {
		Items []DataSocialIdentity `json:"items"`
	}
	require.NoError(t, json.Unmarshal(list.Data, &data))
	require.Len(t, data.Items, 1)
	assert.Equal(t, SocialProviderGoogle, data.Items[0].Provider)
	assert.Equal(t, hideEmail(email), data.Items[0].Email)
	assert.NotEqual(t, email, data.Items[0].Email, "邮箱必须脱敏")
}

func TestUserSocial_EmailIdentityNotListed(t *testing.T) {
	e := newUserSocialEnv(t)
	email := e.tr.email("us-emailrow-" + e.sfx + "@example.com")
	// 绑定可信邮箱会同事务写入 email 身份行
	resp := e.do("POST", "/api/user/social/bind", e.bindBody(SocialProviderGoogle, e.googleToken("g-er-"+e.sfx, email)))
	require.Equal(t, 0, resp.Code, resp.Message)
	require.Equal(t, 1, identitiesOfUser(t, e.user.ID)["email"], "前置：email 身份行存在")

	list := e.do("GET", "/api/user/social", nil)
	var data struct {
		Items []DataSocialIdentity `json:"items"`
	}
	require.NoError(t, json.Unmarshal(list.Data, &data))
	for _, it := range data.Items {
		assert.NotEqual(t, "email", it.Provider)
	}
	assert.Len(t, data.Items, 1)
}

func TestUserSocial_BindConflict(t *testing.T) {
	e := newUserSocialEnv(t)
	other := e.tr.seedUser(BrandKaitu, "")
	sub := "g-conf-" + e.sfx
	e.tr.seedIdentity(BrandKaitu, other.ID, SocialProviderGoogle, sub)

	resp := e.do("POST", "/api/user/social/bind", e.bindBody(SocialProviderGoogle, e.googleToken(sub, "")))
	assert.Equal(t, int(ErrorSocialIdentityTaken), resp.Code)
	assert.Zero(t, identitiesOfUser(t, e.user.ID)[SocialProviderGoogle])
}

func TestUserSocial_BindSecondGoogle(t *testing.T) {
	e := newUserSocialEnv(t)
	e.tr.seedIdentity(BrandKaitu, e.user.ID, SocialProviderGoogle, "g-first-"+e.sfx)

	resp := e.do("POST", "/api/user/social/bind", e.bindBody(SocialProviderGoogle, e.googleToken("g-second-"+e.sfx, "")))
	assert.Equal(t, int(ErrorSocialProviderAlreadyBound), resp.Code)
	assert.Equal(t, 1, identitiesOfUser(t, e.user.ID)[SocialProviderGoogle])
}

func TestUserSocial_UnbindAppleRevokes(t *testing.T) {
	e := newUserSocialEnv(t)
	sub := "a-unbind-" + e.sfx
	e.tr.seedIdentity(BrandKaitu, e.user.ID, SocialProviderApple, sub)
	aud := BrandKaitu.Config().Social.AppleBundleID
	require.NoError(t, saveAppleRefreshToken(context.Background(), BrandKaitu, sub, aud, "apple-rt-"+e.sfx))

	var mu sync.Mutex
	done := make(chan struct{}, 1)
	var gotClientID, gotToken string
	var gotBrand Brand
	stubAppleRevoke(t, func(_ context.Context, cfg socialAuthConfig, clientID, refreshToken string) error {
		mu.Lock()
		gotClientID, gotToken, gotBrand = clientID, refreshToken, cfg.Brand
		mu.Unlock()
		done <- struct{}{}
		return nil
	})

	resp := e.do("DELETE", "/api/user/social/apple", nil)
	require.Equal(t, 0, resp.Code, resp.Message)
	assert.Zero(t, identitiesOfUser(t, e.user.ID)[SocialProviderApple], "身份行已删除")

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("appleRevokeToken 未被调用")
	}
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, aud, gotClientID)
	assert.Equal(t, "apple-rt-"+e.sfx, gotToken)
	assert.Equal(t, BrandKaitu, gotBrand)
}

func TestUserSocial_UnbindGoogleDoesNotRevoke(t *testing.T) {
	e := newUserSocialEnv(t)
	e.tr.seedIdentity(BrandKaitu, e.user.ID, SocialProviderGoogle, "g-nr-"+e.sfx)
	called := make(chan struct{}, 1)
	stubAppleRevoke(t, func(context.Context, socialAuthConfig, string, string) error {
		called <- struct{}{}
		return nil
	})
	resp := e.do("DELETE", "/api/user/social/google", nil)
	require.Equal(t, 0, resp.Code, resp.Message)
	select {
	case <-called:
		t.Fatal("Google 解绑不应调用 Apple revoke")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestUserSocial_UnbindNotBound(t *testing.T) {
	e := newUserSocialEnv(t)
	resp := e.do("DELETE", "/api/user/social/google", nil)
	assert.Equal(t, int(ErrorNotFound), resp.Code)
}

func TestUserSocial_UnbindUnknownProvider(t *testing.T) {
	e := newUserSocialEnv(t)
	resp := e.do("DELETE", "/api/user/social/email", nil)
	assert.Equal(t, int(ErrorInvalidArgument), resp.Code)
}

func TestUserSocial_BindViaWebCookie(t *testing.T) {
	e := newUserSocialEnv(t)
	email := e.tr.email("us-cookie-" + e.sfx + "@example.com")
	body := e.bindBody(SocialProviderGoogle, e.googleToken("g-ck-"+e.sfx, email))

	// 官网账号页：access_token cookie + CSRF，无 X-K2-Client、无 Bearer
	w := NewTestRequest("POST", "/api/user/social/bind").
		WithBody(body).
		WithCookie(CookieAccessToken, GenerateTestToken(e.user.ID, "", time.Hour)).
		WithCSRFToken("csrf-" + e.sfx).
		Execute(SetupTestRouter())
	resp := mustParse(t, w)
	require.Equal(t, 0, resp.Code, resp.Message)
	assert.Equal(t, 1, identitiesOfUser(t, e.user.ID)[SocialProviderGoogle])

	var n int64
	require.NoError(t, db.Get().Model(&LoginIdentify{}).Where("user_id = ? AND type = ?", e.user.ID, SocialProviderGoogle).Count(&n).Error)
	assert.EqualValues(t, 1, n)
}

func mustParse(t *testing.T, w *httptest.ResponseRecorder) *TestResponse {
	t.Helper()
	resp, err := ParseResponse(w)
	require.NoError(t, err, "body=%s", w.Body.String())
	return resp
}

package center

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// POST /api/auth/social/login 集成测试：经 SetupTestRouter() 打真实路由、真库（0 SKIP）。
// 验签密钥用 installTestSocialKeys 注入；Apple code 交换用桩替换，不打外网。

type socialLoginResp struct {
	Code int            `json:"code"`
	Data DataAuthResult `json:"data"`
}

// enableTestSocialAuth 打开 brand 的 Google / Apple 开关（Apple 私钥为测试 EC 密钥），测试结束还原。
func enableTestSocialAuth(t *testing.T, b Brand) {
	t.Helper()
	prefix := "social_auth." + string(b) + "."
	keys := map[string]any{
		prefix + "google.enabled":    true,
		prefix + "apple.key_id":      "TESTKEYID",
		prefix + "apple.private_key": newTestAppleP8(t),
	}
	for k, v := range keys {
		orig, had := viper.Get(k), viper.IsSet(k)
		viper.Set(k, v)
		t.Cleanup(func() {
			if had {
				viper.Set(k, orig)
			} else {
				viper.Set(k, nil)
			}
		})
	}
}

func newTestAppleP8(t *testing.T) string {
	t.Helper()
	pemStr, _ := newTestAppleKey(t)
	return pemStr
}

// newTestAppleKey 生成 P-256 私钥，按 Apple .p8 的格式（PKCS#8 PEM）编码。
func newTestAppleKey(t *testing.T) (string, *ecdsa.PrivateKey) {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(k)
	require.NoError(t, err)
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})), k
}

func stubAppleExchangeCode(t *testing.T, fn func(ctx context.Context, cfg socialAuthConfig, clientID, code, redirectURI string) (string, error)) {
	t.Helper()
	orig := appleExchangeCode
	appleExchangeCode = fn
	t.Cleanup(func() { appleExchangeCode = orig })
}

func trackSocialLoginDevice(t *testing.T, udid string) string {
	t.Helper()
	t.Cleanup(func() { db.Get().Unscoped().Where("udid = ?", udid).Delete(&Device{}) })
	return udid
}

func postSocialLogin(t *testing.T, brand Brand, body map[string]string) socialLoginResp {
	t.Helper()
	r := SetupTestRouter()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/social/login", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-K2-Brand", string(brand))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var resp socialLoginResp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), "body=%s", w.Body.String())
	return resp
}

func TestSocialLogin_GoogleMatchesExistingAccount(t *testing.T) {
	skipIfNoConfig(t)
	key := newTestRSAKey(t)
	installTestSocialKeys(t, key)
	enableTestSocialAuth(t, BrandKaitu)
	tr := newSocialTrack(t)
	sfx := socialTestSuffix()

	email := "sl-g-" + sfx + "@example.com"
	u := tr.seedUser(BrandKaitu, email)
	sub := tr.sub(SocialProviderGoogle, "g-"+sfx)
	udid := trackSocialLoginDevice(t, "udid-sl-g-"+sfx)

	cl := googleTestClaims(BrandKaitu.Config().Social.GoogleWebClientID)
	cl["sub"] = sub
	cl["email"] = email
	resp := postSocialLogin(t, BrandKaitu, map[string]string{
		"provider": SocialProviderGoogle,
		"idToken":  signTestIDToken(t, key, cl),
		"nonce":    testSocialRawNonce,
		"udid":     udid,
		"remark":   "test device",
	})
	require.Equal(t, 0, resp.Code)
	assert.NotEmpty(t, resp.Data.AccessToken)
	assert.NotEmpty(t, resp.Data.RefreshToken)

	var devices []Device
	require.NoError(t, db.Get().Where("udid = ?", udid).Find(&devices).Error)
	require.Len(t, devices, 1)
	assert.Equal(t, u.ID, devices[0].UserID)
	assert.Equal(t, 1, identitiesOfUser(t, u.ID)[SocialProviderGoogle], "google 身份已补写到老用户")
}

func TestSocialLogin_ProviderDisabled(t *testing.T) {
	skipIfNoConfig(t)
	key := newTestRSAKey(t)
	installTestSocialKeys(t, key)
	// 显式关闭 Google（不依赖 config.yml 的实际值）
	k := "social_auth." + string(BrandKaitu) + ".google.enabled"
	orig, had := viper.Get(k), viper.IsSet(k)
	viper.Set(k, false)
	t.Cleanup(func() {
		if had {
			viper.Set(k, orig)
		} else {
			viper.Set(k, nil)
		}
	})

	resp := postSocialLogin(t, BrandKaitu, map[string]string{
		"provider": SocialProviderGoogle,
		"idToken":  signTestIDToken(t, key, googleTestClaims(BrandKaitu.Config().Social.GoogleWebClientID)),
		"nonce":    testSocialRawNonce,
		"udid":     "udid-sl-disabled-" + socialTestSuffix(),
	})
	assert.Equal(t, int(ErrorSocialProviderDisabled), resp.Code)
}

func TestSocialLogin_BlockedUser(t *testing.T) {
	skipIfNoConfig(t)
	key := newTestRSAKey(t)
	installTestSocialKeys(t, key)
	enableTestSocialAuth(t, BrandKaitu)
	tr := newSocialTrack(t)
	sfx := socialTestSuffix()

	u := tr.seedUser(BrandKaitu, "")
	require.NoError(t, db.Get().Model(&User{}).Where("id = ?", u.ID).Update("is_blocked", true).Error)
	tr.seedIdentity(BrandKaitu, u.ID, SocialProviderGoogle, "g-"+sfx)
	udid := trackSocialLoginDevice(t, "udid-sl-blocked-"+sfx)

	cl := googleTestClaims(BrandKaitu.Config().Social.GoogleWebClientID)
	cl["sub"] = "g-" + sfx
	resp := postSocialLogin(t, BrandKaitu, map[string]string{
		"provider": SocialProviderGoogle,
		"idToken":  signTestIDToken(t, key, cl),
		"nonce":    testSocialRawNonce,
		"udid":     udid,
	})
	assert.Equal(t, int(ErrorForbidden), resp.Code)
	var n int64
	require.NoError(t, db.Get().Model(&Device{}).Where("udid = ?", udid).Count(&n).Error)
	assert.Zero(t, n, "被封禁用户不应留下设备行")
}

func TestSocialLogin_AppleStoresRefreshToken(t *testing.T) {
	skipIfNoConfig(t)
	key := newTestRSAKey(t)
	installTestSocialKeys(t, key)
	enableTestSocialAuth(t, BrandOverleap)
	tr := newSocialTrack(t)
	sfx := socialTestSuffix()

	sub := tr.sub(SocialProviderApple, "a-"+sfx)
	email := tr.email("sl-a-" + sfx + "@privaterelay.appleid.com")
	udid := trackSocialLoginDevice(t, "udid-sl-a-"+sfx)
	aud := BrandOverleap.Config().Social.AppleBundleID

	var gotClientID, gotCode, gotRedirect string
	stubAppleExchangeCode(t, func(_ context.Context, cfg socialAuthConfig, clientID, code, redirectURI string) (string, error) {
		assert.Equal(t, BrandOverleap, cfg.Brand)
		gotClientID, gotCode, gotRedirect = clientID, code, redirectURI
		return "apple-rt-" + sfx, nil
	})

	cl := appleTestClaims(aud)
	cl["sub"] = sub
	cl["email"] = email
	resp := postSocialLogin(t, BrandOverleap, map[string]string{
		"provider":          SocialProviderApple,
		"idToken":           signTestIDToken(t, key, cl),
		"nonce":             testSocialRawNonce,
		"authorizationCode": "code-" + sfx,
		"redirectURI":       "https://overleap.io/auth/social/apple",
		"udid":              udid,
	})
	require.Equal(t, 0, resp.Code)
	assert.Equal(t, aud, gotClientID)
	assert.Equal(t, "code-"+sfx, gotCode)
	assert.Equal(t, "https://overleap.io/auth/social/apple", gotRedirect)

	row, err := findSocialIdentity(context.Background(), db.Get(), BrandOverleap, SocialProviderApple, sub)
	require.NoError(t, err)
	assert.Equal(t, aud, row.ProviderClientID)
	plain, err := secretDecryptString(context.Background(), row.ProviderRefreshToken)
	require.NoError(t, err)
	assert.Equal(t, "apple-rt-"+sfx, plain)
}

func TestSocialLogin_AppleCodeExchangeFailureDoesNotBlock(t *testing.T) {
	skipIfNoConfig(t)
	key := newTestRSAKey(t)
	installTestSocialKeys(t, key)
	enableTestSocialAuth(t, BrandKaitu)
	tr := newSocialTrack(t)
	sfx := socialTestSuffix()

	sub := tr.sub(SocialProviderApple, "a-"+sfx)
	email := tr.email("sl-af-" + sfx + "@privaterelay.appleid.com")
	udid := trackSocialLoginDevice(t, "udid-sl-af-"+sfx)

	stubAppleExchangeCode(t, func(context.Context, socialAuthConfig, string, string, string) (string, error) {
		return "", errors.New("apple down")
	})

	cl := appleTestClaims(BrandKaitu.Config().Social.AppleBundleID)
	cl["sub"] = sub
	cl["email"] = email
	resp := postSocialLogin(t, BrandKaitu, map[string]string{
		"provider":          SocialProviderApple,
		"idToken":           signTestIDToken(t, key, cl),
		"nonce":             testSocialRawNonce,
		"authorizationCode": "code-" + sfx,
		"udid":              udid,
	})
	require.Equal(t, 0, resp.Code)
	assert.NotEmpty(t, resp.Data.AccessToken)

	row, err := findSocialIdentity(context.Background(), db.Get(), BrandKaitu, SocialProviderApple, sub)
	require.NoError(t, err)
	assert.Empty(t, row.ProviderRefreshToken)
}

func TestAppleClientSecret_Claims(t *testing.T) {
	pemStr, priv := newTestAppleKey(t)
	cfg := socialAuthConfig{
		Brand: BrandKaitu,
		IDs:   BrandKaitu.Config().Social,
		Apple: socialAppleSecret{TeamID: "NJT954Q3RH", KeyID: "KEY123", PrivateKeyPEM: pemStr},
	}
	now := time.Unix(1_700_000_000, 0)
	s, err := appleClientSecret(cfg, "io.kaitu.signin", now)
	require.NoError(t, err)

	var rc jwt.RegisteredClaims
	tok, err := jwt.ParseWithClaims(s, &rc, func(*jwt.Token) (any, error) { return &priv.PublicKey, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodES256.Alg()}),
		jwt.WithTimeFunc(func() time.Time { return now }),
	)
	require.NoError(t, err)
	assert.Equal(t, "KEY123", tok.Header["kid"])
	assert.Equal(t, "NJT954Q3RH", rc.Issuer)
	assert.Equal(t, "io.kaitu.signin", rc.Subject)
	assert.Equal(t, jwt.ClaimStrings{"https://appleid.apple.com"}, rc.Audience)
	require.NotNil(t, rc.IssuedAt)
	assert.Equal(t, now.Unix(), rc.IssuedAt.Unix())
	require.NotNil(t, rc.ExpiresAt)
	assert.Equal(t, now.Add(5*time.Minute).Unix(), rc.ExpiresAt.Unix())

	_, err = appleClientSecret(socialAuthConfig{Apple: socialAppleSecret{PrivateKeyPEM: "not a pem"}}, "x", now)
	assert.Error(t, err)
}

func TestSocialErrorCode(t *testing.T) {
	cases := map[error]ErrorCode{
		errSocialTokenInvalid:         ErrorSocialTokenInvalid,
		errSocialEmailUnverified:      ErrorSocialEmailUnverified,
		errSocialProviderDisabled:     ErrorSocialProviderDisabled,
		errSocialIdentityTaken:        ErrorSocialIdentityTaken,
		errSocialProviderAlreadyBound: ErrorSocialProviderAlreadyBound,
		errors.New("boom"):            ErrorSystemError,
	}
	for err, want := range cases {
		assert.Equal(t, want, socialErrorCode(err), "%v", err)
		assert.Equal(t, want, socialErrorCode(errors.Join(errors.New("ctx"), err)), "wrapped %v", err)
	}
}

func TestSocialLogin_AppleUnregisteredRedirectURIRejected(t *testing.T) {
	skipIfNoConfig(t)
	key := newTestRSAKey(t)
	installTestSocialKeys(t, key)
	enableTestSocialAuth(t, BrandKaitu)

	called := false
	stubAppleExchangeCode(t, func(context.Context, socialAuthConfig, string, string, string) (string, error) {
		called = true
		return "rt", nil
	})
	cl := appleTestClaims(BrandKaitu.Config().Social.AppleServicesID)
	// 另一品牌的已登记地址对本品牌也无效
	for _, bad := range []string{"https://evil.example/cb", "https://overleap.io/auth/social/apple"} {
		resp := postSocialLogin(t, BrandKaitu, map[string]string{
			"provider":          SocialProviderApple,
			"idToken":           signTestIDToken(t, key, cl),
			"nonce":             testSocialRawNonce,
			"authorizationCode": "code-x",
			"redirectURI":       bad,
			"udid":              "udid-sl-redir-reject",
		})
		assert.Equal(t, int(ErrorInvalidArgument), resp.Code, bad)
	}
	assert.False(t, called)
}

func TestValidAppleRedirectURI(t *testing.T) {
	assert.True(t, validAppleRedirectURI(BrandKaitu, SocialProviderApple, "c", ""))
	assert.True(t, validAppleRedirectURI(BrandKaitu, SocialProviderApple, "c", "https://kaitu.io/auth/social/apple"))
	assert.True(t, validAppleRedirectURI(BrandKaitu, SocialProviderApple, "c", "https://www.kaitu.io/auth/social/apple"))
	assert.False(t, validAppleRedirectURI(BrandKaitu, SocialProviderApple, "c", "https://overleap.io/auth/social/apple"))
	assert.True(t, validAppleRedirectURI(BrandOverleap, SocialProviderApple, "c", "https://www.overleap.io/auth/social/apple"))
	assert.False(t, validAppleRedirectURI(BrandOverleap, SocialProviderApple, "c", "http://overleap.io/auth/social/apple"))
	// 无授权码 / 非 Apple 不检查
	assert.True(t, validAppleRedirectURI(BrandKaitu, SocialProviderApple, "", "https://evil.example"))
	assert.True(t, validAppleRedirectURI(BrandKaitu, SocialProviderGoogle, "c", "https://evil.example"))
}

func TestAppleExchangeCode_RedirectURI(t *testing.T) {
	pemKey, _ := newTestAppleKey(t)
	cfg := socialAuthConfig{Brand: BrandKaitu}
	cfg.Apple.PrivateKeyPEM = pemKey
	cfg.Apple.TeamID, cfg.Apple.KeyID = "TEAM", "KEY"

	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		got = r.PostForm
		_, _ = w.Write([]byte(`{"refresh_token":"rt"}`))
	}))
	defer srv.Close()
	orig := appleTokenURL
	appleTokenURL = srv.URL
	t.Cleanup(func() { appleTokenURL = orig })

	rt, err := appleExchangeCode(context.Background(), cfg, "io.kaitu.signin", "code1", "https://kaitu.io/auth/social/apple")
	require.NoError(t, err)
	assert.Equal(t, "rt", rt)
	assert.Equal(t, "https://kaitu.io/auth/social/apple", got.Get("redirect_uri"))
	assert.Equal(t, "code1", got.Get("code"))

	_, err = appleExchangeCode(context.Background(), cfg, "com.x.ios", "code2", "")
	require.NoError(t, err)
	_, has := got["redirect_uri"]
	assert.False(t, has)
}

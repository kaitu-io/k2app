package center

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testSocialRawNonce = "raw-nonce-for-tests"

func newTestRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	return k
}

// installTestSocialKeys 让两家提供方都用 key 的公钥验签，测试结束还原。
func installTestSocialKeys(t *testing.T, key *rsa.PrivateKey) {
	t.Helper()
	orig := socialKeyfunc
	socialKeyfunc = map[string]jwt.Keyfunc{
		SocialProviderGoogle: func(*jwt.Token) (any, error) { return &key.PublicKey, nil },
		SocialProviderApple:  func(*jwt.Token) (any, error) { return &key.PublicKey, nil },
	}
	t.Cleanup(func() { socialKeyfunc = orig })
}

func signTestIDToken(t *testing.T, key *rsa.PrivateKey, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = "test-kid"
	s, err := tok.SignedString(key)
	require.NoError(t, err)
	return s
}

func readySocialConfig(b Brand) socialAuthConfig {
	return socialAuthConfig{
		Brand:         b,
		IDs:           b.Config().Social,
		GoogleEnabled: true,
		Apple:         socialAppleSecret{TeamID: "T", KeyID: "K", PrivateKeyPEM: "pem"},
	}
}

func googleTestClaims(aud string) jwt.MapClaims {
	now := time.Now()
	return jwt.MapClaims{
		"iss":            "https://accounts.google.com",
		"aud":            aud,
		"sub":            "google-sub-123",
		"email":          "  Alice@Example.COM ",
		"email_verified": true,
		"nonce":          socialNonceHash(testSocialRawNonce),
		"iat":            now.Unix(),
		"exp":            now.Add(time.Hour).Unix(),
	}
}

func appleTestClaims(aud string) jwt.MapClaims {
	now := time.Now()
	return jwt.MapClaims{
		"iss":            "https://appleid.apple.com",
		"aud":            aud,
		"sub":            "001234.apple-sub.0567",
		"email":          "relay@privaterelay.appleid.com",
		"email_verified": true,
		"nonce":          socialNonceHash(testSocialRawNonce),
		"iat":            now.Unix(),
		"exp":            now.Add(time.Hour).Unix(),
	}
}

func TestSocialNonceHash(t *testing.T) {
	// sha256("abc")
	assert.Equal(t, "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad", socialNonceHash("abc"))
}

func TestVerifySocialIDToken_Valid(t *testing.T) {
	key := newTestRSAKey(t)
	installTestSocialKeys(t, key)
	cfg := readySocialConfig(BrandKaitu)

	t.Run("google web aud", func(t *testing.T) {
		aud := cfg.IDs.GoogleWebClientID
		c, err := verifySocialIDToken(context.Background(), cfg, SocialProviderGoogle, signTestIDToken(t, key, googleTestClaims(aud)), testSocialRawNonce)
		require.NoError(t, err)
		assert.Equal(t, SocialProviderGoogle, c.Provider)
		assert.Equal(t, "google-sub-123", c.Sub)
		assert.Equal(t, "alice@example.com", c.Email)
		assert.True(t, c.EmailTrusted)
		assert.Equal(t, aud, c.Audience)
	})

	t.Run("google ios aud, bare issuer", func(t *testing.T) {
		aud := cfg.IDs.GoogleIOSClientID
		cl := googleTestClaims(aud)
		cl["iss"] = "accounts.google.com"
		cl["email_verified"] = "true"
		c, err := verifySocialIDToken(context.Background(), cfg, SocialProviderGoogle, signTestIDToken(t, key, cl), testSocialRawNonce)
		require.NoError(t, err)
		assert.Equal(t, aud, c.Audience)
		assert.True(t, c.EmailTrusted)
	})

	t.Run("apple bundle aud", func(t *testing.T) {
		aud := cfg.IDs.AppleBundleID
		c, err := verifySocialIDToken(context.Background(), cfg, SocialProviderApple, signTestIDToken(t, key, appleTestClaims(aud)), testSocialRawNonce)
		require.NoError(t, err)
		assert.Equal(t, SocialProviderApple, c.Provider)
		assert.Equal(t, "001234.apple-sub.0567", c.Sub)
		assert.Equal(t, "relay@privaterelay.appleid.com", c.Email)
		assert.True(t, c.EmailTrusted)
		assert.Equal(t, aud, c.Audience)
	})

	t.Run("apple services aud (array), no email", func(t *testing.T) {
		aud := cfg.IDs.AppleServicesID
		cl := appleTestClaims("")
		cl["aud"] = []string{aud}
		delete(cl, "email")
		delete(cl, "email_verified")
		c, err := verifySocialIDToken(context.Background(), cfg, SocialProviderApple, signTestIDToken(t, key, cl), testSocialRawNonce)
		require.NoError(t, err)
		assert.Equal(t, aud, c.Audience)
		assert.Equal(t, "", c.Email)
		assert.False(t, c.EmailTrusted)
	})
}

func assertSocialInvalid(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	assert.True(t, errors.Is(err, errSocialTokenInvalid), "want errSocialTokenInvalid, got %v", err)
}

func TestVerifySocialIDToken_WrongSignature(t *testing.T) {
	key := newTestRSAKey(t)
	installTestSocialKeys(t, key)
	cfg := readySocialConfig(BrandKaitu)
	other := newTestRSAKey(t)
	tok := signTestIDToken(t, other, googleTestClaims(cfg.IDs.GoogleWebClientID))
	_, err := verifySocialIDToken(context.Background(), cfg, SocialProviderGoogle, tok, testSocialRawNonce)
	assertSocialInvalid(t, err)
}

func TestVerifySocialIDToken_WrongIssuer(t *testing.T) {
	key := newTestRSAKey(t)
	installTestSocialKeys(t, key)
	cfg := readySocialConfig(BrandKaitu)

	cl := googleTestClaims(cfg.IDs.GoogleWebClientID)
	cl["iss"] = "https://evil.example.com"
	_, err := verifySocialIDToken(context.Background(), cfg, SocialProviderGoogle, signTestIDToken(t, key, cl), testSocialRawNonce)
	assertSocialInvalid(t, err)

	// Apple 的 token 不能冒充 Google（反之亦然）
	cl = googleTestClaims(cfg.IDs.GoogleWebClientID)
	cl["iss"] = "https://appleid.apple.com"
	_, err = verifySocialIDToken(context.Background(), cfg, SocialProviderGoogle, signTestIDToken(t, key, cl), testSocialRawNonce)
	assertSocialInvalid(t, err)

	cl = appleTestClaims(cfg.IDs.AppleBundleID)
	cl["iss"] = "https://accounts.google.com"
	_, err = verifySocialIDToken(context.Background(), cfg, SocialProviderApple, signTestIDToken(t, key, cl), testSocialRawNonce)
	assertSocialInvalid(t, err)

	cl = appleTestClaims(cfg.IDs.AppleBundleID)
	delete(cl, "iss")
	_, err = verifySocialIDToken(context.Background(), cfg, SocialProviderApple, signTestIDToken(t, key, cl), testSocialRawNonce)
	assertSocialInvalid(t, err)
}

func TestVerifySocialIDToken_Expired(t *testing.T) {
	key := newTestRSAKey(t)
	installTestSocialKeys(t, key)
	cfg := readySocialConfig(BrandKaitu)

	cl := googleTestClaims(cfg.IDs.GoogleWebClientID)
	cl["exp"] = time.Now().Add(-2 * time.Minute).Unix()
	_, err := verifySocialIDToken(context.Background(), cfg, SocialProviderGoogle, signTestIDToken(t, key, cl), testSocialRawNonce)
	assertSocialInvalid(t, err)

	// 60s 容差内仍接受
	cl = googleTestClaims(cfg.IDs.GoogleWebClientID)
	cl["exp"] = time.Now().Add(-30 * time.Second).Unix()
	_, err = verifySocialIDToken(context.Background(), cfg, SocialProviderGoogle, signTestIDToken(t, key, cl), testSocialRawNonce)
	require.NoError(t, err)

	// 缺 exp 拒绝
	cl = googleTestClaims(cfg.IDs.GoogleWebClientID)
	delete(cl, "exp")
	_, err = verifySocialIDToken(context.Background(), cfg, SocialProviderGoogle, signTestIDToken(t, key, cl), testSocialRawNonce)
	assertSocialInvalid(t, err)
}

func TestVerifySocialIDToken_NonceMismatch(t *testing.T) {
	key := newTestRSAKey(t)
	installTestSocialKeys(t, key)
	cfg := readySocialConfig(BrandKaitu)
	tok := signTestIDToken(t, key, appleTestClaims(cfg.IDs.AppleBundleID))

	_, err := verifySocialIDToken(context.Background(), cfg, SocialProviderApple, tok, "other-nonce")
	assertSocialInvalid(t, err)

	// 客户端把 hash 当 raw 交上来也不行
	_, err = verifySocialIDToken(context.Background(), cfg, SocialProviderApple, tok, socialNonceHash(testSocialRawNonce))
	assertSocialInvalid(t, err)

	// token 无 nonce、请求也无 nonce：拒绝（不能用 "" == "" 绕过）
	cl := appleTestClaims(cfg.IDs.AppleBundleID)
	delete(cl, "nonce")
	_, err = verifySocialIDToken(context.Background(), cfg, SocialProviderApple, signTestIDToken(t, key, cl), "")
	assertSocialInvalid(t, err)

	// token nonce 是原文（客户端没 hash）
	cl = appleTestClaims(cfg.IDs.AppleBundleID)
	cl["nonce"] = testSocialRawNonce
	_, err = verifySocialIDToken(context.Background(), cfg, SocialProviderApple, signTestIDToken(t, key, cl), testSocialRawNonce)
	assertSocialInvalid(t, err)
}

func TestVerifySocialIDToken_HS256Rejected(t *testing.T) {
	key := newTestRSAKey(t)
	cfg := readySocialConfig(BrandKaitu)
	secret := []byte("shared-secret")
	orig := socialKeyfunc
	// 即便 keyfunc 愿意给出 HMAC 密钥，算法白名单也必须拦住
	socialKeyfunc = map[string]jwt.Keyfunc{
		SocialProviderGoogle: func(*jwt.Token) (any, error) { return secret, nil },
		SocialProviderApple:  func(*jwt.Token) (any, error) { return &key.PublicKey, nil },
	}
	t.Cleanup(func() { socialKeyfunc = orig })

	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, googleTestClaims(cfg.IDs.GoogleWebClientID)).SignedString(secret)
	require.NoError(t, err)
	_, err = verifySocialIDToken(context.Background(), cfg, SocialProviderGoogle, tok, testSocialRawNonce)
	assertSocialInvalid(t, err)

	// alg=none
	none, err := jwt.NewWithClaims(jwt.SigningMethodNone, googleTestClaims(cfg.IDs.GoogleWebClientID)).SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)
	_, err = verifySocialIDToken(context.Background(), cfg, SocialProviderGoogle, none, testSocialRawNonce)
	assertSocialInvalid(t, err)
}

func TestVerifySocialIDToken_CrossBrandAud(t *testing.T) {
	key := newTestRSAKey(t)
	installTestSocialKeys(t, key)
	kaitu := readySocialConfig(BrandKaitu)
	overleap := readySocialConfig(BrandOverleap)

	tok := signTestIDToken(t, key, googleTestClaims(overleap.IDs.GoogleWebClientID))
	_, err := verifySocialIDToken(context.Background(), kaitu, SocialProviderGoogle, tok, testSocialRawNonce)
	assertSocialInvalid(t, err)

	tok = signTestIDToken(t, key, appleTestClaims(overleap.IDs.AppleBundleID))
	_, err = verifySocialIDToken(context.Background(), kaitu, SocialProviderApple, tok, testSocialRawNonce)
	assertSocialInvalid(t, err)

	// 同品牌但跨提供方的 client ID 也不行（Apple 的 aud 不能过 Google）
	tok = signTestIDToken(t, key, googleTestClaims(kaitu.IDs.AppleServicesID))
	_, err = verifySocialIDToken(context.Background(), kaitu, SocialProviderGoogle, tok, testSocialRawNonce)
	assertSocialInvalid(t, err)

	// 缺 aud
	cl := googleTestClaims("")
	delete(cl, "aud")
	_, err = verifySocialIDToken(context.Background(), kaitu, SocialProviderGoogle, signTestIDToken(t, key, cl), testSocialRawNonce)
	assertSocialInvalid(t, err)
}

func TestVerifySocialIDToken_EmptySub(t *testing.T) {
	key := newTestRSAKey(t)
	installTestSocialKeys(t, key)
	cfg := readySocialConfig(BrandKaitu)
	cl := googleTestClaims(cfg.IDs.GoogleWebClientID)
	cl["sub"] = "  "
	_, err := verifySocialIDToken(context.Background(), cfg, SocialProviderGoogle, signTestIDToken(t, key, cl), testSocialRawNonce)
	assertSocialInvalid(t, err)
}

func TestVerifySocialIDToken_ProviderDisabled(t *testing.T) {
	key := newTestRSAKey(t)
	installTestSocialKeys(t, key)

	cfg := readySocialConfig(BrandKaitu)
	cfg.GoogleEnabled = false
	_, err := verifySocialIDToken(context.Background(), cfg, SocialProviderGoogle, signTestIDToken(t, key, googleTestClaims(cfg.IDs.GoogleWebClientID)), testSocialRawNonce)
	require.Error(t, err)
	assert.True(t, errors.Is(err, errSocialProviderDisabled), "got %v", err)

	cfg = readySocialConfig(BrandKaitu)
	cfg.Apple.PrivateKeyPEM = ""
	_, err = verifySocialIDToken(context.Background(), cfg, SocialProviderApple, signTestIDToken(t, key, appleTestClaims(cfg.IDs.AppleBundleID)), testSocialRawNonce)
	assert.True(t, errors.Is(err, errSocialProviderDisabled), "got %v", err)

	_, err = verifySocialIDToken(context.Background(), readySocialConfig(BrandKaitu), "facebook", "x.y.z", testSocialRawNonce)
	assert.True(t, errors.Is(err, errSocialProviderDisabled), "got %v", err)
}

func TestVerifySocialIDToken_GoogleUnverifiedEmail(t *testing.T) {
	key := newTestRSAKey(t)
	installTestSocialKeys(t, key)
	cfg := readySocialConfig(BrandKaitu)

	for _, v := range []any{false, "false", nil} {
		cl := googleTestClaims(cfg.IDs.GoogleWebClientID)
		if v == nil {
			delete(cl, "email_verified")
		} else {
			cl["email_verified"] = v
		}
		c, err := verifySocialIDToken(context.Background(), cfg, SocialProviderGoogle, signTestIDToken(t, key, cl), testSocialRawNonce)
		require.NoError(t, err)
		assert.Equal(t, "alice@example.com", c.Email)
		assert.False(t, c.EmailTrusted, "email_verified=%v", v)
	}
}

func TestVerifySocialIDToken_AppleEmailVerifiedAsString(t *testing.T) {
	key := newTestRSAKey(t)
	installTestSocialKeys(t, key)
	cfg := readySocialConfig(BrandKaitu)

	for _, v := range []any{"true", "false", false} {
		cl := appleTestClaims(cfg.IDs.AppleBundleID)
		cl["email"] = " User@Example.com"
		cl["email_verified"] = v
		c, err := verifySocialIDToken(context.Background(), cfg, SocialProviderApple, signTestIDToken(t, key, cl), testSocialRawNonce)
		require.NoError(t, err)
		assert.Equal(t, "user@example.com", c.Email)
		assert.True(t, c.EmailTrusted, "email_verified=%v", v)
	}
}

// 懒加载的 JWKS keyfunc：初始化失败不缓存，下次调用重试。
func TestSocialJWKSKeyfunc_RetriesAfterInitFailure(t *testing.T) {
	key := newTestRSAKey(t)
	calls := 0
	orig := socialNewJWKS
	socialNewJWKS = func(ctx context.Context, url string) (jwt.Keyfunc, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("network down")
		}
		return func(*jwt.Token) (any, error) { return &key.PublicKey, nil }, nil
	}
	t.Cleanup(func() { socialNewJWKS = orig })

	lazy := newLazySocialJWKS("https://example.invalid/jwks")
	tok := &jwt.Token{Header: map[string]any{"kid": "k"}}
	_, err := lazy(tok)
	require.Error(t, err)
	got, err := lazy(tok)
	require.NoError(t, err)
	assert.Equal(t, &key.PublicKey, got)
	_, err = lazy(tok)
	require.NoError(t, err)
	assert.Equal(t, 2, calls, "成功后不再重复初始化")
}

func TestVerifySocialIDToken_Garbage(t *testing.T) {
	key := newTestRSAKey(t)
	installTestSocialKeys(t, key)
	cfg := readySocialConfig(BrandKaitu)
	for _, s := range []string{"", "abc", strings.Repeat("a.", 3)} {
		_, err := verifySocialIDToken(context.Background(), cfg, SocialProviderGoogle, s, testSocialRawNonce)
		assertSocialInvalid(t, err)
	}
}

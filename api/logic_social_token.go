package center

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
)

// 第三方 ID token（Google / Apple）校验。
//
// 校验项：RS256 签名（JWKS）、iss、aud ∈ 本品牌该提供方的 client ID、exp（60s 容差）、
// nonce == sha256hex(rawNonce)、sub 非空。任一不过 → wrap errSocialTokenInvalid；
// 提供方在本品牌未就绪 → errSocialProviderDisabled。

var (
	errSocialTokenInvalid     = errors.New("social token invalid")
	errSocialProviderDisabled = errors.New("social provider disabled")
)

const socialTokenLeeway = 60 * time.Second

const (
	socialGoogleJWKSURL = "https://www.googleapis.com/oauth2/v3/certs"
	socialAppleJWKSURL  = "https://appleid.apple.com/auth/keys"
)

var socialIssuers = map[string][]string{
	SocialProviderGoogle: {"https://accounts.google.com", "accounts.google.com"},
	SocialProviderApple:  {"https://appleid.apple.com"},
}

type socialClaims struct {
	Provider     string
	Sub          string
	Email        string // 小写去空白；无则 ""
	EmailTrusted bool   // google: email_verified==true；apple: email 非空即 true
	Audience     string // 实际命中的 aud（Apple 交换 code 时用作 client_id）
}

// socialKeyfunc provider → 验签密钥查找。包级可替换：测试注入本地 RSA 公钥。
// 生产值是懒加载的 JWKS（首次使用时才拉取，不在包初始化时打外网）。
var socialKeyfunc = map[string]jwt.Keyfunc{
	SocialProviderGoogle: newLazySocialJWKS(socialGoogleJWKSURL),
	SocialProviderApple:  newLazySocialJWKS(socialAppleJWKSURL),
}

// socialNewJWKS 构造远端 JWKS keyfunc（会起后台刷新 goroutine，故用进程级 ctx 而非请求 ctx）。
// 包级可替换，测试不打外网。
var socialNewJWKS = func(ctx context.Context, url string) (jwt.Keyfunc, error) {
	kf, err := keyfunc.NewDefaultCtx(ctx, []string{url})
	if err != nil {
		return nil, err
	}
	return kf.Keyfunc, nil
}

// newLazySocialJWKS 首次调用时初始化 JWKS；初始化失败不缓存，下次调用重试。
func newLazySocialJWKS(url string) jwt.Keyfunc {
	var (
		mu sync.Mutex
		kf jwt.Keyfunc
	)
	return func(token *jwt.Token) (any, error) {
		mu.Lock()
		if kf == nil {
			k, err := socialNewJWKS(context.Background(), url)
			if err != nil {
				mu.Unlock()
				return nil, fmt.Errorf("init JWKS %s: %w", url, err)
			}
			kf = k
		}
		f := kf
		mu.Unlock()
		return f(token)
	}
}

// socialNonceHash 交给提供方的 nonce = sha256(rawNonce) 小写 hex。
func socialNonceHash(rawNonce string) string {
	sum := sha256.Sum256([]byte(rawNonce))
	return hex.EncodeToString(sum[:])
}

type socialIDTokenClaims struct {
	jwt.RegisteredClaims
	Nonce         string `json:"nonce"`
	Email         string `json:"email"`
	EmailVerified any    `json:"email_verified"` // Google: bool；Apple: bool 或 "true"/"false"
}

func verifySocialIDToken(ctx context.Context, cfg socialAuthConfig, provider, idToken, rawNonce string) (*socialClaims, error) {
	if !cfg.Ready(provider) {
		return nil, fmt.Errorf("%w: %s (brand %s)", errSocialProviderDisabled, provider, cfg.Brand)
	}
	keyFn := socialKeyfunc[provider]
	issuers := socialIssuers[provider]
	if keyFn == nil || len(issuers) == 0 {
		return nil, fmt.Errorf("%w: %s has no verifier", errSocialProviderDisabled, provider)
	}

	var c socialIDTokenClaims
	_, err := jwt.ParseWithClaims(idToken, &c, keyFn,
		jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}),
		jwt.WithLeeway(socialTokenLeeway),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return nil, fmt.Errorf("%w: parse: %v", errSocialTokenInvalid, err)
	}

	if !slices.Contains(issuers, c.Issuer) {
		return nil, fmt.Errorf("%w: unexpected iss %q", errSocialTokenInvalid, c.Issuer)
	}

	allowed := cfg.IDs.Audiences(provider)
	matched := ""
	for _, a := range c.Audience {
		if slices.Contains(allowed, a) {
			matched = a
			break
		}
	}
	if matched == "" {
		return nil, fmt.Errorf("%w: aud %v not allowed for brand %s", errSocialTokenInvalid, []string(c.Audience), cfg.Brand)
	}

	// 比对的是 hash（永不为空），缺 nonce 的 token 不可能通过
	if c.Nonce != socialNonceHash(rawNonce) {
		return nil, fmt.Errorf("%w: nonce mismatch", errSocialTokenInvalid)
	}

	sub := strings.TrimSpace(c.Subject)
	if sub == "" || sub != c.Subject {
		return nil, fmt.Errorf("%w: empty sub", errSocialTokenInvalid)
	}

	email := strings.ToLower(strings.TrimSpace(c.Email))
	trusted := false
	switch provider {
	case SocialProviderGoogle:
		trusted = email != "" && socialClaimTrue(c.EmailVerified)
	case SocialProviderApple:
		// Apple 只下发经其验证的邮箱（含私密中继），email_verified 形态不稳定，不依赖它。
		trusted = email != ""
	}

	return &socialClaims{
		Provider:     provider,
		Sub:          sub,
		Email:        email,
		EmailTrusted: trusted,
		Audience:     matched,
	}, nil
}

func socialClaimTrue(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return strings.EqualFold(x, "true")
	}
	return false
}

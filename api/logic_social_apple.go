package center

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Apple Sign in with Apple 的服务端调用：授权码换 refresh token、撤销 refresh token。
// client secret 是用 .p8 私钥（ES256）现签的短期 JWT。

const (
	appleIDAudience       = "https://appleid.apple.com"
	appleTokenURL         = "https://appleid.apple.com/auth/token"
	appleRevokeURL        = "https://appleid.apple.com/auth/revoke"
	appleClientSecretTTL  = 5 * time.Minute
	appleHTTPTimeout      = 10 * time.Second
	appleErrorBodyMaxSize = 1024
)

var appleHTTPClient = &http.Client{Timeout: appleHTTPTimeout}

// appleClientSecret 生成 Apple client secret：iss=TeamID, sub=clientID, aud=https://appleid.apple.com,
// header kid=KeyID, iat=now, exp=now+5min。
func appleClientSecret(cfg socialAuthConfig, clientID string, now time.Time) (string, error) {
	key, err := jwt.ParseECPrivateKeyFromPEM([]byte(cfg.Apple.PrivateKeyPEM))
	if err != nil {
		return "", fmt.Errorf("parse apple private key: %w", err)
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.RegisteredClaims{
		Issuer:    cfg.Apple.TeamID,
		Subject:   clientID,
		Audience:  jwt.ClaimStrings{appleIDAudience},
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(appleClientSecretTTL)),
	})
	tok.Header["kid"] = cfg.Apple.KeyID
	return tok.SignedString(key)
}

// appleExchangeCode 用授权码换 refresh token。包级可替换，测试不打外网。
var appleExchangeCode = func(ctx context.Context, cfg socialAuthConfig, clientID, code string) (string, error) {
	secret, err := appleClientSecret(cfg, clientID, time.Now())
	if err != nil {
		return "", err
	}
	body, err := applePostForm(ctx, appleTokenURL, url.Values{
		"client_id":     {clientID},
		"client_secret": {secret},
		"code":          {code},
		"grant_type":    {"authorization_code"},
	})
	if err != nil {
		return "", err
	}
	var resp struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("decode apple token response: %w", err)
	}
	if resp.RefreshToken == "" {
		return "", fmt.Errorf("apple token response has no refresh_token")
	}
	return resp.RefreshToken, nil
}

// appleRevokeToken 撤销 refresh token（注销账号 / 解绑时）。包级可替换，测试不打外网。
var appleRevokeToken = func(ctx context.Context, cfg socialAuthConfig, clientID, refreshToken string) error {
	secret, err := appleClientSecret(cfg, clientID, time.Now())
	if err != nil {
		return err
	}
	_, err = applePostForm(ctx, appleRevokeURL, url.Values{
		"client_id":       {clientID},
		"client_secret":   {secret},
		"token":           {refreshToken},
		"token_type_hint": {"refresh_token"},
	})
	return err
}

// applePostForm 发表单请求，非 2xx 视为失败（错误里只带响应体前缀，不含请求内容）。
func applePostForm(ctx context.Context, endpoint string, form url.Values) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := appleHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("apple %s: %w", endpoint, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read apple %s response: %w", endpoint, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet := body
		if len(snippet) > appleErrorBodyMaxSize {
			snippet = snippet[:appleErrorBodyMaxSize]
		}
		return nil, fmt.Errorf("apple %s: status %d: %s", endpoint, resp.StatusCode, snippet)
	}
	return body, nil
}

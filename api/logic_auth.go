package center

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	mathrand "math/rand"
	"strings"
	"time"

	db "github.com/wordgate/qtoolkit/db"

	"github.com/golang-jwt/jwt/v5"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/redis"
	"github.com/wordgate/qtoolkit/util"
)

const (

	// Token 类型常量
	TokenTypeAccess  = "access"
	TokenTypeRefresh = "refresh"

	// 验证码相关常量
	VerificationCodeLength     = 6                  // 验证码长度
	VerificationCodeExpiry     = 300                // 验证码有效期（秒）
	VerificationCodePrefix     = "auth:code:email:" // 验证码缓存前缀
	VerificationCodeLockPrefix = "auth:lock:email:" // 验证码发送锁前缀
	VerificationCodeLockExpiry = 60                 // 验证码发送锁有效期（秒）
)

var (
	// 错误定义
	ErrInvalidCredentials = e(ErrorNotLogin, "invalid credentials")
	ErrInvalidToken       = e(ErrorNotLogin, "invalid token")             // 401: token 无效或过期
	ErrMembershipExpired  = e(ErrorPaymentRequired, "membership expired") // 402: 会员已过期
	ErrDeviceNotFound     = e(ErrorNotFound, "device not found")

	// mock验证码开关
	EnableMockVerificationCode = false
	MockVerificationCode       = "123456"
)

// TokenClaims JWT 声明结构
// 注意：字段名必须保持向后兼容，不能修改 json tag
type TokenClaims struct {
	UserID       uint64 `json:"user_id"`
	DeviceID     string `json:"device_id"`
	Exp          int64  `json:"exp"`
	Type         string `json:"type"` // access/refresh
	TokenIssueAt int64  `json:"token_issue_at"`
	Roles        uint64 `json:"roles"` // 角色位掩码（新增，旧 token 解析为 0）
}

// 实现 jwt.Claims 接口
func (c TokenClaims) GetExpirationTime() (*jwt.NumericDate, error) {
	return jwt.NewNumericDate(time.Unix(c.Exp, 0)), nil
}

func (c TokenClaims) GetNotBefore() (*jwt.NumericDate, error) {
	return nil, nil
}

func (c TokenClaims) GetIssuedAt() (*jwt.NumericDate, error) {
	return nil, nil
}

func (c TokenClaims) GetIssuer() (string, error) {
	return "", nil
}

func (c TokenClaims) GetSubject() (string, error) {
	return "", nil
}

func (c TokenClaims) GetAudience() (jwt.ClaimStrings, error) {
	return nil, nil
}

// generateTokens 生成访问令牌和刷新令牌
func generateTokens(ctx context.Context, userID uint64, deviceID string, roles uint64) (*DataAuthResult, time.Time, error) {

	log.Debugf(ctx, "generating tokens for user %d, device %s, roles %d", userID, deviceID, roles)
	jwtConfig := configJwt(ctx)
	jwtSecret := []byte(jwtConfig.Secret)
	log.Debugf(ctx, "jwt expiry: access=%ds, refresh=%ds", jwtConfig.AccessTokenExpiry, jwtConfig.RefreshTokenExpiry)
	accessTokenExpiry := time.Duration(jwtConfig.AccessTokenExpiry) * time.Second
	refreshTokenExpiry := time.Duration(jwtConfig.RefreshTokenExpiry) * time.Second

	now := time.Now()
	var issue = func(tokenType string, expiry time.Duration) (string, error) {
		claims := TokenClaims{
			UserID:       userID,
			DeviceID:     deviceID,
			Exp:          now.Add(expiry).Unix(),
			Type:         tokenType,
			TokenIssueAt: now.Unix(),
			Roles:        roles,
		}
		return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(jwtSecret)
	}
	accessToken, err1 := issue(TokenTypeAccess, accessTokenExpiry)
	refreshToken, err2 := issue(TokenTypeRefresh, refreshTokenExpiry)
	if err1 != nil {
		return nil, now, err1
	}
	if err2 != nil {
		return nil, now, err2
	}
	r := DataAuthResult{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	}

	return &r, now, nil
}

// generateWebTokens 生成Web认证令牌（无设备绑定）
// 用于 App 端 Bearer Token 认证（保留 refresh token）
func generateWebTokens(ctx context.Context, userID uint64, roles uint64) (*DataAuthResult, time.Time, error) {
	log.Debugf(ctx, "generating web tokens for user %d (no device), roles %d", userID, roles)
	// 使用空字符串作为 deviceID 表示Web认证
	return generateTokens(ctx, userID, "", roles)
}

// WebCookieTokenExpiry Web Cookie 认证的 access_token 有效期（2个月）
const WebCookieTokenExpiry = 60 * 24 * time.Hour // 60 days

// WebCookieRenewalThreshold Web Cookie 自动续期阈值（剩余不足7天时自动续期）
const WebCookieRenewalThreshold = 7 * 24 * time.Hour // 7 days

// generateWebCookieToken 生成 Web Cookie 专用的 access token
// 有效期为 2 个月，不需要 refresh token（通过 sliding expiration 自动续期）
func generateWebCookieToken(ctx context.Context, userID uint64, roles uint64) (*DataAuthResult, time.Time, error) {
	log.Debugf(ctx, "generating web cookie token for user %d (2-month expiry), roles %d", userID, roles)
	jwtConfig := configJwt(ctx)
	jwtSecret := []byte(jwtConfig.Secret)

	now := time.Now()
	claims := TokenClaims{
		UserID:       userID,
		DeviceID:     "", // Web 认证无设备
		Exp:          now.Add(WebCookieTokenExpiry).Unix(),
		Type:         TokenTypeAccess,
		TokenIssueAt: now.Unix(),
		Roles:        roles,
	}

	accessToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(jwtSecret)
	if err != nil {
		return nil, now, err
	}

	return &DataAuthResult{
		AccessToken:  accessToken,
		RefreshToken: "", // Web Cookie 模式不需要 refresh token
	}, now, nil
}

// validateToken 验证令牌
func validateToken(ctx context.Context, tokenString string, tokenType string) (*TokenClaims, *Device, error) {
	log.Debugf(ctx, "validating token")
	jwtConfig := configJwt(ctx)
	jwtSecret := []byte(jwtConfig.Secret)
	token, err := jwt.ParseWithClaims(tokenString, &TokenClaims{}, func(token *jwt.Token) (interface{}, error) {
		return jwtSecret, nil
	})
	if err != nil {
		log.Warnf(ctx, "failed to parse token: %v", err)
		return nil, nil, ErrInvalidToken
	}

	claims, ok := token.Claims.(*TokenClaims)

	if !ok || !token.Valid {
		log.Debugf(ctx, "token is valid for user %d, device %s", claims.UserID, claims.DeviceID)
		log.Warnf(ctx, "invalid token received")
		return nil, nil, ErrInvalidToken
	}
	if claims.Type != tokenType {
		log.Warnf(ctx, "invalid token type: %s,user %d, udid %s", tokenType, claims.UserID, claims.DeviceID)
		return nil, nil, ErrInvalidToken
	}
	log.Debugf(ctx, "refresh token claims parsed for user %d, udid %s", claims.UserID, claims.DeviceID)

	var device Device
	if err := db.Get().Where("user_id = ? AND udid = ?", claims.UserID, claims.DeviceID).First(&device).Error; err != nil {
		if util.DbIsNotFoundErr(err) {
			log.Warnf(ctx, "device not found for user %d, udid %s", claims.UserID, claims.DeviceID)
			return nil, nil, ErrInvalidToken
		}
		return nil, nil, err
	}
	if device.TokenIssueAt != claims.TokenIssueAt {
		log.Warnf(ctx, "token issue at mismatch for user %d, udid %s", claims.UserID, claims.DeviceID)
		return nil, nil, ErrInvalidToken
	}
	return claims, &device, nil

}

// saveEmailVerificationCode 保存验证码
func saveEmailVerificationCode(ctx context.Context, emailId string, code string, expireMinutes int) error {
	if EnableMockVerificationCode {
		return redis.CacheSet(VerificationCodePrefix+emailId, MockVerificationCode, expireMinutes*60)
	}
	// 检查是否在冷却时间内
	lockKey := VerificationCodeLockPrefix + emailId
	log.Debugf(ctx, "attempting to lock for sending verification code to %s", emailId)
	locked, err := redis.TryLock(lockKey, VerificationCodeLockExpiry)
	if err != nil {
		log.Errorf(ctx, "failed to check cooldown for %s: %v", emailId, err)
		return fmt.Errorf("failed to check cooldown: %w", err)
	}
	if !locked {
		log.Warnf(ctx, "sending verification code to %s is locked (too many requests)", emailId)
		return fmt.Errorf("too many requests")
	}

	// 存储验证码到缓存
	cacheKey := VerificationCodePrefix + emailId
	log.Debugf(ctx, "saving verification code for %s", emailId)
	if err := redis.CacheSet(cacheKey, code, expireMinutes*60); err != nil {
		log.Errorf(ctx, "failed to save verification code for %s: %v", emailId, err)
		return fmt.Errorf("failed to save verification code: %w", err)
	}

	return nil
}

// generateVerificationCode 生成验证码，支持稳定因子（stableKey）
func generateVerificationCode(ctx context.Context, stableKey string) string {
	if EnableMockVerificationCode {
		return MockVerificationCode
	}
	cacheKey := ""
	if stableKey != "" {
		cacheKey = VerificationCodePrefix + "stable:" + stableKey
		var cachedCode string
		exist, err := redis.CacheGet(cacheKey, &cachedCode)
		if err == nil && exist && cachedCode != "" {
			log.Debugf(ctx, "returning stable verification code for key %s", stableKey)
			return cachedCode
		}
	}
	// 生成6位数字验证码
	code := fmt.Sprintf("%06d", mathrand.Intn(1000000))
	log.Debugf(ctx, "generated new verification code %s for key %s", code, stableKey)
	if cacheKey != "" {
		_ = redis.CacheSet(cacheKey, code, VerificationCodeExpiry) // 5分钟
	}
	return code
}

// verifyEmailCode 验证验证码
func verifyEmailCode(ctx context.Context, emailId, code string) bool {
	if EnableMockVerificationCode {
		return code == MockVerificationCode
	}
	cacheKey := VerificationCodePrefix + emailId
	var savedCode string
	log.Debugf(ctx, "verifying email code for %s", emailId)
	exist, err := redis.CacheGet(cacheKey, &savedCode)
	if err != nil || !exist {
		if err != nil {
			log.Errorf(ctx, "failed to get verification code from cache for %s: %v", emailId, err)
		} else {
			log.Warnf(ctx, "verification code not found in cache for %s", emailId)
		}
		return false
	}
	isValid := savedCode == code
	if !isValid {
		log.Warnf(ctx, "invalid verification code for %s. expected: %s, got: %s", emailId, savedCode, code)
	}

	return isValid
}

// deleteVerificationCode 删除验证码
func deleteVerificationCode(ctx context.Context, email string) error {
	cacheKey := VerificationCodePrefix + email
	log.Infof(ctx, "deleting verification code for %s", email)
	return redis.CacheDel(cacheKey)
}

// ===================== 智能语言检测系统 =====================

// detectUserLanguage 智能检测用户语言偏好
// 优先级：显式传入参数 > Accept-Language Header > 邮箱域名推测 > 默认值
func detectUserLanguage(c context.Context, requestLang, email, acceptLanguageHeader string) string {
	// 1. 优先级最高：显式传入的语言参数
	if requestLang != "" {
		// 标准化并验证BCP 47语言标签
		normalizedLang := NormalizeBCP47Language(requestLang)
		if IsValidBCP47Language(normalizedLang) {
			log.Infof(c, "using explicit language parameter: %s (normalized: %s)", requestLang, normalizedLang)
			return normalizedLang
		}
		log.Warnf(c, "invalid BCP 47 language parameter: %s, fallback to detection", requestLang)
	}

	// 2. 次优先级：解析Accept-Language Header
	if acceptLanguageHeader != "" {
		detectedLang := parseAcceptLanguageHeader(acceptLanguageHeader)
		if detectedLang != "" {
			log.Infof(c, "detected language from Accept-Language header: %s", detectedLang)
			return detectedLang
		}
	}

	return "zh-CN"
}

// parseAcceptLanguageHeader 解析Accept-Language头部，返回BCP 47标准的语言标签
func parseAcceptLanguageHeader(acceptLang string) string {
	// Accept-Language格式: zh-CN,zh;q=0.9,en;q=0.8,en-GB;q=0.7,en-US;q=0.6
	// 按照质量值排序，返回第一个有效的BCP 47语言标签

	if acceptLang == "" {
		return ""
	}

	// 分割并处理每个语言项
	langItems := strings.Split(acceptLang, ",")

	for _, item := range langItems {
		// 移除质量值部分（;q=0.9）
		item = strings.TrimSpace(item)
		langCode := strings.Split(item, ";")[0]
		langCode = strings.TrimSpace(langCode)

		// 标准化并验证BCP 47格式
		normalized := NormalizeBCP47Language(langCode)
		if IsValidBCP47Language(normalized) {
			return normalized
		}

		// 如果不是完整的BCP 47标签，尝试基础语言匹配
		baseLang := strings.ToLower(langCode)
		switch baseLang {
		case "zh", "zh-cn", "chinese":
			return "zh-CN"
		case "zh-tw", "zh-hant":
			return "zh-TW"
		case "zh-hk":
			return "zh-HK"
		case "en", "english":
			return "en-US"
		case "en-gb", "en-uk":
			return "en-GB"
		case "en-au":
			return "en-AU"
		case "ja", "japanese":
			return "ja"
		case "ko", "korean":
			return "ko"
		case "fr", "french":
			return "fr"
		case "de", "german":
			return "de"
		case "es", "spanish":
			return "es"
		}
	}

	return ""
}

// ===================== CSRF Token 和 Cookie 认证 =====================

const (
	// Cookie 名称常量
	CookieAccessToken  = "access_token"
	CookieRefreshToken = "refresh_token"
	CookieCSRFToken    = "csrf_token"

	// CSRF Token 长度（字节）
	CSRFTokenLength = 32
)

// GenerateCSRFToken 生成安全的 CSRF Token
func GenerateCSRFToken() string {
	bytes := make([]byte, CSRFTokenLength)
	if _, err := rand.Read(bytes); err != nil {
		// fallback to less secure method if crypto/rand fails
		for i := range bytes {
			bytes[i] = byte(mathrand.Intn(256))
		}
	}
	return hex.EncodeToString(bytes)
}

// isSecureRequest 判断是否为 HTTPS 请求
func isSecureRequest(proto, host string) bool {
	return proto == "https" || strings.HasPrefix(host, "https")
}

package center

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	db "github.com/wordgate/qtoolkit/db"
	"gorm.io/gorm"

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
	VerificationCodeLength = 6 // 验证码长度
	// VerificationCodeExpiry 是验证码的初始/刷新有效期（秒）。
	// 同一邮箱在窗口内复用同一验证码，每次重发都把 TTL 重置到 30 分钟。
	VerificationCodeExpiry        = 1800
	VerificationCodeExpiryMinutes = VerificationCodeExpiry / 60 // 暴露给邮件模板
	// VerificationCodeUsedGracePeriod 是 verify 成功后保留验证码的宽限期（秒）。
	// 用途：客户端双击 / 网络抖动重试时，second verify 仍能命中并幂等成功。
	// 上限严格：宽限期内任何一次 /api/auth/code 重发都会作废老码生成新码，
	// 即"60s 上限"由代码强制 —— 而不是"60s 或者直到下一次重发"。
	VerificationCodeUsedGracePeriod = 60
	VerificationCodePrefix          = "auth:code:email:" // 验证码缓存前缀
	// verificationCodeConsumedPrefix 标记一份码已经通过 verify、正处于宽限期。
	// 用 value-prefix（而不是另一个 key）来表达"已消费"语义，避免双 key
	// 同步漂移；issueOrRefresh 看到这个前缀必须重新生成而不是复用。
	verificationCodeConsumedPrefix = "used:"
)

// VerifyCodeResult 区分 verify 失败的子原因，让前端能给出更精准的提示。
type VerifyCodeResult int

const (
	VerifyCodeOK        VerifyCodeResult = iota // 验证通过
	VerifyCodeWrong                             // 缓存里有码但值不匹配（用户输错）
	VerifyCodeNotIssued                         // 缓存里没有码（已过期 / 从未发送 / 已超出宽限期）
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

// init 启动期 self-test：验证 crypto/rand 工作正常。
// Go 1.24+ 起 crypto/rand 已是 infallible —— 系统 entropy 不可用时 crypto/rand
// 内部直接 panic，不再向调用方返回 error。本 self-test 是 belt-and-suspenders：
// 任何 crypto/rand 故障会在进程启动期暴露（包括 unit test、CI、deploy），
// 而不是运行时 OTP / CSRF 路径偶发。失败 → process 退出 → systemd 标记失败 →
// 部署中断 → 0 流量受影响。
func init() {
	if _, err := randomSixDigitCode(); err != nil {
		panic(fmt.Sprintf("crypto/rand self-test failed at startup (randomSixDigitCode): %v", err))
	}
	if got := GenerateCSRFToken(); len(got) != CSRFTokenLength*2 {
		panic(fmt.Sprintf("CSRF token self-test failed: expected %d hex chars, got %d", CSRFTokenLength*2, len(got)))
	}
}

// randomSixDigitCode 用 crypto/rand 生成 6 位数字验证码（左零填充）。
// 用 crypto/rand 而不是 math/rand：OTP 是认证凭据，可猜测性即可登录。
// 在 Go 1.24+ 上 crypto/rand.Read 是 infallible（失败会 panic in crypto/rand），
// 返回的 err 实际是 dead branch；保留 error 返回值是为了：
//  1) 应对未来 Go 版本可能再改契约
//  2) 让 init() self-test 可以走 error 通道而不是依赖 panic 链
//
// 用 rejection sampling 而不是 `% 1000000`：彻底消除 modulo bias，让所有
// 10^6 个码等概率出现。
func randomSixDigitCode() (string, error) {
	const codeSpace = 1000000
	// 2^32 = 4294967296; 4294 * 10^6 = 4294000000 是最大无偏上界
	const limit = 4294000000
	var b [4]byte
	for {
		if _, err := rand.Read(b[:]); err != nil {
			return "", err
		}
		n := binary.BigEndian.Uint32(b[:])
		if n < limit {
			return fmt.Sprintf("%06d", n%codeSpace), nil
		}
		// 落入偏置区——重新抽样（每次拒绝概率 ~0.23%）
	}
}

// verificationCodeKey 返回验证码在 Redis 中的统一存储 key。
// 单一 key 同时承担"复用同码"和"verify 校验"两个角色——历史上分成两个 key
// (`auth:code:email:stable:<hash>` 复用 + `auth:code:email:<hash>` 校验)
// 会出现 stable 命中但 storage 没命中的 case，合并后不会再有这种漂移。
func verificationCodeKey(emailHash string) string {
	return VerificationCodePrefix + emailHash
}

// issueOrRefreshVerificationCode 拿到给定邮箱当前有效的验证码：
// - 缓存里已有"活跃"码 → 复用，并把 TTL 重置到 VerificationCodeExpiry。
// - 缓存里有"已消费"标记（used: 前缀，处于宽限期）→ 不复用，生成新码覆盖，
//   把宽限期内的老码即时作废。这保证了"60s 上限"语义。
// - 缓存里没有 → 新生成 6 位数字码，写入并返回。
// 调用方负责把返回的 code 发出去（邮件 / Slack）。
func issueOrRefreshVerificationCode(ctx context.Context, emailHash string) (string, error) {
	if EnableMockVerificationCode {
		return MockVerificationCode, nil
	}
	cacheKey := verificationCodeKey(emailHash)
	var existing string
	if exist, err := redis.CacheGet(cacheKey, &existing); err == nil && exist && existing != "" {
		if !strings.HasPrefix(existing, verificationCodeConsumedPrefix) {
			// 活跃码 — 复用并刷 TTL
			if err := redis.CacheSet(cacheKey, existing, VerificationCodeExpiry); err != nil {
				log.Errorf(ctx, "failed to refresh verification code TTL for %s: %v", emailHash, err)
				return "", fmt.Errorf("failed to refresh verification code: %w", err)
			}
			log.Debugf(ctx, "reusing verification code for %s, TTL refreshed", emailHash)
			return existing, nil
		}
		// 已消费 — 落入下面的"生成新码"分支，老码在新码 SET 后立即失效
		log.Debugf(ctx, "verification code for %s is in grace period, issuing fresh code", emailHash)
	} else if err != nil {
		log.Errorf(ctx, "failed to read verification code cache for %s: %v", emailHash, err)
		// 不致命——继续生成新码覆盖写入
	}
	code, err := randomSixDigitCode()
	if err != nil {
		log.Errorf(ctx, "failed to generate verification code for %s: %v", emailHash, err)
		return "", fmt.Errorf("failed to generate verification code: %w", err)
	}
	if err := redis.CacheSet(cacheKey, code, VerificationCodeExpiry); err != nil {
		log.Errorf(ctx, "failed to save verification code for %s: %v", emailHash, err)
		return "", fmt.Errorf("failed to save verification code: %w", err)
	}
	log.Debugf(ctx, "generated new verification code for %s", emailHash)
	return code, nil
}

// verifyEmailCode 校验验证码。返回值区分"输错"和"未发/已过期"，
// 让前端能给出精准提示而不是笼统的"验证码错误"。
// 已消费的码（used: 前缀）在宽限期内仍接受，实现双击/重试幂等。
func verifyEmailCode(ctx context.Context, emailHash, code string) VerifyCodeResult {
	if EnableMockVerificationCode {
		if code == MockVerificationCode {
			return VerifyCodeOK
		}
		return VerifyCodeWrong
	}
	cacheKey := verificationCodeKey(emailHash)
	var savedCode string
	log.Debugf(ctx, "verifying email code for %s", emailHash)
	exist, err := redis.CacheGet(cacheKey, &savedCode)
	if err != nil {
		log.Errorf(ctx, "failed to get verification code from cache for %s: %v", emailHash, err)
		// Redis 故障当作"未发/已过期"处理——比误判为"输错"更安全：
		// 用户会被提示重新发送，而不是反复尝试同一个码。
		return VerifyCodeNotIssued
	}
	if !exist {
		log.Warnf(ctx, "verification code not found in cache for %s", emailHash)
		return VerifyCodeNotIssued
	}
	if strings.HasPrefix(savedCode, verificationCodeConsumedPrefix) {
		// 宽限期：剥前缀比对原始码，匹配则幂等通过
		original := strings.TrimPrefix(savedCode, verificationCodeConsumedPrefix)
		if original == code {
			return VerifyCodeOK
		}
		log.Warnf(ctx, "invalid verification code (during grace) for %s", emailHash)
		return VerifyCodeWrong
	}
	if savedCode != code {
		log.Warnf(ctx, "invalid verification code for %s", emailHash)
		return VerifyCodeWrong
	}
	return VerifyCodeOK
}

// markVerificationCodeUsed 把已通过验证的码改写为 "used:<原码>" 并把 TTL
// 缩短到 VerificationCodeUsedGracePeriod。
// - 宽限期内 verify 仍能识别（剥前缀比对）→ 双击 / 网络抖动重试幂等
// - 宽限期内 issueOrRefresh 看到 used: 前缀 → 强制生成新码、覆盖老码
// 历史上这里是 deleteVerificationCode(rawEmail) — 但 save 写的是 indexID，
// raw email 永远不命中 → 实际"删不掉"。本函数统一收到 emailHash (indexID)。
func markVerificationCodeUsed(ctx context.Context, emailHash string) error {
	cacheKey := verificationCodeKey(emailHash)
	var savedCode string
	exist, err := redis.CacheGet(cacheKey, &savedCode)
	if err != nil {
		log.Errorf(ctx, "failed to read verification code while marking used for %s: %v", emailHash, err)
		return fmt.Errorf("failed to read verification code: %w", err)
	}
	if !exist {
		// 已过期 / 并发被另一次请求消耗 — 都不算错误
		log.Debugf(ctx, "verification code already gone for %s, nothing to mark", emailHash)
		return nil
	}
	if strings.HasPrefix(savedCode, verificationCodeConsumedPrefix) {
		// 已是 used: 状态（并发的 second verify 已经标记过）— 不重复写
		log.Debugf(ctx, "verification code for %s already marked used", emailHash)
		return nil
	}
	consumed := verificationCodeConsumedPrefix + savedCode
	if err := redis.CacheSet(cacheKey, consumed, VerificationCodeUsedGracePeriod); err != nil {
		log.Errorf(ctx, "failed to mark verification code used for %s: %v", emailHash, err)
		return fmt.Errorf("failed to mark verification code used: %w", err)
	}
	log.Infof(ctx, "verification code for %s marked used (grace=%ds)", emailHash, VerificationCodeUsedGracePeriod)
	return nil
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

// GenerateCSRFToken 生成安全的 CSRF Token。
// crypto/rand 失败是系统级故障——绝不降级到 math/rand，否则会签发可猜测的
// CSRF token，破坏 Cookie auth 的 CSRF 防御。让 panic 升到 recovery middleware
// 比静默签发弱 token 安全。
func GenerateCSRFToken() string {
	bytes := make([]byte, CSRFTokenLength)
	if _, err := rand.Read(bytes); err != nil {
		panic(fmt.Sprintf("crypto/rand unavailable, refusing to issue weak CSRF token: %v", err))
	}
	return hex.EncodeToString(bytes)
}

// isSecureRequest 判断是否为 HTTPS 请求
func isSecureRequest(proto, host string) bool {
	return proto == "https" || strings.HasPrefix(host, "https")
}

// checkDeviceLimitOrKick enforces per-type device limits within a login transaction.
// For gateway (router) devices: rejects with 402/403 if no router permission or limit reached.
// For app devices: kicks (deletes) the oldest device if limit reached.
func checkDeviceLimitOrKick(c context.Context, tx *gorm.DB, user *User, isGateway bool) error {
	quota := user.Quota()
	if isGateway {
		if quota.MaxRouterDevice == 0 {
			log.Warnf(c, "user %d plan does not support router, rejecting gateway login", user.ID)
			return e(ErrorPaymentRequired, "plan does not support router")
		}
		var routerCount int64
		if err := tx.Model(&Device{}).Where("user_id = ? AND is_gateway = true", user.ID).Count(&routerCount).Error; err != nil {
			log.Errorf(c, "failed to count router devices for user %d: %v", user.ID, err)
			return err
		}
		if quota.MaxRouterDevice > 0 && routerCount >= int64(quota.MaxRouterDevice) {
			log.Warnf(c, "router device limit reached for user %d (%d/%d)", user.ID, routerCount, quota.MaxRouterDevice)
			return e(ErrorForbidden, "router device limit reached")
		}
		return nil
	}

	// App device limit: count only app devices, kick oldest on limit
	var appDeviceCount int64
	if err := tx.Model(&Device{}).Where("user_id = ? AND is_gateway = false", user.ID).Count(&appDeviceCount).Error; err != nil {
		log.Errorf(c, "failed to count app devices for user %d: %v", user.ID, err)
		return err
	}
	if appDeviceCount >= int64(quota.MaxDevice) {
		log.Warnf(c, "app device limit reached for user %d, will remove oldest app device", user.ID)
		var oldestDevice Device
		if err := tx.Where("user_id = ? AND is_gateway = false", user.ID).Order("token_last_used_at ASC").First(&oldestDevice).Error; err != nil {
			log.Errorf(c, "failed to find oldest app device for user %d: %v", user.ID, err)
			return err
		}
		if err := tx.Delete(&oldestDevice).Error; err != nil {
			log.Errorf(c, "failed to delete oldest device %s for user %d: %v", oldestDevice.UDID, user.ID, err)
			return err
		}
		log.Infof(c, "deleted oldest app device %s for user %d", oldestDevice.UDID, user.ID)

		meta := DeviceKickMeta{
			KickTime: time.Now().Format("2006-01-02 15:04:05"),
			Remark:   oldestDevice.Remark,
		}
		if err := emailToUser(c, int64(user.ID), deviceKickTemplate, meta); err != nil {
			log.Errorf(c, "failed to send device kick email to user %d: %v", user.ID, err)
		}
	}
	return nil
}

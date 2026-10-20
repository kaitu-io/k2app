// Package center 提供 Kaitu 中心服务的 API 接口
package center

import (
	"fmt"
	"net/http"
	"net/mail"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/slack"
	"github.com/wordgate/qtoolkit/util"
	"gorm.io/gorm"
)

// SendAuthCodeRequest 获取验证码请求数据结构
//
type SendAuthCodeRequest struct {
	Email    string `json:"email" binding:"required,email" example:"user@example.com"` // 邮箱地址
	Language string `json:"language" example:"en-US"`                                  // 用户语言偏好（注册时使用）
}

// api_send_auth_code 发送验证码（统一处理登录/注册）
//
func api_send_auth_code(c *gin.Context) {
	sendCodeWithMode(c, false) // 统一不要求用户存在，自动创建
}

// sanitizeEmail strips all whitespace (including non-ASCII like U+2006) and
// re-validates with net/mail. validator.v10's `email` tag uses an ASCII-only
// regex and lets non-ASCII whitespace through into the SMTP path; this helper
// is the second line of defense.
func sanitizeEmail(raw string) (string, error) {
	cleaned := strings.ToLower(strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, raw))
	if _, err := mail.ParseAddress(cleaned); err != nil {
		return "", err
	}
	return cleaned, nil
}

// cleanEmailField sanitizes an email pointer in place and emits an audit log
// when sanitization actually changed the value. Used by all auth handlers that
// take a user-supplied email so that fixes like "strip U+2006" stay consistent
// across send-code, login, web-login, and password-login.
func cleanEmailField(c *gin.Context, email *string) error {
	cleaned, err := sanitizeEmail(*email)
	if err != nil {
		return err
	}
	if cleaned != *email {
		log.Infof(c, "sanitized email input: %q -> %q", *email, cleaned)
	}
	*email = cleaned
	return nil
}

// sendCodeWithMode 发送验证码的内部实现
func sendCodeWithMode(c *gin.Context, userExistRequired bool) {
	var req SendAuthCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnf(c, "invalid send code request: %v", err)
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}
	if err := cleanEmailField(c, &req.Email); err != nil {
		log.Warnf(c, "invalid email format after sanitization: %v", err)
		Error(c, ErrorInvalidArgument, "invalid email format")
		return
	}

	log.Infof(c, "request to send auth code to email: %s, userExistRequired: %v", req.Email, userExistRequired)
	indexID := secretHashIt(c, []byte(req.Email))

	// 查找现有用户
	var identify LoginIdentify
	var user *User
	userExists := false

	if err := db.Get().Preload("User").Where("type = ? AND index_id = ? AND brand = ?", "email", indexID, string(ReqBrand(c))).First(&identify).Error; err != nil {
		if !util.DbIsNotFoundErr(err) {
			log.Errorf(c, "failed to check user: %v", err)
			Error(c, ErrorSystemError, "failed to check user")
			return
		}
		// 用户不存在
		userExists = false
	} else {
		userExists = true
		user = identify.User
	}

	if userExists && isUserBlocked(user) {
		log.Warnf(c, "send-code rejected: user %d is blocked", user.ID)
		Error(c, ErrorForbidden, "account blocked")
		return
	}

	// 根据 userExistRequired 参数处理
	if userExistRequired {
		// 要求用户必须存在（登录模式）
		if !userExists {
			log.Warnf(c, "user not found, but userExistRequired=true, email (hashed): %s", indexID)
			Error(c, ErrorNotFound, "user not found")
			return
		}
	} else {
		// 不要求用户存在（注册模式）：如果用户不存在则创建
		if !userExists {
			acceptLanguage := c.GetHeader("Accept-Language")
			newUser, err := FindOrCreateUserByEmail(c, ReqBrand(c), req.Email, req.Language, acceptLanguage)
			if err != nil {
				log.Errorf(c, "failed to create user for registration: %v", err)
				Error(c, ErrorSystemError, "failed to create user")
				return
			}
			user = newUser
			userExists = true
			log.Infof(c, "created new user %d for registration with language: %s", user.ID, user.Language)
		} else {
			log.Infof(c, "user already exists for registration mode, proceeding with login flow")
		}
	}

	// 生成（或复用）验证码 — 同邮箱在 TTL 窗口内复用同一个码，每次重发刷 TTL
	code, err := issueOrRefreshVerificationCode(c, indexID)
	if err != nil {
		log.Errorf(c, "failed to issue verification code for email (hashed) %s: %v", indexID, err)
		Error(c, ErrorSystemError, "failed to issue verification code")
		return
	}
	expireMinutes := VerificationCodeExpiryMinutes

	// 发送验证码邮件
	meta := VerificationCodeMeta{
		UserEmail:     req.Email,
		Code:          code,
		ExpireMinutes: expireMinutes,
	}
	// user 此时理论上恒非 nil（userExistRequired=false 分支已 FindOrCreateUserByEmail），
	// 但仍以 ReqBrand(c) 兜底：万一未来 userExistRequired=true 的调用点在此之前返回，
	// 品牌选择也不应该 panic 或误判。
	codeBrand := ReqBrand(c)
	if user != nil {
		codeBrand = Brand(user.Brand)
	}
	if err := emailTo(c, codeBrand, req.Email, brandedVerificationCodeTemplate.For(codeBrand), meta); err != nil {
		log.Errorf(c, "failed to send verification code email to %s: %v", req.Email, err)
		Error(c, ErrorSystemError, err.Error())
		return
	}

	// 发送管理员 Slack 通知。
	// Slack 消息本身就是 admin 2FA 的递送通道，必须带明文 code；
	// 但 Errorf/Debugf 日志会落到 S3，绝不能把 code 写进日志。
	if user != nil && user.IsAdmin != nil && *user.IsAdmin {
		if err := slack.Send("verify", fmt.Sprintf("管理员 %s 登录验证码: %s", req.Email, code)); err != nil {
			log.Errorf(c, "failed to send slack alert for admin %s login verification code: %v", hideEmail(req.Email), err)
		} else {
			log.Debugf(c, "successfully sent slack alert for admin %s login verification code", hideEmail(req.Email))
		}
	}

	log.Infof(c, "successfully sent auth code to email: %s, userExists: %v", hideEmail(req.Email), userExists)

	// 返回用户存在状态、激活状态和首单状态
	response := &SendCodeResponse{
		UserExists:       userExists,
		IsActivated:      false, // 默认值
		IsFirstOrderDone: false, // 默认值
	}

	// 如果用户存在，设置激活状态和首单状态
	if user != nil {
		response.IsActivated = user.IsActivated != nil && *user.IsActivated
		response.IsFirstOrderDone = user.IsFirstOrderDone != nil && *user.IsFirstOrderDone
	}

	Success(c, response)
}

// api_login 用户登录
//
func api_login(c *gin.Context) {
	var req DataLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnf(c, "invalid login request: %v", err)
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}
	if err := cleanEmailField(c, &req.Email); err != nil {
		log.Warnf(c, "invalid email format on login: %v", err)
		Error(c, ErrorInvalidArgument, "invalid email format")
		return
	}
	log.Infof(c, "login request from email: %s, udid: %s", req.Email, req.UDID)

	// Reject unknown client-class tokens early — silently creating a Device row
	// here would create an unrecoverable account once EnforceDeviceClass kicks in
	// on the next request. Header absence is still legal (legacy compat).
	if rawHeader := c.GetHeader("X-K2-Client"); rawHeader != "" && parseClientHeader(rawHeader) == nil {
		log.Warnf(c, "login rejected: invalid client class header=%q remote=%s", rawHeader, c.ClientIP())
		Error(c, ErrorInvalidClientClass, "invalid client class token")
		return
	}

	indexID := secretHashIt(c, []byte(req.Email))

	switch verifyEmailCode(c, indexID, req.VerificationCode) {
	case VerifyCodeOK:
		// fall through to login flow
	case VerifyCodeNotIssued:
		log.Warnf(c, "verification code expired or not issued for email: %s", req.Email)
		Error(c, ErrorVerificationCodeExpired, "verification code expired or not sent")
		return
	case VerifyCodeWrong:
		log.Warnf(c, "invalid verification code for email: %s", req.Email)
		Error(c, ErrorInvalidVerificationCode, "invalid verification code")
		return
	}

	// 缩短 TTL 到宽限期（非立即删除），让双击 / 网络抖动重试在宽限期内幂等成功
	if err := markVerificationCodeUsed(c, indexID); err != nil {
		log.Errorf(c, "failed to mark verification code used for email %s: %v", req.Email, err)
		Error(c, ErrorSystemError, "failed to mark verification code used")
		return
	}

	var identify LoginIdentify
	if err := db.Get().Where("type = ? AND index_id = ? AND brand = ?", "email", indexID, string(ReqBrand(c))).First(&identify).Error; err != nil {
		if util.DbIsNotFoundErr(err) {
			log.Warnf(c, "user not found during login for email (hashed): %s", indexID)
			Error(c, ErrorNotFound, "user not found")
			return
		} else {
			log.Errorf(c, "failed to check user during login for email (hashed) %s: %v", indexID, err)
			Error(c, ErrorSystemError, "failed to check user")
			return
		}
	}

	authResult, err := completeDeviceLogin(c, identify.UserID, deviceLoginParams{
		UDID:       req.UDID,
		Remark:     req.Remark,
		Language:   req.Language,
		Email:      req.Email,
		InviteCode: req.InviteCode,
	})
	if err != nil {
		log.Errorf(c, "login transaction failed for email %s: %v", req.Email, err)
		ErrorE(c, err)
		return
	}

	log.Infof(c, "user %d logged in successfully with device %s", identify.UserID, req.UDID)
	Success(c, authResult)
}

// api_refresh_token 刷新 token
//
func api_refresh_token(c *gin.Context) {
	var refreshToken string

	// 优先从 Cookie 读取 refresh token (Web 端)
	if cookieToken, err := c.Cookie(CookieRefreshToken); err == nil && cookieToken != "" {
		log.Debugf(c, "using refresh token from cookie")
		refreshToken = cookieToken
	} else {
		// 从 request body 读取 (非 Web 端，如 Desktop/Mobile)
		var req struct {
			RefreshToken string `json:"refreshToken"`
		}
		if err := c.ShouldBindJSON(&req); err == nil && req.RefreshToken != "" {
			log.Debugf(c, "using refresh token from request body")
			refreshToken = req.RefreshToken
		}
	}

	if refreshToken == "" {
		log.Warnf(c, "no refresh token provided")
		Error(c, ErrorInvalidArgument, "refresh token required")
		return
	}
	log.Infof(c, "request to refresh token")

	claims, device, err := validateToken(c, refreshToken, TokenTypeRefresh)
	if err != nil {
		log.Warnf(c, "failed to validate refresh token: %v", err)
		ErrorE(c, err)
		return
	}

	var user User
	if err := db.Get().First(&user, claims.UserID).Error; err != nil {
		log.Errorf(c, "failed to load user %d during token refresh: %v", claims.UserID, err)
		Error(c, ErrorSystemError, "failed to refresh token")
		return
	}
	if isUserBlocked(&user) {
		log.Warnf(c, "token refresh rejected: user %d is blocked", claims.UserID)
		Error(c, ErrorForbidden, "account blocked")
		return
	}

	authResult, tokenIssueTime, err := generateTokens(c, claims.UserID, claims.DeviceID, claims.Roles)
	if err != nil {
		log.Errorf(c, "failed to generate new access token for user %d: %v", claims.UserID, err)
		Error(c, ErrorSystemError, "failed to generate new access token")
		return
	}

	device.TokenIssueAt = tokenIssueTime.Unix()
	device.TokenLastUsedAt = time.Now().Unix()
	if err := db.Get().Save(&device).Error; err != nil {
		log.Errorf(c, "failed to save new token info for user %d, device %s: %v", claims.UserID, claims.DeviceID, err)
		Error(c, ErrorSystemError, "failed to save new token info")
		return
	}

	// 更新 HttpOnly Cookie（Web 端）
	setAuthCookies(c, authResult)

	log.Infof(c, "successfully refreshed token for user %d, device %s", claims.UserID, claims.DeviceID)
	Success(c, authResult)
}

// api_logout 用户登出
//
func api_logout(c *gin.Context) {
	udid := ReqUDID(c)
	userID := ReqUserID(c)
	log.Infof(c, "user %d request to logout from device %s", userID, udid)

	// 清除认证 Cookie（Web 端）
	clearAuthCookies(c)

	// 如果是设备认证，删除设备记录。零身份（设备账号）保留设备行：udid 是找回账号的唯一凭据，
	// 删了重装即丢会员（spec §4.4）。
	if udid != "" {
		has, err := userHasIdentity(db.Get(), userID)
		if err != nil {
			log.Errorf(c, "failed to check login identity for user %d on logout: %v", userID, err)
			Error(c, ErrorSystemError, "failed to logout")
			return
		}
		if !has {
			// 保留设备行（找回凭据），但作废已签发的 token：单调推进 token_issue_at
			// （同一秒内登录后立刻登出也必须与旧值不同）。/device-account 找回会重新签发。
			if err := db.Get().Model(&Device{}).Where("udid = ? AND user_id = ?", udid, userID).
				Update("token_issue_at", gorm.Expr("GREATEST(?, token_issue_at + 1)", time.Now().Unix())).Error; err != nil {
				log.Errorf(c, "failed to revoke tokens of device %s for user %d on logout: %v", udid, userID, err)
				Error(c, ErrorSystemError, "failed to logout")
				return
			}
			log.Infof(c, "user %d has no login identity, keeping device %s but revoking its tokens on logout", userID, udid)
			SuccessEmpty(c)
			return
		}
		if err := db.Get().Where("udid = ? AND user_id = ?", udid, userID).Delete(&Device{}).Error; err != nil {
			log.Errorf(c, "failed to delete device %s from database for user %d: %v", udid, userID, err)
			Error(c, ErrorSystemError, "failed to logout")
			return
		}
		log.Infof(c, "user %d successfully logged out from device %s", userID, udid)
	} else {
		// Web 认证登出，只清除 Cookie
		log.Infof(c, "user %d successfully logged out from web (cookies cleared)", userID)
	}

	SuccessEmpty(c)
}

// api_web_auth Web用户登录（无设备绑定）
//
func api_web_auth(c *gin.Context) {
	var req DataWebLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnf(c, "invalid web login request: %v", err)
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}
	if err := cleanEmailField(c, &req.Email); err != nil {
		log.Warnf(c, "invalid email format on web login: %v", err)
		Error(c, ErrorInvalidArgument, "invalid email format")
		return
	}
	log.Infof(c, "web login request from email: %s", req.Email)

	indexID := secretHashIt(c, []byte(req.Email))

	switch verifyEmailCode(c, indexID, req.VerificationCode) {
	case VerifyCodeOK:
		// fall through
	case VerifyCodeNotIssued:
		log.Warnf(c, "verification code expired or not issued for web login email: %s", req.Email)
		Error(c, ErrorVerificationCodeExpired, "verification code expired or not sent")
		return
	case VerifyCodeWrong:
		log.Warnf(c, "invalid verification code for web login email: %s", req.Email)
		Error(c, ErrorInvalidVerificationCode, "invalid verification code")
		return
	}

	if err := markVerificationCodeUsed(c, indexID); err != nil {
		log.Errorf(c, "failed to mark verification code used for email %s: %v", req.Email, err)
		Error(c, ErrorSystemError, "failed to mark verification code used")
		return
	}

	var identify LoginIdentify
	if err := db.Get().Where(&LoginIdentify{Type: "email", IndexID: indexID, Brand: string(ReqBrand(c))}).First(&identify).Error; err != nil {
		if util.DbIsNotFoundErr(err) {
			log.Warnf(c, "user not found during web login for email (hashed): %s", indexID)
			Error(c, ErrorNotFound, "user not found")
			return
		} else {
			log.Errorf(c, "failed to check user during web login for email (hashed) %s: %v", indexID, err)
			Error(c, ErrorSystemError, "failed to check user")
			return
		}
	}

	resp, err := completeWebLogin(c, identify.UserID, webLoginParams{
		Language:   req.Language,
		Email:      req.Email,
		InviteCode: req.InviteCode,
	})
	if err != nil {
		ErrorE(c, err)
		return
	}

	// Tokens 通过 HttpOnly Cookie 设置，AccessToken 也在 body 里返回供
	// fallback 使用（iOS 微信 WKWebView 等不持久化 Set-Cookie 的环境）。
	Success(c, resp)
}

type AuthWithDeviceRequest struct {
	UDID string `json:"udid" binding:"required"`
}

// api_auth_with_device 通过设备udid认证（已废弃，保留用于向后兼容）
//
func api_auth_with_device(c *gin.Context) {
	// 该功能已废弃，统一返回 403 错误，保留接口用于向后兼容
	log.Infof(c, "deprecated api_auth_with_device called, returning forbidden error")
	Error(c, ErrorForbidden, "device not found")
}

// ===================== Cookie 认证辅助函数 =====================

// setAuthCookies 设置认证相关的 HttpOnly Cookie
// 用于 Web 端安全认证，防止 XSS 攻击窃取 token
// 使用 sliding expiration 模式：access_token 有效期 2 个月，不需要 refresh_token
// 安全属性：HttpOnly + Secure (HTTPS) + SameSite=Lax
func setAuthCookies(c *gin.Context, authResult *DataAuthResult) {
	// 判断是否为 HTTPS 请求
	isSecure := c.GetHeader("X-Forwarded-Proto") == "https" ||
		c.Request.TLS != nil

	// Web Cookie 有效期（2个月，单位秒）
	cookieMaxAge := int(WebCookieTokenExpiry.Seconds())

	// 设置 SameSite 属性（防止 CSRF 攻击的第二道防线）
	// Lax 模式：允许顶级导航的 GET 请求携带 Cookie，阻止跨站 POST
	c.SetSameSite(http.SameSiteLaxMode)

	// Access Token Cookie (HttpOnly)
	c.SetCookie(
		CookieAccessToken,
		authResult.AccessToken,
		cookieMaxAge,
		"/",
		"",       // Domain (auto)
		isSecure, // Secure
		true,     // HttpOnly
	)

	// 注意：Web Cookie 模式不需要 refresh_token
	// 通过 middleware 的 sliding expiration 机制自动续期

	// CSRF Token Cookie (非 HttpOnly，前端需要读取并附加到请求头)
	csrfToken := GenerateCSRFToken()
	c.SetCookie(
		CookieCSRFToken,
		csrfToken,
		cookieMaxAge,
		"/",
		"",
		isSecure,
		false, // 非 HttpOnly，前端可读取
	)
}

// clearAuthCookies 清除所有认证相关的 Cookie
// 用于登出时清理客户端认证状态
func clearAuthCookies(c *gin.Context) {
	// 通过设置 MaxAge=-1 来删除 Cookie
	c.SetCookie(CookieAccessToken, "", -1, "/", "", false, true)
	c.SetCookie(CookieCSRFToken, "", -1, "/", "", false, false)
	// 注意：不再使用 refresh_token cookie
}

// PasswordLoginRequest request body for password login
type PasswordLoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
	// Device info for binding
	UDID   string `json:"udid" binding:"required"`
	Remark string `json:"remark"`
	// Device info for email notification
	DeviceName string `json:"deviceName"`
	Platform   string `json:"platform"`
	// Language preference
	Language string `json:"language"`
}

// api_password_login handles password-based authentication
func api_password_login(c *gin.Context) {
	var req PasswordLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnf(c, "invalid password login request: %v", err)
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}
	if err := cleanEmailField(c, &req.Email); err != nil {
		log.Warnf(c, "invalid email format on password login: %v", err)
		Error(c, ErrorInvalidArgument, "invalid email format")
		return
	}
	log.Infof(c, "password login request from email: %s, udid: %s", hideEmail(req.Email), req.UDID)

	// Reject unknown client-class tokens early — silently creating a Device row
	// here would create an unrecoverable account once EnforceDeviceClass kicks in
	// on the next request. Header absence is still legal (legacy compat).
	if rawHeader := c.GetHeader("X-K2-Client"); rawHeader != "" && parseClientHeader(rawHeader) == nil {
		log.Warnf(c, "login rejected: invalid client class header=%q remote=%s", rawHeader, c.ClientIP())
		Error(c, ErrorInvalidClientClass, "invalid client class token")
		return
	}

	indexID := secretHashIt(c, []byte(req.Email))

	// Find user by email
	var identify LoginIdentify
	if err := db.Get().Preload("User").Where("type = ? AND index_id = ? AND brand = ?", "email", indexID, string(ReqBrand(c))).First(&identify).Error; err != nil {
		if util.DbIsNotFoundErr(err) {
			log.Warnf(c, "user not found for password login, email (hashed): %s", indexID)
			// Use generic error to prevent email enumeration
			Error(c, ErrorInvalidCredentials, "invalid email or password")
			return
		}
		log.Errorf(c, "failed to find user for password login: %v", err)
		Error(c, ErrorSystemError, "login failed")
		return
	}

	user := identify.User
	if user == nil {
		log.Errorf(c, "user object is nil for identify %d", identify.ID)
		Error(c, ErrorSystemError, "login failed")
		return
	}

	if isUserBlocked(user) {
		log.Warnf(c, "password login rejected: user %d is blocked", user.ID)
		Error(c, ErrorForbidden, "account blocked")
		return
	}

	// Check if password is set
	if !HasPasswordSet(user) {
		log.Warnf(c, "user %d has no password set", user.ID)
		Error(c, ErrorInvalidCredentials, "invalid email or password")
		return
	}

	// Check if account is locked
	if IsAccountLocked(user) {
		remainingSeconds := user.PasswordLockedUntil - time.Now().Unix()
		log.Warnf(c, "user %d account is locked, remaining: %ds", user.ID, remainingSeconds)
		Error(c, ErrorTooManyRequests, "account temporarily locked")
		return
	}

	// Verify password
	if !UserPasswordVerify(req.Password, user.PasswordHash) {
		log.Warnf(c, "invalid password for user %d", user.ID)
		if err := RecordFailedPasswordAttempt(c, user); err != nil {
			log.Errorf(c, "failed to record failed attempt for user %d: %v", user.ID, err)
		}
		Error(c, ErrorInvalidCredentials, "invalid email or password")
		return
	}

	// Reset failed attempts on success
	if err := ResetFailedPasswordAttempts(c, user); err != nil {
		log.Errorf(c, "failed to reset failed attempts for user %d: %v", user.ID, err)
	}

	// Proceed with device binding (similar to api_login)
	var device Device
	var authResult *DataAuthResult
	var err error

	err = db.Get().Transaction(func(tx *gorm.DB) error {
		// Check for device transfer
		var oldDevice Device
		oldDeviceErr := tx.Where("udid = ?", req.UDID).First(&oldDevice).Error
		if oldDeviceErr == nil && oldDevice.UserID != identify.UserID {
			log.Warnf(c, "device transfer detected: udid=%s, from user %d to user %d",
				req.UDID, oldDevice.UserID, identify.UserID)
			if err := notifyDeviceTransfer(c, tx, &oldDevice); err != nil {
				return err
			}
		}

		// Delete old device record
		if err := tx.Where("udid = ?", req.UDID).Delete(&Device{}).Error; err != nil {
			return err
		}

		// Check device limit by type (app vs router)
		if err := checkDeviceLimitOrKick(c, tx, user, isGatewayRequest(c)); err != nil {
			return err
		}

		// Generate tokens
		var tokenIssueTime time.Time
		authResult, tokenIssueTime, err = generateTokens(c, identify.UserID, req.UDID, user.Roles)
		if err != nil {
			return err
		}

		// Create device record
		device = Device{
			UDID:            req.UDID,
			Remark:          req.Remark,
			UserID:          identify.UserID,
			TokenIssueAt:    tokenIssueTime.Unix(),
			TokenLastUsedAt: time.Now().Unix(),
		}
		createDeviceWithAppInfo(c, &device)
		return tx.Create(&device).Error
	})

	if err != nil {
		log.Errorf(c, "password login transaction failed for user %d: %v", user.ID, err)
		ErrorE(c, err)
		return
	}

	// Send password login notification email
	meta := PasswordLoginMeta{
		DeviceName: req.DeviceName,
		Platform:   req.Platform,
		ClientIP:   c.ClientIP(),
		LoginTime:  time.Now().Format("2006-01-02 15:04:05"),
	}
	if meta.DeviceName == "" {
		meta.DeviceName = req.Remark
	}
	if meta.Platform == "" {
		meta.Platform = "Unknown"
	}
	if err := emailToUser(c, int64(identify.UserID), brandedPasswordLoginTemplate, meta); err != nil {
		log.Errorf(c, "failed to send password login email to user %d: %v", identify.UserID, err)
	}

	log.Infof(c, "user %d logged in via password with device %s", identify.UserID, device.UDID)
	Success(c, authResult)
}

// WebPasswordLoginRequest is the request body for cookie-based password
// login on the public website. No UDID, no device binding — pure
// authentication that returns an HttpOnly cookie (plus accessToken in body
// for WebView fallback).
type WebPasswordLoginRequest struct {
	Email      string `json:"email" binding:"required,email"`
	Password   string `json:"password" binding:"required"`
	Language   string `json:"language"`
	InviteCode string `json:"inviteCode"`
}

// api_web_password_login handles cookie-based password authentication for
// the public website. Mirrors api_web_auth's transaction template
// (language + invite + activation) but gates on password instead of
// verification code, using the same lock/strength helpers as the
// device-bound api_password_login.
//
// Anti-enumeration: unknown user, wrong password, and "password not set"
// all collapse to ErrorInvalidCredentials. Only IsAccountLocked diverges
// (ErrorTooManyRequests), which is acceptable because the lockout state
// is observable to the attacker regardless.
func api_web_password_login(c *gin.Context) {
	var req WebPasswordLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnf(c, "invalid web password login request: %v", err)
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}
	if err := cleanEmailField(c, &req.Email); err != nil {
		log.Warnf(c, "invalid email format on web password login: %v", err)
		Error(c, ErrorInvalidArgument, "invalid email format")
		return
	}
	log.Infof(c, "web password login request from email: %s", hideEmail(req.Email))

	indexID := secretHashIt(c, []byte(req.Email))

	var identify LoginIdentify
	if err := db.Get().Preload("User").Where("type = ? AND index_id = ? AND brand = ?", "email", indexID, string(ReqBrand(c))).First(&identify).Error; err != nil {
		if util.DbIsNotFoundErr(err) {
			log.Warnf(c, "user not found for web password login, email (hashed): %s", indexID)
			Error(c, ErrorInvalidCredentials, "invalid email or password")
			return
		}
		log.Errorf(c, "failed to find user for web password login: %v", err)
		Error(c, ErrorSystemError, "login failed")
		return
	}

	user := identify.User
	if user == nil {
		log.Errorf(c, "user object is nil for identify %d", identify.ID)
		Error(c, ErrorSystemError, "login failed")
		return
	}

	if isUserBlocked(user) {
		log.Warnf(c, "web password login rejected: user %d is blocked", user.ID)
		Error(c, ErrorForbidden, "account blocked")
		return
	}

	if !HasPasswordSet(user) {
		log.Warnf(c, "user %d has no password set (web)", user.ID)
		Error(c, ErrorInvalidCredentials, "invalid email or password")
		return
	}

	if IsAccountLocked(user) {
		remainingSeconds := user.PasswordLockedUntil - time.Now().Unix()
		log.Warnf(c, "user %d account is locked (web), remaining: %ds", user.ID, remainingSeconds)
		Error(c, ErrorTooManyRequests, "account temporarily locked")
		return
	}

	if !UserPasswordVerify(req.Password, user.PasswordHash) {
		log.Warnf(c, "invalid password for user %d (web)", user.ID)
		if err := RecordFailedPasswordAttempt(c, user); err != nil {
			log.Errorf(c, "failed to record failed attempt for user %d: %v", user.ID, err)
		}
		Error(c, ErrorInvalidCredentials, "invalid email or password")
		return
	}

	if err := ResetFailedPasswordAttempts(c, user); err != nil {
		log.Errorf(c, "failed to reset failed attempts for user %d: %v", user.ID, err)
	}

	var userIsAdmin bool
	var userRoles uint64

	err := db.Get().Transaction(func(tx *gorm.DB) error {
		var u User
		if err := tx.First(&u, identify.UserID).Error; err != nil {
			return err
		}
		userIsAdmin = u.IsAdmin != nil && *u.IsAdmin
		userRoles = u.Roles

		needSave := false

		if req.Language != "" {
			acceptLanguage := c.GetHeader("Accept-Language")
			detectedLanguage := detectUserLanguage(c, req.Language, req.Email, acceptLanguage)
			if detectedLanguage != u.Language {
				u.Language = detectedLanguage
				needSave = true
			}
		}

		// Invite code: only honoured for unactivated users — same rule as
		// the web verify-code login path.
		if req.InviteCode != "" && (u.IsActivated == nil || !*u.IsActivated) {
			inviteCodeID := InviteCodeID(req.InviteCode)
			var inviteCode InviteCode
			if err := tx.First(&inviteCode, inviteCodeID).Error; err != nil {
				if util.DbIsNotFoundErr(err) {
					return e(ErrorInvalidInviteCode, "invalid invite code")
				}
				return err
			}
			if inviteCode.UserID == identify.UserID {
				return e(ErrorSelfInvitation, "cannot use your own invite code")
			}
			u.InvitedByCodeID = inviteCodeID
			needSave = true
			go handleInviteDownloadReward(c, identify.UserID)
		}

		if u.IsActivated == nil || !*u.IsActivated {
			u.IsActivated = BoolPtr(true)
			u.ActivatedAt = time.Now().Unix()
			needSave = true
		}

		if needSave {
			if err := tx.Save(&u).Error; err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		log.Errorf(c, "web password login transaction failed for user %d: %v", identify.UserID, err)
		ErrorE(c, err)
		return
	}

	authResult, _, err := generateWebCookieToken(c, identify.UserID, userRoles)
	if err != nil {
		log.Errorf(c, "failed to generate web cookie token for user %d: %v", identify.UserID, err)
		Error(c, ErrorSystemError, "failed to generate tokens")
		return
	}

	setAuthCookies(c, authResult)
	// 漏斗：把匿名 sid 关联到该用户（只读不种；非阻塞，失败不影响登录）。
	if sid, optedOut := readFunnelSid(c); !optedOut && !funnelGPC(c) && sid != "" {
		linkFunnelIdentity(c, "sid", sid, identify.UserID, ReqBrand(c))
	}

	// Reuse the existing webLoginTemplate — users get the same notification
	// email regardless of whether they used the code or password path.
	meta := WebLoginMeta{
		LoginTime: time.Now().Format("2006-01-02 15:04:05"),
		ClientIP:  c.ClientIP(),
	}
	if err := emailToUser(c, int64(identify.UserID), brandedWebLoginTemplate, meta); err != nil {
		log.Errorf(c, "failed to send web login email to user %d: %v", identify.UserID, err)
	}

	log.Infof(c, "user %d successfully logged in via web password", identify.UserID)
	// HasPassword is unconditionally true on this code path — we just verified
	// the user's password to get here. Routed through HasPasswordSet for
	// consistency with api_web_auth (cheap, no extra query).
	Success(c, &DataWebLoginResponse{
		User: DataWebLoginUser{
			ID:          identify.UserID,
			Email:       req.Email,
			IsAdmin:     userIsAdmin,
			Roles:       userRoles,
			HasPassword: HasPasswordSet(user),
		},
		AccessToken: authResult.AccessToken,
	})
}


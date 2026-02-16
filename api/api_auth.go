// Package center 提供 Kaitu 中心服务的 API 接口
package center

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
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

// sendCodeWithMode 发送验证码的内部实现
func sendCodeWithMode(c *gin.Context, userExistRequired bool) {
	var req SendAuthCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnf(c, "invalid send code request: %v", err)
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}
	req.Email = strings.ToLower(req.Email)

	log.Infof(c, "request to send auth code to email: %s, userExistRequired: %v", req.Email, userExistRequired)
	indexID := secretHashIt(c, []byte(req.Email))

	// 查找现有用户
	var identify LoginIdentify
	var user *User
	userExists := false

	if err := db.Get().Preload("User").Where("type = ? AND index_id = ?", "email", indexID).First(&identify).Error; err != nil {
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
			newUser, err := FindOrCreateUserByEmail(c, req.Email, req.Language, acceptLanguage)
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

	// 生成验证码
	code := generateVerificationCode(c, indexID)
	expireMinutes := 5

	// 发送验证码邮件
	meta := VerificationCodeMeta{
		UserEmail:     req.Email,
		Code:          code,
		ExpireMinutes: expireMinutes,
	}
	if err := emailTo(c, req.Email, verificationCodeTemplate, meta); err != nil {
		log.Errorf(c, "failed to send verification code email to %s: %v", req.Email, err)
		Error(c, ErrorSystemError, err.Error())
		return
	}

	// 保存验证码
	if err := saveEmailVerificationCode(c, indexID, code, expireMinutes); err != nil {
		log.Errorf(c, "failed to save verification code for email (hashed) %s: %v", indexID, err)
		Error(c, ErrorSystemError, "failed to save verification code")
		return
	}

	// 发送管理员 Slack 通知
	if user != nil && user.IsAdmin != nil && *user.IsAdmin {
		err := slack.Send("verify", fmt.Sprintf("管理员 %s 登录验证码: %s", req.Email, code))
		if err != nil {
			log.Errorf(c, "failed to send slack alert for admin %s login verification code: %s: %v", hideEmail(req.Email), code, err)
		} else {
			log.Debugf(c, "successfully sent slack alert for admin %s login verification code: %s", hideEmail(req.Email), code)
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
	req.Email = strings.ToLower(req.Email)
	log.Infof(c, "login request from email: %s, udid: %s", req.Email, req.UDID)

	indexID := secretHashIt(c, []byte(req.Email))

	if !verifyEmailCode(c, indexID, req.VerificationCode) {
		log.Warnf(c, "invalid verification code for email: %s", req.Email)
		Error(c, ErrorInvalidVerificationCode, "invalid verification code")
		return
	}

	if err := deleteVerificationCode(c, req.Email); err != nil {
		log.Errorf(c, "failed to delete verification code for email %s: %v", req.Email, err)
		Error(c, ErrorSystemError, "failed to delete verification code")
		return
	}

	var identify LoginIdentify
	if err := db.Get().Where("type = ? AND index_id = ?", "email", indexID).First(&identify).Error; err != nil {
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

	var device Device
	var authResult *DataAuthResult
	var err error

	err = db.Get().Transaction(func(tx *gorm.DB) error {
		// 第1步：查询是否存在旧设备记录（用于检测设备转移）
		var oldDevice Device
		oldDeviceErr := tx.Where("udid = ?", req.UDID).First(&oldDevice).Error
		if oldDeviceErr == nil {
			// 检查是否发生设备转移
			if oldDevice.UserID != identify.UserID {
				log.Warnf(c, "device transfer detected: udid=%s, from user %d to user %d, old_remark=%s",
					req.UDID, oldDevice.UserID, identify.UserID, oldDevice.Remark)

				// 发送邮件通知原所有者
				transferMeta := DeviceTransferMeta{
					TransferTime: time.Now().Format("2006-01-02 15:04:05"),
					DeviceRemark: oldDevice.Remark,
				}
				if err := emailToUser(c, int64(oldDevice.UserID), deviceTransferTemplate, transferMeta); err != nil {
					log.Errorf(c, "failed to send device transfer email to user %d: %v", oldDevice.UserID, err)
					// 不阻止登录流程，仅记录错误
				}
			}
		} else if !util.DbIsNotFoundErr(oldDeviceErr) {
			log.Errorf(c, "failed to check existing device: %v", oldDeviceErr)
			return oldDeviceErr
		}

		// 第2步：删除旧设备记录
		if err := tx.Where("udid = ?", req.UDID).Delete(&Device{}).Error; err != nil {
			log.Errorf(c, "failed to delete existing device with udid %s in transaction: %v", req.UDID, err)
			return err
		}

		// 获取用户信息以获取最大设备数限制
		var user User
		if err := tx.First(&user, identify.UserID).Error; err != nil {
			log.Errorf(c, "failed to get user %d: %v", identify.UserID, err)
			return err
		}

		// 追踪是否需要保存用户信息
		needSave := false

		// 如果提供了语言偏好，更新用户语言设置
		if req.Language != "" {
			acceptLanguage := c.GetHeader("Accept-Language")
			detectedLanguage := detectUserLanguage(c, req.Language, req.Email, acceptLanguage)
			if detectedLanguage != user.Language {
				user.Language = detectedLanguage
				needSave = true
				log.Infof(c, "will update user %d language to: %s", identify.UserID, detectedLanguage)
			}
		}

		// 处理邀请码逻辑（仅未激活用户可以设置邀请码）
		if req.InviteCode != "" {
			if user.IsActivated == nil || !*user.IsActivated {
				log.Infof(c, "user %d is not activated, processing invite code: %s", identify.UserID, req.InviteCode)
				inviteCodeID := InviteCodeID(req.InviteCode)
				var inviteCode InviteCode
				if err := tx.First(&inviteCode, inviteCodeID).Error; err != nil {
					if util.DbIsNotFoundErr(err) {
						log.Warnf(c, "invalid invite code %s for user %d", req.InviteCode, identify.UserID)
						return e(ErrorInvalidInviteCode, "invalid invite code")
					}
					log.Errorf(c, "failed to check invite code %s: %v", req.InviteCode, err)
					return err
				}

				// 检查自邀请
				if inviteCode.UserID == identify.UserID {
					log.Warnf(c, "self-invitation detected for user %d with code %s", identify.UserID, req.InviteCode)
					return e(ErrorSelfInvitation, "cannot use your own invite code")
				}

				// 设置邀请码
				user.InvitedByCodeID = inviteCodeID
				needSave = true
				log.Infof(c, "will set invite code %s for user %d", req.InviteCode, identify.UserID)

				// 异步处理邀请奖励
				go handleInviteDownloadReward(c, identify.UserID)
			} else {
				log.Infof(c, "user %d is already activated, ignoring invite code", identify.UserID)
			}
		}

		// 设置用户为已激活状态
		if user.IsActivated == nil || !*user.IsActivated {
			user.IsActivated = BoolPtr(true)
			user.ActivatedAt = time.Now().Unix()
			needSave = true
			log.Infof(c, "will set user %d as activated at %d", identify.UserID, user.ActivatedAt)
		}

		// 统一保存所有用户信息修改
		if needSave {
			if err := tx.Save(&user).Error; err != nil {
				log.Errorf(c, "failed to save user %d updates: %v", identify.UserID, err)
				return err
			}
			log.Infof(c, "successfully saved user %d updates (language: %s, activated: %v, invite code ID: %d)",
				identify.UserID, user.Language, user.IsActivated, user.InvitedByCodeID)
		}

		var deviceCount int64
		if err := tx.Model(&Device{}).Where("user_id = ?", identify.UserID).Count(&deviceCount).Error; err != nil {
			log.Errorf(c, "failed to count devices for user %d: %v", identify.UserID, err)
			return err
		}

		if deviceCount >= int64(user.MaxDevice) {
			log.Warnf(c, "device limit reached for user %d, will remove oldest device", identify.UserID)
			var oldestDevice Device
			if err := tx.Where("user_id = ?", identify.UserID).Order("token_last_used_at ASC").First(&oldestDevice).Error; err != nil {
				log.Errorf(c, "failed to find oldest device for user %d: %v", identify.UserID, err)
				return err
			}
			if err := tx.Delete(&oldestDevice).Error; err != nil {
				log.Errorf(c, "failed to delete oldest device %s for user %d: %v", oldestDevice.UDID, identify.UserID, err)
				return err
			}
			log.Infof(c, "deleted oldest device %s for user %d", oldestDevice.UDID, identify.UserID)

			meta := DeviceKickMeta{
				KickTime: time.Now().Format("2006-01-02 15:04:05"),
				Remark:   oldestDevice.Remark,
			}
			if err := emailToUser(c, int64(identify.UserID), deviceKickTemplate, meta); err != nil {
				log.Errorf(c, "failed to send device kick email to user %d: %v", identify.UserID, err)
			}
		}

		var tokenIssueTime time.Time
		authResult, tokenIssueTime, err = generateTokens(c, identify.UserID, req.UDID, user.Roles)
		if err != nil {
			log.Errorf(c, "failed to generate tokens during login for user %d: %v", identify.UserID, err)
			return err
		}

		device = Device{
			UDID:            req.UDID,
			Remark:          req.Remark,
			UserID:          identify.UserID,
			TokenIssueAt:    tokenIssueTime.Unix(),
			TokenLastUsedAt: time.Now().Unix(),
		}
		if err := tx.Create(&device).Error; err != nil {
			log.Errorf(c, "failed to create new device in transaction for user %d: %v", identify.UserID, err)
			return err
		}
		log.Debugf(c, "created new device %s for user %d", device.UDID, device.UserID)

		meta := NewDeviceLoginMeta{
			LoginTime: time.Now().Format("2006-01-02 15:04:05"),
			Remark:    device.Remark,
		}
		if err := emailToUser(c, int64(identify.UserID), newDeviceLoginTemplate, meta); err != nil {
			log.Errorf(c, "failed to send new device login email to user %d: %v", identify.UserID, err)
		}

		return nil
	})

	if err != nil {
		log.Errorf(c, "login transaction failed for email %s: %v", req.Email, err)
		ErrorE(c, err)
		return
	}

	log.Infof(c, "user %d logged in successfully with device %s", identify.UserID, device.UDID)
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

	// 如果是设备认证，删除设备记录
	if udid != "" {
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
	req.Email = strings.ToLower(req.Email)
	log.Infof(c, "web login request from email: %s", req.Email)

	indexID := secretHashIt(c, []byte(req.Email))

	if !verifyEmailCode(c, indexID, req.VerificationCode) {
		log.Warnf(c, "invalid verification code for web login email: %s", req.Email)
		Error(c, ErrorInvalidVerificationCode, "invalid verification code")
		return
	}

	if err := deleteVerificationCode(c, req.Email); err != nil {
		log.Errorf(c, "failed to delete verification code for email %s: %v", req.Email, err)
		Error(c, ErrorSystemError, "failed to delete verification code")
		return
	}

	var identify LoginIdentify
	if err := db.Get().Where(&LoginIdentify{Type: "email", IndexID: indexID}).First(&identify).Error; err != nil {
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

	var authResult *DataAuthResult
	var err error
	var userIsAdmin bool  // 用于响应中返回用户信息
	var userRoles uint64  // 用于 JWT 中的角色

	// 使用事务处理用户信息更新和邀请码设置
	err = db.Get().Transaction(func(tx *gorm.DB) error {
		// 获取用户信息
		var user User
		if err := tx.First(&user, identify.UserID).Error; err != nil {
			log.Errorf(c, "failed to get user %d: %v", identify.UserID, err)
			return err
		}
		// 保存用户信息用于响应和 JWT
		userIsAdmin = user.IsAdmin != nil && *user.IsAdmin
		userRoles = user.Roles

		// 追踪是否需要保存用户信息
		needSave := false

		// 如果提供了语言偏好，更新用户语言设置
		if req.Language != "" {
			acceptLanguage := c.GetHeader("Accept-Language")
			detectedLanguage := detectUserLanguage(c, req.Language, req.Email, acceptLanguage)
			if detectedLanguage != user.Language {
				user.Language = detectedLanguage
				needSave = true
				log.Infof(c, "will update user %d language to: %s", identify.UserID, detectedLanguage)
			}
		}

		// 处理邀请码逻辑（仅未激活用户可以设置邀请码）
		if req.InviteCode != "" {
			if user.IsActivated == nil || !*user.IsActivated {
				log.Infof(c, "user %d is not activated, processing invite code: %s", identify.UserID, req.InviteCode)
				inviteCodeID := InviteCodeID(req.InviteCode)
				var inviteCode InviteCode
				if err := tx.First(&inviteCode, inviteCodeID).Error; err != nil {
					if util.DbIsNotFoundErr(err) {
						log.Warnf(c, "invalid invite code %s for user %d", req.InviteCode, identify.UserID)
						return e(ErrorInvalidInviteCode, "invalid invite code")
					}
					log.Errorf(c, "failed to check invite code %s: %v", req.InviteCode, err)
					return err
				}

				// 检查自邀请
				if inviteCode.UserID == identify.UserID {
					log.Warnf(c, "self-invitation detected for user %d with code %s", identify.UserID, req.InviteCode)
					return e(ErrorSelfInvitation, "cannot use your own invite code")
				}

				// 设置邀请码
				user.InvitedByCodeID = inviteCodeID
				needSave = true
				log.Infof(c, "will set invite code %s for user %d", req.InviteCode, identify.UserID)

				// 异步处理邀请奖励
				go handleInviteDownloadReward(c, identify.UserID)
			} else {
				log.Infof(c, "user %d is already activated, ignoring invite code", identify.UserID)
			}
		}

		// 如果未激活用户没有提供邀请码，也需要激活账号
		if user.IsActivated == nil || !*user.IsActivated {
			user.IsActivated = BoolPtr(true)
			user.ActivatedAt = time.Now().Unix()
			needSave = true
			log.Infof(c, "will activate user %d (web login without invite code) at %d", identify.UserID, user.ActivatedAt)
		}

		// 保存用户信息
		if needSave {
			if err := tx.Save(&user).Error; err != nil {
				log.Errorf(c, "failed to save user %d: %v", identify.UserID, err)
				return err
			}
			log.Infof(c, "successfully saved user %d updates", identify.UserID)
		}

		return nil
	})

	if err != nil {
		log.Errorf(c, "failed to update user info for web login: %v", err)
		ErrorE(c, err)
		return
	}

	// 生成 Web Cookie 专用 token（2个月有效期，无 refresh token）
	authResult, _, err = generateWebCookieToken(c, identify.UserID, userRoles)
	if err != nil {
		log.Errorf(c, "failed to generate web cookie token for user %d: %v", identify.UserID, err)
		Error(c, ErrorSystemError, "failed to generate tokens")
		return
	}

	// 设置 HttpOnly Cookie（Web 端安全认证，sliding expiration）
	setAuthCookies(c, authResult)

	// 发送 Web 登录邮件通知
	meta := WebLoginMeta{
		LoginTime: time.Now().Format("2006-01-02 15:04:05"),
		ClientIP:  c.ClientIP(),
	}
	if err := emailToUser(c, int64(identify.UserID), webLoginTemplate, meta); err != nil {
		log.Errorf(c, "failed to send web login email to user %d: %v", identify.UserID, err)
		// 不影响登录流程，仅记录错误
	}

	log.Infof(c, "user %d successfully logged in via web", identify.UserID)

	// 返回用户信息（tokens已通过HttpOnly Cookie设置）
	Success(c, &DataWebLoginResponse{
		User: DataWebLoginUser{
			ID:      identify.UserID,
			Email:   req.Email,
			IsAdmin: userIsAdmin,
			Roles:   userRoles,
		},
	})
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
	req.Email = strings.ToLower(req.Email)
	log.Infof(c, "password login request from email: %s, udid: %s", hideEmail(req.Email), req.UDID)

	indexID := secretHashIt(c, []byte(req.Email))

	// Find user by email
	var identify LoginIdentify
	if err := db.Get().Preload("User").Where("type = ? AND index_id = ?", "email", indexID).First(&identify).Error; err != nil {
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

	// Check if password is set
	if !HasPasswordSet(user) {
		log.Warnf(c, "user %d has no password set", user.ID)
		Error(c, ErrorInvalidCredentials, "password not set")
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
			transferMeta := DeviceTransferMeta{
				TransferTime: time.Now().Format("2006-01-02 15:04:05"),
				DeviceRemark: oldDevice.Remark,
			}
			if err := emailToUser(c, int64(oldDevice.UserID), deviceTransferTemplate, transferMeta); err != nil {
				log.Errorf(c, "failed to send device transfer email: %v", err)
			}
		}

		// Delete old device record
		if err := tx.Where("udid = ?", req.UDID).Delete(&Device{}).Error; err != nil {
			return err
		}

		// Check device limit
		var deviceCount int64
		if err := tx.Model(&Device{}).Where("user_id = ?", identify.UserID).Count(&deviceCount).Error; err != nil {
			return err
		}

		if deviceCount >= int64(user.MaxDevice) {
			var oldestDevice Device
			if err := tx.Where("user_id = ?", identify.UserID).Order("token_last_used_at ASC").First(&oldestDevice).Error; err != nil {
				return err
			}
			if err := tx.Delete(&oldestDevice).Error; err != nil {
				return err
			}
			log.Infof(c, "deleted oldest device %s for user %d", oldestDevice.UDID, identify.UserID)

			meta := DeviceKickMeta{
				KickTime: time.Now().Format("2006-01-02 15:04:05"),
				Remark:   oldestDevice.Remark,
			}
			if err := emailToUser(c, int64(identify.UserID), deviceKickTemplate, meta); err != nil {
				log.Errorf(c, "failed to send device kick email: %v", err)
			}
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
	if err := emailToUser(c, int64(identify.UserID), passwordLoginTemplate, meta); err != nil {
		log.Errorf(c, "failed to send password login email to user %d: %v", identify.UserID, err)
	}

	log.Infof(c, "user %d logged in via password with device %s", identify.UserID, device.UDID)
	Success(c, authResult)
}

// ===================== WebSocket Token =====================

// WebSocketTokenExpiry WebSocket 认证 token 的有效期（5分钟）
// 短有效期因为：1) 仅用于 WebSocket 握手 2) 防止 token 泄露后的长期滥用
const WebSocketTokenExpiry = 5 * time.Minute

// DataWebSocketToken WebSocket 认证 token 响应
type DataWebSocketToken struct {
	Token     string `json:"token"`     // JWT token for WebSocket authentication
	ExpiresAt int64  `json:"expiresAt"` // Token expiration timestamp (Unix seconds)
}

// api_get_ws_token 获取用于 WebSocket 连接的短期认证 token
// WebSocket 连接无法携带跨域 Cookie，需要通过 URL 参数传递 token
// 此端点为已认证用户生成一个短期（5分钟）token，用于 WebSocket 握手
func api_get_ws_token(c *gin.Context) {
	user := ReqUser(c)
	if user == nil {
		log.Warnf(c, "ws-token request without authentication")
		Error(c, ErrorNotLogin, "authentication required")
		return
	}

	// Generate a short-lived token for WebSocket authentication
	jwtConfig := configJwt(c)
	jwtSecret := []byte(jwtConfig.Secret)

	now := time.Now()
	expiresAt := now.Add(WebSocketTokenExpiry)

	claims := TokenClaims{
		UserID:       user.ID,
		DeviceID:     "", // Web 认证无设备
		Exp:          expiresAt.Unix(),
		Type:         TokenTypeAccess,
		TokenIssueAt: now.Unix(),
		Roles:        user.Roles,
	}

	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(jwtSecret)
	if err != nil {
		log.Errorf(c, "failed to generate ws token for user %d: %v", user.ID, err)
		Error(c, ErrorSystemError, "failed to generate token")
		return
	}

	log.Infof(c, "generated ws token for user %d, expires at %v", user.ID, expiresAt)
	Success(c, &DataWebSocketToken{
		Token:     token,
		ExpiresAt: expiresAt.Unix(),
	})
}

package center

import (
	"time"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/util"
	"gorm.io/gorm"
)

// deviceLoginParams 设备登录事务的入参（验证码登录与第三方登录共用）。
// Email 只用于语言探测；InviteCode 为空则跳过邀请码逻辑。
type deviceLoginParams struct {
	UDID, Remark, Language, Email, InviteCode string
}

// completeDeviceLogin 身份已确认为 userID 后的设备登录事务：
// 设备转移邮件 → 删旧设备 → 封禁检查 → 语言/邀请码/激活 → 设备上限 → 签发 token → 建 Device → 新设备邮件。
// 返回的错误可能是 e(...) 富错误（封禁 → ErrorForbidden 等），调用方用 ErrorE 输出。
func completeDeviceLogin(c *gin.Context, userID uint64, p deviceLoginParams) (*DataAuthResult, error) {
	var device Device
	var authResult *DataAuthResult
	var err error

	err = db.Get().Transaction(func(tx *gorm.DB) error {
		// 第1步：查询是否存在旧设备记录（用于检测设备转移）
		var oldDevice Device
		oldDeviceErr := tx.Where("udid = ?", p.UDID).First(&oldDevice).Error
		if oldDeviceErr == nil {
			// 检查是否发生设备转移
			if oldDevice.UserID != userID {
				log.Warnf(c, "device transfer detected: udid=%s, from user %d to user %d, old_remark=%s",
					p.UDID, oldDevice.UserID, userID, oldDevice.Remark)

				if err := notifyDeviceTransfer(c, tx, &oldDevice); err != nil {
					return err
				}
			}
		} else if !util.DbIsNotFoundErr(oldDeviceErr) {
			log.Errorf(c, "failed to check existing device: %v", oldDeviceErr)
			return oldDeviceErr
		}

		// 第2步：删除旧设备记录
		if err := tx.Where("udid = ?", p.UDID).Delete(&Device{}).Error; err != nil {
			log.Errorf(c, "failed to delete existing device with udid %s in transaction: %v", p.UDID, err)
			return err
		}

		// 获取用户信息以获取最大设备数限制
		var user User
		if err := tx.First(&user, userID).Error; err != nil {
			log.Errorf(c, "failed to get user %d: %v", userID, err)
			return err
		}

		if isUserBlocked(&user) {
			log.Warnf(c, "login rejected: user %d is blocked", userID)
			return e(ErrorForbidden, "account blocked")
		}

		// 追踪是否需要保存用户信息
		needSave := false

		// 如果提供了语言偏好，更新用户语言设置
		if p.Language != "" {
			acceptLanguage := c.GetHeader("Accept-Language")
			detectedLanguage := detectUserLanguage(c, p.Language, p.Email, acceptLanguage)
			if detectedLanguage != user.Language {
				user.Language = detectedLanguage
				needSave = true
				log.Infof(c, "will update user %d language to: %s", userID, detectedLanguage)
			}
		}

		// 处理邀请码逻辑（仅未激活用户可以设置邀请码）
		if p.InviteCode != "" {
			if user.IsActivated == nil || !*user.IsActivated {
				log.Infof(c, "user %d is not activated, processing invite code: %s", userID, p.InviteCode)
				inviteCodeID := InviteCodeID(p.InviteCode)
				var inviteCode InviteCode
				if err := tx.First(&inviteCode, inviteCodeID).Error; err != nil {
					if util.DbIsNotFoundErr(err) {
						log.Warnf(c, "invalid invite code %s for user %d", p.InviteCode, userID)
						return e(ErrorInvalidInviteCode, "invalid invite code")
					}
					log.Errorf(c, "failed to check invite code %s: %v", p.InviteCode, err)
					return err
				}

				// 检查自邀请
				if inviteCode.UserID == userID {
					log.Warnf(c, "self-invitation detected for user %d with code %s", userID, p.InviteCode)
					return e(ErrorSelfInvitation, "cannot use your own invite code")
				}

				// 设置邀请码
				user.InvitedByCodeID = inviteCodeID
				needSave = true
				log.Infof(c, "will set invite code %s for user %d", p.InviteCode, userID)

				// 异步处理邀请奖励
				go handleInviteDownloadReward(c, userID)
			} else {
				log.Infof(c, "user %d is already activated, ignoring invite code", userID)
			}
		}

		// 设置用户为已激活状态
		if user.IsActivated == nil || !*user.IsActivated {
			user.IsActivated = BoolPtr(true)
			user.ActivatedAt = time.Now().Unix()
			needSave = true
			log.Infof(c, "will set user %d as activated at %d", userID, user.ActivatedAt)
		}

		// 统一保存所有用户信息修改
		if needSave {
			if err := tx.Save(&user).Error; err != nil {
				log.Errorf(c, "failed to save user %d updates: %v", userID, err)
				return err
			}
			log.Infof(c, "successfully saved user %d updates (language: %s, activated: %v, invite code ID: %d)",
				userID, user.Language, user.IsActivated, user.InvitedByCodeID)
		}

		// Check device limit by type (app vs router)
		if err := checkDeviceLimitOrKick(c, tx, &user, isGatewayRequest(c)); err != nil {
			return err
		}

		var tokenIssueTime time.Time
		authResult, tokenIssueTime, err = generateTokens(c, userID, p.UDID, user.Roles)
		if err != nil {
			log.Errorf(c, "failed to generate tokens during login for user %d: %v", userID, err)
			return err
		}

		device = Device{
			UDID:            p.UDID,
			Remark:          p.Remark,
			UserID:          userID,
			TokenIssueAt:    tokenIssueTime.Unix(),
			TokenLastUsedAt: time.Now().Unix(),
		}
		createDeviceWithAppInfo(c, &device)
		if err := tx.Create(&device).Error; err != nil {
			log.Errorf(c, "failed to create new device in transaction for user %d: %v", userID, err)
			return err
		}
		log.Debugf(c, "created new device %s for user %d", device.UDID, device.UserID)

		meta := NewDeviceLoginMeta{
			LoginTime: time.Now().Format("2006-01-02 15:04:05"),
			Remark:    device.Remark,
		}
		if err := emailToUser(c, int64(userID), brandedNewDeviceLoginTemplate, meta); err != nil {
			log.Errorf(c, "failed to send new device login email to user %d: %v", userID, err)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}
	return authResult, nil
}

// webLoginParams 网页登录收尾的入参（验证码登录与第三方登录共用）。
// Email 用于语言探测与响应；InviteCode 为空则跳过邀请码逻辑。
type webLoginParams struct {
	Language, Email, InviteCode string
}

// completeWebLogin 身份已确认为 userID 后的网页登录收尾：
// 封禁检查 → 语言/邀请码/激活 → generateWebCookieToken → setAuthCookies → 漏斗 sid 关联 → 网页登录邮件。
// 返回的错误可能是 e(...) 富错误，调用方用 ErrorE 输出。
func completeWebLogin(c *gin.Context, userID uint64, p webLoginParams) (*DataWebLoginResponse, error) {
	var userIsAdmin bool     // 用于响应中返回用户信息
	var userRoles uint64     // 用于 JWT 中的角色
	var userHasPassword bool // 用于响应中告知前端 /account/security 应显示「修改」还是「设置」

	// 使用事务处理用户信息更新和邀请码设置
	err := db.Get().Transaction(func(tx *gorm.DB) error {
		// 获取用户信息
		var user User
		if err := tx.First(&user, userID).Error; err != nil {
			log.Errorf(c, "failed to get user %d: %v", userID, err)
			return err
		}

		if isUserBlocked(&user) {
			log.Warnf(c, "web login rejected: user %d is blocked", userID)
			return e(ErrorForbidden, "account blocked")
		}
		// 保存用户信息用于响应和 JWT
		userIsAdmin = user.IsAdmin != nil && *user.IsAdmin
		userRoles = user.Roles
		userHasPassword = HasPasswordSet(&user)

		// 追踪是否需要保存用户信息
		needSave := false

		// 如果提供了语言偏好，更新用户语言设置
		if p.Language != "" {
			acceptLanguage := c.GetHeader("Accept-Language")
			detectedLanguage := detectUserLanguage(c, p.Language, p.Email, acceptLanguage)
			if detectedLanguage != user.Language {
				user.Language = detectedLanguage
				needSave = true
				log.Infof(c, "will update user %d language to: %s", userID, detectedLanguage)
			}
		}

		// 处理邀请码逻辑（仅未激活用户可以设置邀请码）
		if p.InviteCode != "" {
			if user.IsActivated == nil || !*user.IsActivated {
				log.Infof(c, "user %d is not activated, processing invite code: %s", userID, p.InviteCode)
				inviteCodeID := InviteCodeID(p.InviteCode)
				var inviteCode InviteCode
				if err := tx.First(&inviteCode, inviteCodeID).Error; err != nil {
					if util.DbIsNotFoundErr(err) {
						log.Warnf(c, "invalid invite code %s for user %d", p.InviteCode, userID)
						return e(ErrorInvalidInviteCode, "invalid invite code")
					}
					log.Errorf(c, "failed to check invite code %s: %v", p.InviteCode, err)
					return err
				}

				// 检查自邀请
				if inviteCode.UserID == userID {
					log.Warnf(c, "self-invitation detected for user %d with code %s", userID, p.InviteCode)
					return e(ErrorSelfInvitation, "cannot use your own invite code")
				}

				// 设置邀请码
				user.InvitedByCodeID = inviteCodeID
				needSave = true
				log.Infof(c, "will set invite code %s for user %d", p.InviteCode, userID)

				// 异步处理邀请奖励
				go handleInviteDownloadReward(c, userID)
			} else {
				log.Infof(c, "user %d is already activated, ignoring invite code", userID)
			}
		}

		// 如果未激活用户没有提供邀请码，也需要激活账号
		if user.IsActivated == nil || !*user.IsActivated {
			user.IsActivated = BoolPtr(true)
			user.ActivatedAt = time.Now().Unix()
			needSave = true
			log.Infof(c, "will activate user %d (web login without invite code) at %d", userID, user.ActivatedAt)
		}

		// 保存用户信息
		if needSave {
			if err := tx.Save(&user).Error; err != nil {
				log.Errorf(c, "failed to save user %d: %v", userID, err)
				return err
			}
			log.Infof(c, "successfully saved user %d updates", userID)
		}

		return nil
	})
	if err != nil {
		log.Errorf(c, "failed to update user info for web login: %v", err)
		return nil, err
	}

	// 生成 Web Cookie 专用 token（2个月有效期，无 refresh token）
	authResult, _, err := generateWebCookieToken(c, userID, userRoles)
	if err != nil {
		log.Errorf(c, "failed to generate web cookie token for user %d: %v", userID, err)
		return nil, e(ErrorSystemError, "failed to generate tokens")
	}

	// 设置 HttpOnly Cookie（Web 端安全认证，sliding expiration）
	setAuthCookies(c, authResult)
	// 漏斗：把匿名 sid 关联到该用户（只读不种；非阻塞，失败不影响登录）。
	if sid, optedOut := readFunnelSid(c); !optedOut && !funnelGPC(c) && sid != "" {
		linkFunnelIdentity(c, "sid", sid, userID, ReqBrand(c))
	}

	// 发送 Web 登录邮件通知
	meta := WebLoginMeta{
		LoginTime: time.Now().Format("2006-01-02 15:04:05"),
		ClientIP:  c.ClientIP(),
	}
	if err := emailToUser(c, int64(userID), brandedWebLoginTemplate, meta); err != nil {
		log.Errorf(c, "failed to send web login email to user %d: %v", userID, err)
		// 不影响登录流程，仅记录错误
	}

	log.Infof(c, "user %d successfully logged in via web", userID)

	return &DataWebLoginResponse{
		User: DataWebLoginUser{
			ID:          userID,
			Email:       p.Email,
			IsAdmin:     userIsAdmin,
			Roles:       userRoles,
			HasPassword: userHasPassword,
		},
		AccessToken: authResult.AccessToken,
	}, nil
}

// notifyDeviceTransfer 给被转走设备的原所有者发设备转移邮件（邮件失败不阻止登录）。
// 原所有者零身份（设备账号）= 没有邮箱，显式跳过，不记错误（spec §4.4）。
// 返回的错误只来自身份查询（DB 故障），调用方应中止事务。
func notifyDeviceTransfer(c *gin.Context, tx *gorm.DB, oldDevice *Device) error {
	has, err := userHasIdentity(tx, oldDevice.UserID)
	if err != nil {
		log.Errorf(c, "failed to check login identity for user %d: %v", oldDevice.UserID, err)
		return err
	}
	if !has {
		log.Infof(c, "device transfer from zero-identity user %d: no mail address, skip transfer email", oldDevice.UserID)
		return nil
	}
	transferMeta := DeviceTransferMeta{
		TransferTime: time.Now().Format("2006-01-02 15:04:05"),
		DeviceRemark: oldDevice.Remark,
	}
	// 收件人是设备原所有者（oldDevice.UserID），与本次登录用户不同——
	// emailToUser 按收件人自己的 User 行解析品牌，不是当前请求品牌。
	if err := emailToUser(c, int64(oldDevice.UserID), brandedDeviceTransferTemplate, transferMeta); err != nil {
		log.Errorf(c, "failed to send device transfer email to user %d: %v", oldDevice.UserID, err)
	}
	return nil
}

package center

import (
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/slack"
	"github.com/wordgate/qtoolkit/util"
)

// api_get_user_info 获取用户信息
//
func api_get_user_info(c *gin.Context) {
	userID := ReqUserID(c)
	log.Infof(c, "user %d requesting user info", userID)

	deviceID := ReqUDID(c)

	var user User
	var device *Device

	// 判断是设备认证还是Web认证
	if deviceID != "" {
		// 设备认证模式：通过设备查找用户信息
		log.Debugf(c, "device auth mode for user %d, device %s", userID, deviceID)
		var deviceRecord Device
		err := db.Get().Preload("User.InvitedByCode").
			Preload("User.LoginIdentifies").
			Preload("User.Devices").
			Where("udid = ?", deviceID).First(&deviceRecord).Error
		if err != nil {
			log.Errorf(c, "failed to get device %s for user %d: %v", deviceID, userID, err)
			Error(c, ErrorSystemError, "failed to get device")
			return
		}
		user = *deviceRecord.User
		device = &deviceRecord
	} else {
		// Web认证模式：直接通过用户ID查找用户信息
		log.Debugf(c, "web auth mode for user %d", userID)
		err := db.Get().Preload("InvitedByCode").
			Preload("LoginIdentifies").
			Preload("Devices").
			Where(&User{ID: userID}).First(&user).Error
		if err != nil {
			log.Errorf(c, "failed to get user %d: %v", userID, err)
			Error(c, ErrorSystemError, "failed to get user")
			return
		}
		device = nil // Web认证模式下没有设备信息
	}

	// 解密登录身份
	for i := range user.LoginIdentifies {
		value, _ := secretDecryptString(c, user.LoginIdentifies[i].EncryptedValue)
		user.LoginIdentifies[i].IndexID = value // 临时存储解密后的值用于buildDataUser
	}

	// 构建设备信息（Web认证模式下为nil）
	var deviceData *DataDevice
	if device != nil {
		deviceData = &DataDevice{
			UDID:            device.UDID,
			Remark:          device.Remark,
			TokenLastUsedAt: device.TokenLastUsedAt,
		}
	}

	dataUser := buildDataUserWithDevice(&user, deviceData)
	Success(c, dataUser)
}

// UpdateLoginEmailRequest 更新登录邮箱请求数据结构
//
type UpdateLoginEmailRequest struct {
	Email            string `json:"email" binding:"required,email" example:"newemail@example.com"` // 新邮箱地址
	VerificationCode string `json:"verificationCode" binding:"required" example:"123456"`          // 验证码
}

// api_update_login_email 更新登录邮箱
//
func api_update_login_email(c *gin.Context) {
	var req UpdateLoginEmailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnf(c, "invalid request for update login email: %v", err)
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}
	userID := ReqUserID(c)
	req.Email = strings.ToLower(req.Email)
	log.Infof(c, "user %d requesting to update login email to %s", userID, req.Email)

	indexID := secretHashIt(c, []byte(req.Email))

	// 校验验证码
	if !verifyEmailCode(c, indexID, req.VerificationCode) {
		log.Warnf(c, "invalid verification code for user %d, email %s", userID, req.Email)
		Error(c, ErrorInvalidArgument, "invalid verification code")
		return
	}
	if err := deleteVerificationCode(c, req.Email); err != nil {
		log.Errorf(c, "failed to delete verification code for user %d, email %s: %v", userID, req.Email, err)
		Error(c, ErrorSystemError, "failed to delete verification code")
		return
	}
	// 获取当前用户
	if userID == 0 {
		log.Warnf(c, "attempt to update login email with user ID 0")
		Error(c, ErrorNotLogin, "unauthorized")
		return
	}

	// 检查邮箱是否已被其他用户绑定
	var exist LoginIdentify
	err := db.Get().Where(&LoginIdentify{
		Type:    "email",
		IndexID: indexID,
	}).First(&exist).Error

	if err == nil {
		// Email exists, check if it belongs to another user
		if exist.UserID != userID {
			log.Warnf(c, "user %d attempted to bind email %s which is already in use by user %d", userID, req.Email, exist.UserID)
			Error(c, ErrorInvalidArgument, "email already in use")
			return
		}
	} else if !util.DbIsNotFoundErr(err) {
		// Database error (not "not found")
		log.Errorf(c, "failed to check email uniqueness for user %d: %v", userID, err)
		Error(c, ErrorSystemError, "failed to check email")
		return
	}
	encEmail, _ := secretEncryptString(c, req.Email)
	// 更新/插入 LoginIdentify
	var identify LoginIdentify
	err = db.Get().Where("user_id = ? AND type = ?", userID, "email").First(&identify).Error
	if err == nil {
		log.Infof(c, "updating existing login identify for user %d", userID)
		identify.IndexID = indexID
		identify.EncryptedValue = encEmail
		if err := db.Get().Save(&identify).Error; err != nil {
			log.Errorf(c, "failed to update email for user %d: %v", userID, err)
			Error(c, ErrorSystemError, "failed to update email")
			return
		}
	} else if util.DbIsNotFoundErr(err) {
		log.Infof(c, "creating new login identify for user %d", userID)
		identify = LoginIdentify{
			UserID:         userID,
			Type:           "email",
			IndexID:        indexID,
			EncryptedValue: encEmail,
		}
		if err := db.Get().Create(&identify).Error; err != nil {
			log.Errorf(c, "failed to bind email for user %d: %v", userID, err)
			Error(c, ErrorSystemError, "failed to bind email")
			return
		}
	} else {
		log.Errorf(c, "failed to find login identify for user %d: %v", userID, err)
		Error(c, ErrorSystemError, "failed to update email")
		return
	}
	log.Infof(c, "successfully updated login email for user %d to %s", userID, req.Email)
	SuccessEmpty(c)
}

// SendVerificationEmailRequest 发送验证码邮箱请求数据结构
//
type SendVerificationEmailRequest struct {
	Email string `json:"email" binding:"required,email" example:"user@example.com"` // 邮箱地址
}

// api_send_bind_email_verification 发送绑定邮箱验证码
//
func api_send_bind_email_verification(c *gin.Context) {
	deviceID := ReqUDID(c)
	userID := ReqUserID(c)
	user := ReqUser(c)
	var req SendVerificationEmailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnf(c, "invalid request for send verification email by user %d: %v", userID, err)
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}
	req.Email = strings.ToLower(req.Email)
	log.Infof(c, "user %d requesting to send verification email to %s", userID, req.Email)

	indexID := secretHashIt(c, []byte(req.Email))

	// 检查邮箱是否已被其他用户绑定
	var exist LoginIdentify
	err := db.Get().Where(&LoginIdentify{
		Type:    "email",
		IndexID: indexID,
	}).First(&exist).Error

	if err == nil {
		// Email exists, check if it belongs to another user
		if exist.UserID != userID {
			log.Warnf(c, "user %d attempted to bind email %s which is already in use by user %d", userID, req.Email, exist.UserID)
			Error(c, ErrorInvalidArgument, "email already in use")
			return
		}
	} else if !util.DbIsNotFoundErr(err) {
		// Database error (not "not found")
		log.Errorf(c, "failed to check email uniqueness for user %d: %v", userID, err)
		Error(c, ErrorSystemError, "failed to check email")
		return
	}

	// 生成验证码
	code := generateVerificationCode(c, deviceID)
	expireMinutes := 5
	// 发送验证码邮件
	meta := VerificationCodeMeta{
		UserEmail:     req.Email,
		Code:          code,
		ExpireMinutes: expireMinutes,
	}
	if err := emailTo(c, req.Email, verificationCodeTemplate, meta); err != nil {
		log.Errorf(c, "failed to send verification email to %s for user %d: %v", req.Email, userID, err)
		Error(c, ErrorSystemError, err.Error())
		return
	}
	// 保存验证码
	if err := saveEmailVerificationCode(c, indexID, code, expireMinutes); err != nil {
		log.Errorf(c, "failed to save verification code for email %s, user %d: %v", indexID, userID, err)
		Error(c, ErrorSystemError, "failed to save verification code")
		return
	}
	if user.IsAdmin != nil && *user.IsAdmin {
		err := slack.Send("verify", fmt.Sprintf("管理员 %s 修改邮箱验证码: %s", req.Email, code))
		if err != nil {
			log.Errorf(c, "failed to send slack alert for admin %s login verification code: %s: %v", hideEmail(req.Email), code, err)
		} else {
			log.Debugf(c, "successfully sent slack alert for admin %s login verification code: %s", hideEmail(req.Email), code)
		}
	}
	log.Infof(c, "successfully sent verification email to %s for user %d", req.Email, userID)
	SuccessEmpty(c)
}

// api_delete_user_account 删除用户账号（软删除）
//
func api_delete_user_account(c *gin.Context) {
	userID := ReqUserID(c)
	log.Infof(c, "user %d requesting to delete their account", userID)

	// 获取当前用户
	var user User
	if err := db.Get().Where(&User{ID: userID}).First(&user).Error; err != nil {
		log.Errorf(c, "failed to get user %d for deletion: %v", userID, err)
		Error(c, ErrorSystemError, "failed to get user")
		return
	}

	// 执行软删除
	if err := db.Get().Delete(&user).Error; err != nil {
		log.Errorf(c, "failed to delete user %d: %v", userID, err)
		Error(c, ErrorSystemError, "failed to delete account")
		return
	}

	log.Infof(c, "successfully deleted user account %d", userID)
	SuccessEmpty(c)
}

// api_get_access_key 获取当前用户的AccessKey
//
func api_get_access_key(c *gin.Context) {
	userID := ReqUserID(c)
	log.Infof(c, "user %d requesting access key", userID)

	var user User
	if err := db.Get().Where(&User{ID: userID}).First(&user).Error; err != nil {
		log.Errorf(c, "failed to get user %d: %v", userID, err)
		Error(c, ErrorSystemError, "failed to get user")
		return
	}

	// 如果用户还没有AccessKey，生成一个
	if user.AccessKey == "" {
		user.AccessKey = generateAccessKey()
		if err := db.Get().Model(&user).Update("access_key", user.AccessKey).Error; err != nil {
			log.Errorf(c, "failed to update access key for user %d: %v", userID, err)
			Error(c, ErrorSystemError, "failed to generate access key")
			return
		}
	}

	Success(c, &DataAccessKey{
		AccessKey: user.AccessKey,
	})
}

// api_regenerate_access_key 重新生成AccessKey
//
func api_regenerate_access_key(c *gin.Context) {
	userID := ReqUserID(c)
	log.Infof(c, "user %d requesting to regenerate access key", userID)

	newAccessKey := generateAccessKey()

	if err := db.Get().Model(&User{}).Where(&User{ID: userID}).Update("access_key", newAccessKey).Error; err != nil {
		log.Errorf(c, "failed to regenerate access key for user %d: %v", userID, err)
		Error(c, ErrorSystemError, "failed to regenerate access key")
		return
	}

	log.Infof(c, "successfully regenerated access key for user %d", userID)
	Success(c, &DataAccessKey{
		AccessKey: newAccessKey,
	})
}

// api_update_user_language 更新用户语言偏好
//
func api_update_user_language(c *gin.Context) {
	userID := ReqUserID(c)
	log.Infof(c, "user %d requesting to update language preference", userID)

	var req UpdateLanguageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnf(c, "invalid request: %v", err)
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	// 标准化语言代码
	normalizedLang := NormalizeBCP47Language(req.Language)

	// 验证BCP 47语言标签
	if !IsValidBCP47Language(normalizedLang) {
		log.Warnf(c, "invalid BCP 47 language tag: %s", req.Language)
		Error(c, ErrorInvalidArgument, fmt.Sprintf("invalid BCP 47 language tag: %s", req.Language))
		return
	}

	// 更新用户语言偏好（使用标准化后的语言代码）
	if err := db.Get().Model(&User{}).Where(&User{ID: userID}).Update("language", normalizedLang).Error; err != nil {
		log.Errorf(c, "failed to update language for user %d: %v", userID, err)
		Error(c, ErrorSystemError, "failed to update language preference")
		return
	}

	// 重新查询用户信息
	var user User
	if err := db.Get().Preload("InvitedByCode").Preload("LoginIdentifies").Preload("Devices").Where(&User{ID: userID}).First(&user).Error; err != nil {
		if util.DbIsNotFoundErr(err) {
			log.Warnf(c, "user not found: %d", userID)
			Error(c, ErrorNotFound, "user not found")
		} else {
			log.Errorf(c, "failed to query user: %v", err)
			Error(c, ErrorSystemError, "failed to query user")
		}
		return
	}

	// 解密登录身份信息（用于buildDataUserWithDevice）
	for i := range user.LoginIdentifies {
		value, _ := secretDecryptString(c, user.LoginIdentifies[i].EncryptedValue)
		user.LoginIdentifies[i].IndexID = value // 临时存储解密后的值
	}

	log.Infof(c, "successfully updated language preference for user %d to %s", userID, req.Language)
	dataUser := buildDataUserWithDevice(&user, nil) // 语言更新API不涉及设备信息
	Success(c, dataUser)
}

// SetPasswordRequest request body for setting user password
type SetPasswordRequest struct {
	Password        string `json:"password" binding:"required"`
	ConfirmPassword string `json:"confirmPassword" binding:"required"`
}

// api_set_password sets or updates user password
func api_set_password(c *gin.Context) {
	userID := ReqUserID(c)
	log.Infof(c, "user %d request to set password", userID)

	var req SetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnf(c, "invalid set password request: %v", err)
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	// Validate passwords match
	if req.Password != req.ConfirmPassword {
		log.Warnf(c, "passwords do not match for user %d", userID)
		Error(c, ErrorInvalidArgument, "passwords do not match")
		return
	}

	// Validate password strength
	if errKey := ValidatePasswordStrength(req.Password); errKey != "" {
		log.Warnf(c, "password does not meet requirements for user %d: %s", userID, errKey)
		Error(c, ErrorInvalidArgument, errKey)
		return
	}

	// Hash password
	hash, err := UserPasswordHash(req.Password)
	if err != nil {
		log.Errorf(c, "failed to hash password for user %d: %v", userID, err)
		Error(c, ErrorSystemError, "failed to set password")
		return
	}

	// Update user
	var user User
	if err := db.Get().First(&user, userID).Error; err != nil {
		log.Errorf(c, "failed to find user %d: %v", userID, err)
		Error(c, ErrorSystemError, "user not found")
		return
	}

	user.PasswordHash = hash
	user.PasswordFailedAttempts = 0
	user.PasswordLockedUntil = 0

	if err := db.Get().Save(&user).Error; err != nil {
		log.Errorf(c, "failed to save password for user %d: %v", userID, err)
		Error(c, ErrorSystemError, "failed to save password")
		return
	}

	log.Infof(c, "user %d successfully set password", userID)
	SuccessEmpty(c)
}

// buildDataUserWithDevice 构建用户数据响应（带设备信息）
func buildDataUserWithDevice(user *User, device *DataDevice) *DataUser {
	if user == nil {
		return nil
	}

	// 构建登录身份列表
	loginIdentifies := make([]DataLoginIdentify, 0)
	for _, loginIdentify := range user.LoginIdentifies {
		// 注意：这里可能需要解密，但在某些上下文中可能没有gin.Context
		// 简化处理，如果需要解密请在调用处处理
		loginIdentifies = append(loginIdentifies, DataLoginIdentify{
			Type:  loginIdentify.Type,
			Value: loginIdentify.IndexID, // 使用IndexID作为简化值
		})
	}

	// 构建邀请码信息
	var inviteCode *DataInviteCode
	if user.InvitedByCode != nil {
		inviteCode = &DataInviteCode{
			Code:      user.InvitedByCode.GetCode(),
			CreatedAt: user.InvitedByCode.CreatedAt.Unix(),
			Remark:    user.InvitedByCode.Remark,
			Link:      user.InvitedByCode.Link(),
		}
	}

	return &DataUser{
		UUID:             user.UUID,
		ExpiredAt:        user.ExpiredAt,
		IsFirstOrderDone: user.IsFirstOrderDone != nil && *user.IsFirstOrderDone,
		InvitedByCode:    inviteCode,
		LoginIdentifies:  loginIdentifies,
		Device:           device,
		DeviceCount:      int64(len(user.Devices)),
		Language:         user.Language,
		IsRetailer:       user.IsRetailer != nil && *user.IsRetailer,
		Roles:            user.Roles,
	}
}

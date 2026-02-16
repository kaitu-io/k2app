package center

import (
	"context"
	"errors"
	"fmt"
	"strings"

	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"
)

// GetEmailIdentifyByUserID 获取用户的邮箱信息
func GetEmailIdentifyByUserID(ctx context.Context, userID int64) (*LoginIdentify, error) {
	log.Debugf(ctx, "getting email identify for user %d", userID)
	var identify LoginIdentify

	// 查询用户的邮箱标识
	err := db.Get().Where("user_id = ? AND type = ?", userID, "email").First(&identify).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			log.Warnf(ctx, "email identify not found for user %d", userID)
		} else {
			log.Errorf(ctx, "failed to get email identify for user %d: %v", userID, err)
		}
		return nil, fmt.Errorf("failed to get email identify: %v", err)
	}

	log.Debugf(ctx, "successfully got email identify for user %d", userID)
	return &identify, nil
}


// AddUser adds a new user with the given email.
// It will return an error if a user with that email already exists.
func AddUser(ctx context.Context, email string) (*User, error) {
	email = strings.ToLower(email)
	log.Infof(ctx, "attempting to add user with email: %s", hideEmail(email))
	indexID := secretHashIt(ctx, []byte(email))
	encEmail, err := secretEncryptString(ctx, email)
	if err != nil {
		log.Errorf(ctx, "failed to encrypt email %s: %v", hideEmail(email), err)
		return nil, err
	}

	// Check if user already exists
	var existingIdentify LoginIdentify
	err = db.Get().Where("type = ? AND index_id=?", "email", indexID).First(&existingIdentify).Error
	if err == nil {
		log.Warnf(ctx, "user with email %s already exists (id: %d)", hideEmail(email), existingIdentify.UserID)
		return nil, errors.New("user with this email already exists")
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		log.Errorf(ctx, "failed to check for existing user with email %s: %v", hideEmail(email), err)
		return nil, err // Database error
	}

	// User does not exist, create a new one
	var user User
	err = db.Get().Transaction(func(tx *gorm.DB) error {
		user = User{
			UUID:      generateId("user"),
			AccessKey: generateAccessKey(),
		}
		if err := tx.Create(&user).Error; err != nil {
			log.Errorf(ctx, "failed to create user record: %v", err)
			return err
		}
		log.Infof(ctx, "created new user with id %d", user.ID)

		identify := LoginIdentify{
			UserID:         user.ID,
			Type:           "email",
			IndexID:        indexID,
			EncryptedValue: encEmail,
		}
		if err := tx.Create(&identify).Error; err != nil {
			log.Errorf(ctx, "failed to create login identify for user %d: %v", user.ID, err)
			return err
		}
		log.Infof(ctx, "created login identify for user %d", user.ID)

		return nil
	})

	if err != nil {
		return nil, err
	}

	log.Infof(ctx, "successfully added user with email %s, new user id: %d", hideEmail(email), user.ID)
	return &user, nil
}

// SetUserAdminStatus finds a user by email and sets their admin status.
func SetUserAdminStatus(ctx context.Context, email string, isAdmin bool) error {
	email = strings.ToLower(email)
	log.Infof(ctx, "setting admin status for user %s to %v", hideEmail(email), isAdmin)
	indexID := secretHashIt(ctx, []byte(email))

	var identify LoginIdentify
	if err := db.Get().Where("type = ? AND index_id = ?", "email", indexID).First(&identify).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			log.Warnf(ctx, "user with email %s not found for setting admin status", hideEmail(email))
			return errors.New("user not found")
		}
		log.Errorf(ctx, "failed to find user by email %s: %v", hideEmail(email), err)
		return err // Database error
	}

	if err := db.Get().Model(&User{}).Where("id = ?", identify.UserID).Update("is_admin", isAdmin).Error; err != nil {
		log.Errorf(ctx, "failed to update admin status for user %d: %v", identify.UserID, err)
		return err
	}

	log.Infof(ctx, "successfully set admin status for user %d to %v", identify.UserID, isAdmin)
	return nil
}

// SetUserRetailerStatus finds a user by email and sets their retailer status, generating AccessKey if not exists.
func SetUserRetailerStatus(ctx context.Context, email string, isRetailer bool) error {
	email = strings.ToLower(email)
	log.Infof(ctx, "setting retailer status for user %s to %v", hideEmail(email), isRetailer)
	indexID := secretHashIt(ctx, []byte(email))

	var identify LoginIdentify
	if err := db.Get().Where("type = ? AND index_id = ?", "email", indexID).First(&identify).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			log.Warnf(ctx, "user with email %s not found for setting retailer status", hideEmail(email))
			return errors.New("user not found")
		}
		log.Errorf(ctx, "failed to find user by email %s: %v", hideEmail(email), err)
		return err // Database error
	}

	// 如果要设置为分销商且当前用户没有AccessKey，则生成一个
	var user User
	if err := db.Get().Where("id = ?", identify.UserID).First(&user).Error; err != nil {
		log.Errorf(ctx, "failed to find user %d: %v", identify.UserID, err)
		return err
	}

	updates := map[string]interface{}{
		"is_retailer": isRetailer,
	}

	// 如果设置为分销商且AccessKey为空，生成新的AccessKey
	if isRetailer && user.AccessKey == "" {
		updates["access_key"] = generateAccessKey()
		log.Infof(ctx, "generated new access key for retailer user %d", identify.UserID)
	}

	if err := db.Get().Model(&User{}).Where("id = ?", identify.UserID).Updates(updates).Error; err != nil {
		log.Errorf(ctx, "failed to update retailer status for user %d: %v", identify.UserID, err)
		return err
	}

	log.Infof(ctx, "successfully set retailer status for user %d to %v", identify.UserID, isRetailer)
	return nil
}


// FindOrCreateUserByEmail 根据邮箱查找或创建用户
// 与 AddUser 不同，如果用户已存在会返回现有用户而不是错误
// 可选参数：requestLang, acceptLanguageHeader 用于新用户的语言检测
func FindOrCreateUserByEmail(c context.Context, email string, langParams ...string) (*User, error) {
	var requestLang, acceptLanguageHeader string
	if len(langParams) > 0 {
		requestLang = langParams[0]
	}
	if len(langParams) > 1 {
		acceptLanguageHeader = langParams[1]
	}
	email = strings.ToLower(email)
	indexID := secretHashIt(c, []byte(email))

	// 首先尝试查找现有用户
	var existingIdentify LoginIdentify
	if err := db.Get().Model(&LoginIdentify{}).
		Where("type = ? AND index_id = ?", "email", indexID).
		Preload("User").
		First(&existingIdentify).Error; err == nil {
		// 用户已存在，直接返回
		return existingIdentify.User, nil
	}

	// 用户不存在，创建新用户
	encEmail, err := secretEncryptString(c, email)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt email: %v", err)
	}

	var newUser User
	err = db.Get().Transaction(func(tx *gorm.DB) error {
		// 检测用户语言偏好
		detectedLanguage := detectUserLanguage(c, requestLang, email, acceptLanguageHeader)

		// 创建用户
		newUser = User{
			UUID:      generateId("user"),
			AccessKey: generateAccessKey(),
			ExpiredAt: 0, // 新用户默认未付费
			Language:  detectedLanguage,
		}
		if err := tx.Create(&newUser).Error; err != nil {
			return fmt.Errorf("failed to create user: %v", err)
		}

		// 创建登录身份
		identify := LoginIdentify{
			UserID:         newUser.ID,
			Type:           "email",
			IndexID:        indexID,
			EncryptedValue: encEmail,
		}
		if err := tx.Create(&identify).Error; err != nil {
			return fmt.Errorf("failed to create login identify: %v", err)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return &newUser, nil
}


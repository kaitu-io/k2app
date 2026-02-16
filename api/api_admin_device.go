package center

import (
	"crypto/md5"
	"encoding/hex"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"
)

// api_admin_get_user_devices 获取用户设备列表
//
func api_admin_get_user_devices(c *gin.Context) {
	userUUID := c.Param("uuid")
	log.Infof(c, "Admin requesting devices for user %s", userUUID)

	// 查询用户
	var user User
	if err := db.Get().Where(&User{UUID: userUUID}).First(&user).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			log.Warnf(c, "User not found: %s", userUUID)
			Error(c, ErrorNotFound, "user not found")
			return
		}
		log.Errorf(c, "Failed to query user: %v", err)
		Error(c, ErrorSystemError, "failed to query user")
		return
	}

	// 查询用户的所有设备
	var devices []Device
	if err := db.Get().Where("user_id = ?", user.ID).Order("created_at DESC").Find(&devices).Error; err != nil {
		log.Errorf(c, "Failed to query devices: %v", err)
		Error(c, ErrorSystemError, "failed to query devices")
		return
	}

	log.Infof(c, "Found %d devices for user %d", len(devices), user.ID)

	// 转换为 AdminDeviceData
	deviceList := make([]AdminDeviceData, 0, len(devices))
	for _, device := range devices {
		deviceList = append(deviceList, AdminDeviceData{
			UDID:            device.UDID,
			Remark:          device.Remark,
			TokenIssueAt:    device.TokenIssueAt,
			TokenLastUsedAt: device.TokenLastUsedAt,
			AppVersion:      device.AppVersion,
			AppPlatform:     device.AppPlatform,
			AppArch:         device.AppArch,
			CreatedAt:       device.CreatedAt.Unix(),
			UpdatedAt:       device.UpdatedAt.Unix(),
		})
	}

	ItemsAll(c, deviceList)
}

// api_admin_issue_test_token 签发设备 token
//
func api_admin_issue_test_token(c *gin.Context) {
	userUUID := c.Param("uuid")
	udid := c.Param("udid")

	adminUser := ReqUser(c)
	log.Warnf(c, "Admin %s requesting to issue token for user %s device %s",
		adminUser.UUID, userUUID, udid)

	// 查询用户
	var user User
	if err := db.Get().Where(&User{UUID: userUUID}).First(&user).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			log.Warnf(c, "User not found: %s", userUUID)
			Error(c, ErrorNotFound, "user not found")
			return
		}
		log.Errorf(c, "Failed to query user: %v", err)
		Error(c, ErrorSystemError, "failed to query user")
		return
	}

	// 查询设备
	var device Device
	if err := db.Get().Where(&Device{UDID: udid, UserID: user.ID}).First(&device).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			log.Warnf(c, "Device not found or not owned by user: %s", udid)
			Error(c, ErrorNotFound, "device not found or not owned by user")
			return
		}
		log.Errorf(c, "Failed to query device: %v", err)
		Error(c, ErrorSystemError, "failed to query device")
		return
	}

	// 生成新的 TokenIssueAt
	now := time.Now()
	tokenIssueAt := now.Unix()

	// 生成 token 和密码（传入用户角色）
	tokenResp, err := generateDeviceTokenWithPassword(c, user.ID, device.UDID, tokenIssueAt, user.Roles)
	if err != nil {
		log.Errorf(c, "Failed to generate device token: %v", err)
		Error(c, ErrorSystemError, "failed to generate device token")
		return
	}

	// 更新设备的 TokenIssueAt 和 PasswordHash
	device.TokenIssueAt = tokenIssueAt
	device.TokenLastUsedAt = now.Unix()
	device.PasswordHash = tokenResp.passwordHash // 内部字段，不返回给前端
	if err := db.Get().Save(&device).Error; err != nil {
		log.Errorf(c, "Failed to update device: %v", err)
		Error(c, ErrorSystemError, "failed to update device")
		return
	}

	log.Infof(c, "Successfully issued token for user %d device %s", user.ID, udid)
	Success(c, &IssueDeviceTokenResponse{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		IssuedAt:     tokenResp.IssuedAt,
		ExpiresIn:    tokenResp.ExpiresIn,
		Password:     tokenResp.Password,
	})
}

// deviceTokenResult 内部结构，包含密码哈希
type deviceTokenResult struct {
	AccessToken  string
	RefreshToken string
	IssuedAt     int64
	ExpiresIn    int64
	Password     string // 密码 = AccessToken 的 MD5
	passwordHash string // 密码哈希（内部使用）
}

// generateDeviceTokenWithPassword 生成设备 token 和密码
// 密码 = AccessToken 的 MD5 (32位)
func generateDeviceTokenWithPassword(ctx *gin.Context, userID uint64, deviceID string, tokenIssueAt int64, roles uint64) (*deviceTokenResult, error) {
	jwtConfig := configJwt(ctx)
	jwtSecret := []byte(jwtConfig.Secret)

	accessExp := jwtConfig.AccessTokenExpiry
	refreshExp := jwtConfig.RefreshTokenExpiry

	now := time.Now()

	// 生成 access token
	accessClaims := TokenClaims{
		UserID:       userID,
		DeviceID:     deviceID,
		Exp:          now.Add(time.Duration(accessExp) * time.Second).Unix(),
		Type:         TokenTypeAccess,
		TokenIssueAt: tokenIssueAt,
		Roles:        roles,
	}
	accessToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims).SignedString(jwtSecret)
	if err != nil {
		return nil, err
	}

	// 生成 refresh token
	refreshClaims := TokenClaims{
		UserID:       userID,
		DeviceID:     deviceID,
		Exp:          now.Add(time.Duration(refreshExp) * time.Second).Unix(),
		Type:         TokenTypeRefresh,
		TokenIssueAt: tokenIssueAt,
		Roles:        roles,
	}
	refreshToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims).SignedString(jwtSecret)
	if err != nil {
		return nil, err
	}

	// 生成密码 = AccessToken 的 MD5
	password := md5Hash(accessToken)

	// 计算密码哈希用于存储
	passwordHash, err := PasswordHash(password)
	if err != nil {
		return nil, err
	}

	return &deviceTokenResult{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		IssuedAt:     tokenIssueAt,
		ExpiresIn:    accessExp,
		Password:     password,
		passwordHash: passwordHash,
	}, nil
}

// md5Hash 计算字符串的 MD5 哈希（32位小写十六进制）
func md5Hash(s string) string {
	h := md5.New()
	h.Write([]byte(s))
	return hex.EncodeToString(h.Sum(nil))
}

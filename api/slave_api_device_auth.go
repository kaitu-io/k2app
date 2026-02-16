package center

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// SlaveDeviceCheckAuthRequest 节点设备认证请求
//
type SlaveDeviceCheckAuthRequest struct {
	UDID  string `json:"udid" binding:"required" example:"device-123"`                            // 设备唯一标识 (必填)
	Token string `json:"token" binding:"required" example:"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9"` // JWT token 或密码（MD5格式）
}

// SlaveDeviceCheckAuthResult 节点设备认证结果
//
type SlaveDeviceCheckAuthResult struct {
	UserID           uint64 `json:"userID" example:"123456"`   // 用户ID
	UDID             string `json:"udid" example:"device-123"` // 设备唯一标识
	ServiceExpiredAt int64  `json:"serviceExpiredAt"`          // 服务过期时间
}

// api_slave_device_check_auth 节点设备认证
//
func api_slave_device_check_auth(c *gin.Context) {
	var req SlaveDeviceCheckAuthRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnf(c, "invalid device check auth request: %v", err)
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	// 判断 token 格式：JWT 格式以 "eyJ" 开头（base64 编码的 {"alg":...}）
	if isJWTToken(req.Token) {
		// JWT token 认证
		handleSlaveJWTAuth(c, req.UDID, req.Token)
	} else {
		// 密码认证（token 作为密码）
		handleSlavePasswordAuth(c, req.UDID, req.Token)
	}
}

// isJWTToken 判断是否为 JWT token 格式
func isJWTToken(token string) bool {
	// JWT 格式：header.payload.signature，以 "eyJ" 开头
	return strings.HasPrefix(token, "eyJ") && strings.Count(token, ".") == 2
}

// handleSlaveJWTAuth 处理 JWT token 认证（k2wss 协议）
// udid 参数必填：必须与 token 中的 UDID 匹配
func handleSlaveJWTAuth(c *gin.Context, udid, token string) {
	// 1. 验证 UDID 必填
	if udid == "" {
		log.Warnf(c, "UDID is required for JWT auth")
		Error(c, ErrorInvalidArgument, "udid is required")
		return
	}

	// 2. 验证 token 有效性（401 如果无效）
	claims, device, err := validateToken(c, token, TokenTypeAccess)
	if err != nil {
		log.Warnf(c, "failed to validate access token: %v", err)
		ErrorE(c, err) // 返回 401
		return
	}

	// 3. 验证 UDID 匹配（必须匹配）
	if device.UDID != udid {
		log.Warnf(c, "UDID mismatch: token=%s, request=%s", device.UDID, udid)
		Error(c, ErrorNotLogin, "UDID mismatch")
		return
	}

	// 4. 获取用户信息
	user := User{}
	err = db.Get().First(&user, device.UserID).Error
	if err != nil {
		log.Errorf(c, "failed to get user %d: %v", device.UserID, err)
		ErrorE(c, err)
		return
	}

	// 5. 检查会员是否过期（402 如果过期）
	if user.IsExpired() {
		log.Warnf(c, "membership expired for user %d (expired at %d)", user.ID, user.ExpiredAt)
		ErrorE(c, ErrMembershipExpired) // 返回 402
		return
	}

	// 返回认证成功结果（UDID 从 token 中获取）
	Success(c, &SlaveDeviceCheckAuthResult{
		UserID:           claims.UserID,
		UDID:             claims.DeviceID,
		ServiceExpiredAt: user.ExpiredAt,
	})
}

// handleSlavePasswordAuth 处理 UDID + Password 认证（k2oc 协议，RADIUS）
func handleSlavePasswordAuth(c *gin.Context, udid, password string) {
	// 1. 根据 UDID 查找设备
	var device Device
	if err := db.Get().Where(&Device{UDID: udid}).First(&device).Error; err != nil {
		log.Warnf(c, "device not found for udid %s: %v", udid, err)
		Error(c, ErrorNotLogin, "invalid credentials")
		return
	}

	// 2. 验证密码
	if device.PasswordHash == "" {
		log.Warnf(c, "device %s has no password set", udid)
		Error(c, ErrorNotLogin, "invalid credentials")
		return
	}
	if !PasswordVerify(password, device.PasswordHash) {
		log.Warnf(c, "invalid password for device %s", udid)
		Error(c, ErrorNotLogin, "invalid credentials")
		return
	}

	// 3. 获取用户信息
	var user User
	if err := db.Get().First(&user, device.UserID).Error; err != nil {
		log.Errorf(c, "failed to get user %d: %v", device.UserID, err)
		ErrorE(c, err)
		return
	}

	// 4. 检查会员是否过期（402 如果过期）
	if user.IsExpired() {
		log.Warnf(c, "membership expired for user %d (expired at %d)", user.ID, user.ExpiredAt)
		ErrorE(c, ErrMembershipExpired) // 返回 402
		return
	}

	// 5. 更新设备最后使用时间
	device.TokenLastUsedAt = time.Now().Unix()
	if err := db.Get().Save(&device).Error; err != nil {
		log.Warnf(c, "failed to update device last used time: %v", err)
		// 不影响认证结果，继续返回成功
	}

	// 返回认证成功结果
	Success(c, &SlaveDeviceCheckAuthResult{
		UserID:           user.ID,
		UDID:             udid,
		ServiceExpiredAt: user.ExpiredAt,
	})
}

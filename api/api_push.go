package center

import (
	"time"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"
)

// ========================= 推送令牌 API =========================

// RegisterPushTokenRequest 注册推送令牌请求
// 注意：device_id 不再需要从请求中传入，从 JWT 中获取
type RegisterPushTokenRequest struct {
	Platform    string `json:"platform" binding:"required,oneof=ios android"`
	Provider    string `json:"provider" binding:"required,oneof=apns jpush fcm"`
	Token       string `json:"token" binding:"required"`
	AppFlavor   string `json:"app_flavor" binding:"required,oneof=china google_play"`
	AppVersion  string `json:"app_version"`
	AppBundle   string `json:"app_bundle"`
	OSVersion   string `json:"os_version"`
	DeviceModel string `json:"device_model"`
	Topic       string `json:"topic"`   // APNs topic (Bundle ID)
	Sandbox     *bool  `json:"sandbox"` // APNs sandbox mode
}

// RegisterPushTokenResponse 注册推送令牌响应
type RegisterPushTokenResponse struct {
	TokenID uint64 `json:"token_id"`
	Status  string `json:"status"` // "created" or "updated"
}

// api_register_push_token 注册或更新设备推送令牌
// POST /api/push/token
// Auth: Required（必须登录，从 JWT 中获取用户和设备信息）
func api_register_push_token(c *gin.Context) {
	ctx := c.Request.Context()

	// 从 JWT 获取用户和设备信息
	userID := ReqUserID(c)
	deviceUDID := ReqUDID(c)

	var req RegisterPushTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnf(ctx, "Invalid push token registration request: %v", err)
		Error(c, ErrorInvalidArgument, "Invalid request body: "+err.Error())
		return
	}

	// 验证 platform/provider 组合
	if !isValidPlatformProviderCombination(req.Platform, req.Provider, req.AppFlavor) {
		log.Warnf(ctx, "Invalid platform/provider combination: platform=%s, provider=%s, flavor=%s",
			req.Platform, req.Provider, req.AppFlavor)
		Error(c, ErrorInvalidArgument, "Invalid provider for platform and flavor combination")
		return
	}

	platform := PushPlatform(req.Platform)
	provider := PushProvider(req.Provider)
	appFlavor := AppFlavor(req.AppFlavor)

	// 查找现有令牌（按 device_udid + provider 查找，确保每设备每通道一个令牌）
	var existingToken PushToken
	err := db.Get().Where("device_udid = ? AND provider = ?", deviceUDID, provider).First(&existingToken).Error

	sandbox := false
	if req.Sandbox != nil {
		sandbox = *req.Sandbox
	}

	if err == gorm.ErrRecordNotFound {
		// 创建新令牌
		newToken := PushToken{
			UserID:      userID,
			DeviceUDID:  deviceUDID,
			Platform:    platform,
			Provider:    provider,
			Token:       req.Token,
			Topic:       req.Topic,
			Sandbox:     &sandbox,
			AppFlavor:   appFlavor,
			AppVersion:  req.AppVersion,
			AppBundle:   req.AppBundle,
			OSVersion:   req.OSVersion,
			DeviceModel: req.DeviceModel,
			Status:      PushTokenStatusActive,
			LastSeenAt:  time.Now().Unix(),
		}

		if err := db.Get().Create(&newToken).Error; err != nil {
			log.Errorf(ctx, "Failed to create push token: %v", err)
			Error(c, ErrorSystemError, "Failed to register push token")
			return
		}

		log.Infof(ctx, "Created new push token: id=%d, user_id=%d, device_udid=%s, provider=%s",
			newToken.ID, userID, deviceUDID, req.Provider)

		Success(c, &RegisterPushTokenResponse{
			TokenID: newToken.ID,
			Status:  "created",
		})
		return
	}

	if err != nil {
		log.Errorf(ctx, "Failed to query push token: %v", err)
		Error(c, ErrorSystemError, "Failed to register push token")
		return
	}

	// 更新现有令牌
	updates := map[string]interface{}{
		"user_id":      userID,
		"token":        req.Token,
		"status":       PushTokenStatusActive,
		"last_seen_at": time.Now().Unix(),
		"app_version":  req.AppVersion,
		"os_version":   req.OSVersion,
		"device_model": req.DeviceModel,
	}

	if req.Topic != "" {
		updates["topic"] = req.Topic
	}
	if req.Sandbox != nil {
		updates["sandbox"] = *req.Sandbox
	}
	if req.AppBundle != "" {
		updates["app_bundle"] = req.AppBundle
	}

	if err := db.Get().Model(&existingToken).Updates(updates).Error; err != nil {
		log.Errorf(ctx, "Failed to update push token: %v", err)
		Error(c, ErrorSystemError, "Failed to update push token")
		return
	}

	log.Infof(ctx, "Updated push token: id=%d, user_id=%d, device_udid=%s, provider=%s",
		existingToken.ID, userID, deviceUDID, req.Provider)

	Success(c, &RegisterPushTokenResponse{
		TokenID: existingToken.ID,
		Status:  "updated",
	})
}

// UnregisterPushTokenRequest 解绑推送令牌请求
// 注意：device_id 不再需要从请求中传入，从 JWT 中获取
type UnregisterPushTokenRequest struct {
	Reason string `json:"reason"` // logout, uninstall, user_disabled, token_invalid
}

// UnregisterPushTokenResponse 解绑推送令牌响应
type UnregisterPushTokenResponse struct {
	Deactivated bool   `json:"deactivated"`
	TokenID     uint64 `json:"token_id,omitempty"`
}

// api_unregister_push_token 解绑设备推送令牌
// DELETE /api/push/token
// Auth: Required（从 JWT 中获取设备信息）
func api_unregister_push_token(c *gin.Context) {
	ctx := c.Request.Context()
	userID := ReqUserID(c)
	deviceUDID := ReqUDID(c)

	var req UnregisterPushTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// 允许空 body
		req.Reason = "logout"
	}

	// 查找用户在该设备上的推送令牌
	var token PushToken
	err := db.Get().Where("device_udid = ? AND user_id = ?", deviceUDID, userID).First(&token).Error

	if err == gorm.ErrRecordNotFound {
		// 没有找到也返回成功（幂等）
		log.Infof(ctx, "No push token found for device_udid=%s, user_id=%d", deviceUDID, userID)
		Success(c, &UnregisterPushTokenResponse{
			Deactivated: false,
		})
		return
	}

	if err != nil {
		log.Errorf(ctx, "Failed to query push token: %v", err)
		Error(c, ErrorSystemError, "Failed to unregister push token")
		return
	}

	// 标记为不活跃
	token.MarkInactive()
	if err := db.Get().Save(&token).Error; err != nil {
		log.Errorf(ctx, "Failed to deactivate push token: %v", err)
		Error(c, ErrorSystemError, "Failed to unregister push token")
		return
	}

	log.Infof(ctx, "Deactivated push token: id=%d, device_udid=%s, reason=%s",
		token.ID, deviceUDID, req.Reason)

	Success(c, &UnregisterPushTokenResponse{
		Deactivated: true,
		TokenID:     token.ID,
	})
}


// ========================= 辅助函数 =========================

// isValidPlatformProviderCombination 验证 platform/provider/flavor 组合是否有效
func isValidPlatformProviderCombination(platform, provider, flavor string) bool {
	switch platform {
	case "ios":
		// iOS 只能使用 APNs
		return provider == "apns"
	case "android":
		switch flavor {
		case "china":
			// 中国版 Android 使用 JPush
			return provider == "jpush"
		case "google_play":
			// Google Play 版 Android 使用 FCM
			return provider == "fcm"
		}
	}
	return false
}

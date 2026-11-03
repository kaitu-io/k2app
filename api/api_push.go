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
	AppFlavor   string `json:"app_flavor" binding:"required,oneof=china google_play app_store"`
	AppVersion  string `json:"app_version"`
	AppBundle   string `json:"app_bundle"`
	OSVersion   string `json:"os_version"`
	DeviceModel string `json:"device_model"`
	Topic       string `json:"topic"`   // APNs topic (Bundle ID)；可空，服务端按用户品牌填，不符则拒绝
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

	if deviceUDID == "" {
		Error(c, ErrorInvalidArgument, "push token requires a device session")
		return
	}

	// APNs topic = 用户品牌 App 的 bundle id（spec 2026-11-03-ios-push-design §6.1）。
	// 由服务端决定，客户端上报的只用来核对：错品牌的 token 发出去必然 DeviceTokenNotForTopic，
	// 更糟的是可能把一个品牌的推送打到另一个品牌的 App 上。
	if req.Provider == string(PushProviderAPNs) {
		u := ReqUser(c)
		if u == nil {
			Error(c, ErrorNotLogin, "not logged in")
			return
		}
		want := Brand(u.Brand).Config().Social.AppleBundleID
		if want == "" {
			Error(c, ErrorNotSupported, "brand has no iOS app")
			return
		}
		if req.Topic != "" && req.Topic != want {
			log.Warnf(ctx, "push token topic mismatch: user=%d brand=%s topic=%s want=%s", userID, u.Brand, req.Topic, want)
			Error(c, ErrorInvalidArgument, "topic does not match brand")
			return
		}
		req.Topic = want
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
		"app_flavor":   appFlavor,
	}

	if req.Topic != "" {
		updates["topic"] = req.Topic
	}
	if req.Sandbox != nil {
		updates["sandbox"] = *req.Sandbox
	} else if req.Token != existingToken.Token {
		// sandbox 标记是投递时自动识别出来的、跟着 token 走。换了 token（同一台设备
		// 从 dev 构建换装到 TestFlight / App Store，UDID 在钥匙串里不变）就清掉，
		// 否则生产 token 会一直被发到 sandbox → BadDeviceToken → 停用，循环往复。
		updates["sandbox"] = false
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

// ========================= 营销推送同意 =========================

// PushPreferences 推送偏好（按用户，不按设备）
type PushPreferences struct {
	MarketingOptIn bool `json:"marketingOptIn"`
}

// UpdatePushPreferencesRequest 用指针区分「没传」和「传了 false」：缺字段不能被当成撤回同意。
type UpdatePushPreferencesRequest struct {
	MarketingOptIn *bool `json:"marketingOptIn"`
}

// api_get_push_preferences GET /api/push/preferences
func api_get_push_preferences(c *gin.Context) {
	var u User
	if err := db.Get().Select("id", "push_marketing_opt_in").First(&u, ReqUserID(c)).Error; err != nil {
		log.Errorf(c, "load push preferences of user %d: %v", ReqUserID(c), err)
		Error(c, ErrorSystemError, "failed to load push preferences")
		return
	}
	Success(c, &PushPreferences{MarketingOptIn: u.PushMarketingOptIn})
}

// api_update_push_preferences PUT /api/push/preferences
// 营销推送同意（App Store 审核规则 4.5.4）。记录表态时刻，便于事后举证。
func api_update_push_preferences(c *gin.Context) {
	var req UpdatePushPreferencesRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.MarketingOptIn == nil {
		Error(c, ErrorInvalidArgument, "marketingOptIn is required")
		return
	}
	userID := ReqUserID(c)
	if err := db.Get().Model(&User{}).Where("id = ?", userID).Updates(map[string]any{
		"push_marketing_opt_in":    *req.MarketingOptIn,
		"push_marketing_opt_in_at": time.Now().Unix(),
	}).Error; err != nil {
		log.Errorf(c, "update push preferences of user %d: %v", userID, err)
		Error(c, ErrorSystemError, "failed to update push preferences")
		return
	}
	log.Infof(c, "user %d set marketing push opt-in=%v", userID, *req.MarketingOptIn)
	Success(c, &PushPreferences{MarketingOptIn: *req.MarketingOptIn})
}

// deactivateDevicePushTokens 登出时停用该设备上该用户的推送 token。
// 失败只记日志：登出本身不能因为推送表出问题而失败；设备归属 join 是第二道防线。
func deactivateDevicePushTokens(c *gin.Context, udid string, userID uint64) {
	if err := db.Get().Model(&PushToken{}).
		Where("device_udid = ? AND user_id = ?", udid, userID).
		Update("status", PushTokenStatusInactive).Error; err != nil {
		log.Errorf(c, "deactivate push tokens of device %s user %d on logout: %v", udid, userID, err)
	}
}

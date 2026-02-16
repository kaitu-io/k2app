package center

import (
	"context"
	"fmt"

	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// =====================================================================
// 推送服务 - 以用户为最小粒度
// =====================================================================
//
// 设计原则：推送的目的是到达用户，而不是记录日志
// - PushToUser: 向用户的所有活跃设备发送推送
// - 使用 asynq 异步队列确保推送可靠到达
// - 不记录推送日志到数据库，仅保留运行时日志
//
// =====================================================================

// PushNotification 推送通知内容
type PushNotification struct {
	Title    string                 `json:"title"`              // 推送标题
	Body     string                 `json:"body"`               // 推送正文
	Data     map[string]interface{} `json:"data,omitempty"`     // 自定义数据
	Sound    string                 `json:"sound,omitempty"`    // 提示音，默认 "default"
	Badge    *int                   `json:"badge,omitempty"`    // 角标数字（iOS）
	ImageURL string                 `json:"imageUrl,omitempty"` // 图片 URL（富媒体推送）
}

// PushToUser 向用户的所有活跃设备发送推送
// 这是推送的主入口，以用户为最小粒度
func PushToUser(ctx context.Context, userID uint64, notification PushNotification) error {
	// 查找用户的所有活跃推送令牌
	var tokens []PushToken
	if err := db.Get().Where("user_id = ? AND status = ?", userID, PushTokenStatusActive).Find(&tokens).Error; err != nil {
		log.Errorf(ctx, "[PUSH] Failed to query push tokens for user %d: %v", userID, err)
		return fmt.Errorf("failed to query push tokens: %w", err)
	}

	if len(tokens) == 0 {
		log.Infof(ctx, "[PUSH] No active push tokens for user %d, skipping", userID)
		return nil
	}

	// 入队推送任务
	taskID, err := EnqueuePushTask(ctx, userID, notification)
	if err != nil {
		log.Errorf(ctx, "[PUSH] Failed to enqueue push task for user %d: %v", userID, err)
		return fmt.Errorf("failed to enqueue push task: %w", err)
	}

	log.Infof(ctx, "[PUSH] Push task enqueued for user %d, taskId=%s, devices=%d", userID, taskID, len(tokens))
	return nil
}

// PushToUsers 批量向多个用户发送推送
// 每个用户会创建一个独立的推送任务
func PushToUsers(ctx context.Context, userIDs []uint64, notification PushNotification) error {
	if len(userIDs) == 0 {
		return nil
	}

	var enqueueCount int
	for _, userID := range userIDs {
		if err := PushToUser(ctx, userID, notification); err != nil {
			log.Warnf(ctx, "[PUSH] Failed to enqueue push for user %d: %v", userID, err)
			continue
		}
		enqueueCount++
	}

	log.Infof(ctx, "[PUSH] Batch push enqueued: total=%d, enqueued=%d", len(userIDs), enqueueCount)
	return nil
}

// sendPushToToken 发送推送到单个令牌（内部函数）
// 这是实际调用推送提供商 SDK 的地方
func sendPushToToken(ctx context.Context, token *PushToken, notification *PushNotification) error {
	switch token.Provider {
	case PushProviderAPNs:
		return sendAPNsPush(ctx, token, notification)
	case PushProviderFCM:
		return sendFCMPush(ctx, token, notification)
	case PushProviderJPush:
		return sendJPushPush(ctx, token, notification)
	default:
		return fmt.Errorf("unsupported push provider: %s", token.Provider)
	}
}

// sendAPNsPush 发送 APNs 推送
// TODO: 实现 APNs 推送
func sendAPNsPush(ctx context.Context, token *PushToken, notification *PushNotification) error {
	log.Infof(ctx, "[PUSH:APNs] Sending to device=%s, title=%s", token.DeviceUDID, notification.Title)
	// TODO: 集成 APNs SDK
	// 使用 token.Token 作为设备令牌
	// 使用 token.Topic 作为 APNs topic (Bundle ID)
	// 使用 token.Sandbox 判断是否为沙盒环境
	return nil
}

// sendFCMPush 发送 FCM 推送
// TODO: 实现 FCM 推送
func sendFCMPush(ctx context.Context, token *PushToken, notification *PushNotification) error {
	log.Infof(ctx, "[PUSH:FCM] Sending to device=%s, title=%s", token.DeviceUDID, notification.Title)
	// TODO: 集成 FCM SDK
	// 使用 token.Token 作为 FCM 注册令牌
	return nil
}

// sendJPushPush 发送极光推送
// TODO: 实现极光推送
func sendJPushPush(ctx context.Context, token *PushToken, notification *PushNotification) error {
	log.Infof(ctx, "[PUSH:JPush] Sending to device=%s, title=%s", token.DeviceUDID, notification.Title)
	// TODO: 集成极光推送 SDK
	// 使用 token.Token 作为极光推送注册 ID
	return nil
}

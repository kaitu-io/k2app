package center

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// MessageType 消息类型
const (
	MessageTypeDeviceKick   = "device_kick"   // 设备被踢除
	MessageTypeSystemNotice = "system_notice" // 系统通知
)

// MessageStatus 消息状态
const (
	MessageStatusPending = "pending" // 待发送
	MessageStatusSent    = "sent"    // 已发送
	MessageStatusFailed  = "failed"  // 发送失败
)

// CreateEmailMessage 创建邮件消息
func CreateEmailMessage(ctx context.Context, userID uint64, msgType, title, content string, metadata map[string]interface{}) error {
	log.Infof(ctx, "creating email message for user %d, type: %s", userID, msgType)
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		log.Errorf(ctx, "failed to marshal metadata for user %d, msgType %s: %v", userID, msgType, err)
		return err
	}

	message := Message{
		UserID:   userID,
		Type:     msgType,
		Title:    title,
		Content:  content,
		Status:   MessageStatusPending,
		Metadata: string(metadataJSON),
	}

	return db.Get().Create(&message).Error
}

// CreateDeviceKickEmail 创建设备踢除邮件消息
func CreateDeviceKickEmail(ctx context.Context, userID uint64, deviceUDID string) error {
	log.Infof(ctx, "creating device kick email for user %d, device %s", userID, deviceUDID)
	metadata := map[string]interface{}{
		"kicked_device_udid": deviceUDID,
		"kick_reason":        "device_limit",
		"kick_time":          time.Now(),
	}

	return CreateEmailMessage(
		ctx,
		userID,
		MessageTypeDeviceKick,
		"设备已被移除",
		fmt.Sprintf("您的设备 %s 由于长时间未使用已被系统移除。", deviceUDID),
		metadata,
	)
}

// GetPendingEmailMessages 获取待发送的邮件消息
func GetPendingEmailMessages() ([]Message, error) {
	var messages []Message
	err := db.Get().Where("status = ?", MessageStatusPending).Find(&messages).Error
	return messages, err
}

// UpdateEmailMessageStatus 更新邮件消息状态
func UpdateEmailMessageStatus(messageID uint64, status string, sentAt *time.Time) error {
	updates := map[string]interface{}{
		"status": status,
	}
	if sentAt != nil {
		updates["sent_at"] = sentAt
	}
	return db.Get().Model(&Message{}).Where("id = ?", messageID).Updates(updates).Error
}

// GetMessageMetadata 获取消息元数据
func GetMessageMetadata(message *Message) (map[string]interface{}, error) {
	var metadata map[string]interface{}
	if err := json.Unmarshal([]byte(message.Metadata), &metadata); err != nil {
		return nil, err
	}
	return metadata, nil
}

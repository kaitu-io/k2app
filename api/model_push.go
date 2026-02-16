package center

import (
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

// ========================= 推送通知模型 =========================

// PushProvider 推送服务提供商
type PushProvider string

const (
	PushProviderAPNs  PushProvider = "apns"  // Apple Push Notification service
	PushProviderJPush PushProvider = "jpush" // 极光推送（中国 Android）
	PushProviderFCM   PushProvider = "fcm"   // Firebase Cloud Messaging（海外 Android）
)

// PushPlatform 平台类型
type PushPlatform string

const (
	PushPlatformIOS     PushPlatform = "ios"
	PushPlatformAndroid PushPlatform = "android"
)

// AppFlavor 应用版本/渠道
type AppFlavor string

const (
	AppFlavorChina      AppFlavor = "china"       // 中国版（使用 JPush）
	AppFlavorGooglePlay AppFlavor = "google_play" // Google Play 版（使用 FCM）
)

// PushTokenStatus 推送令牌状态
type PushTokenStatus string

const (
	PushTokenStatusActive   PushTokenStatus = "active"   // 活跃
	PushTokenStatusInactive PushTokenStatus = "inactive" // 不活跃（provider 返回失效）
	PushTokenStatusExpired  PushTokenStatus = "expired"  // 过期（长时间未使用）
)

// PushToken 推送令牌模型
// 存储设备的推送令牌信息，支持多通道推送
type PushToken struct {
	ID        uint64         `gorm:"primarykey" json:"id"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	// 用户关联（必须登录才能注册推送）
	UserID uint64 `gorm:"not null;index" json:"userId"`
	User   *User  `gorm:"foreignKey:UserID" json:"user,omitempty"`

	// 设备关联（通过 Device.UDID 关联）
	DeviceUDID string  `gorm:"type:varchar(255);not null;index" json:"deviceUdid"` // 关联 Device.UDID
	Device     *Device `gorm:"foreignKey:DeviceUDID;references:UDID" json:"device,omitempty"`

	// 推送通道信息
	Platform PushPlatform `gorm:"type:varchar(20);not null;index" json:"platform"`  // ios / android
	Provider PushProvider `gorm:"type:varchar(20);not null;index" json:"provider"`  // apns / jpush / fcm
	Token    string       `gorm:"type:varchar(512);not null;index" json:"token"`    // 推送令牌（FCM/APNs/JPush 的设备令牌）
	Topic    string       `gorm:"type:varchar(255)" json:"topic,omitempty"`         // APNs 的 topic（Bundle ID）
	Sandbox  *bool        `gorm:"default:false" json:"sandbox,omitempty"`           // APNs 是否为沙盒环境

	// 应用信息
	AppFlavor  AppFlavor `gorm:"type:varchar(20);not null;index" json:"appFlavor"` // china / google_play
	AppVersion string    `gorm:"type:varchar(32)" json:"appVersion,omitempty"`     // 应用版本，如 "1.0.0"
	AppBundle  string    `gorm:"type:varchar(255)" json:"appBundle,omitempty"`     // 应用包名/Bundle ID

	// 设备信息
	OSVersion   string `gorm:"type:varchar(32)" json:"osVersion,omitempty"`   // 操作系统版本
	DeviceModel string `gorm:"type:varchar(64)" json:"deviceModel,omitempty"` // 设备型号

	// 状态与活跃度
	Status     PushTokenStatus `gorm:"type:varchar(20);not null;default:'active';index" json:"status"` // active / inactive / expired
	LastSeenAt int64           `gorm:"not null;index" json:"lastSeenAt"`                               // 最后活跃时间戳（Unix 秒）

	// 元数据（存储 provider 特定信息）
	Metadata string `gorm:"type:json" json:"metadata,omitempty"` // JSON: provider 特定数据
}

// TableName 指定表名
func (PushToken) TableName() string {
	return "push_tokens"
}

// IsActive 检查令牌是否活跃
func (pt *PushToken) IsActive() bool {
	return pt.Status == PushTokenStatusActive
}

// MarkInactive 标记令牌为不活跃
func (pt *PushToken) MarkInactive() {
	pt.Status = PushTokenStatusInactive
}

// MarkActive 标记令牌为活跃
func (pt *PushToken) MarkActive() {
	pt.Status = PushTokenStatusActive
	pt.LastSeenAt = time.Now().Unix()
}

// GetMetadata 获取元数据
func (pt *PushToken) GetMetadata() (map[string]interface{}, error) {
	if pt.Metadata == "" {
		return make(map[string]interface{}), nil
	}
	var meta map[string]interface{}
	err := json.Unmarshal([]byte(pt.Metadata), &meta)
	return meta, err
}

// SetMetadata 设置元数据
func (pt *PushToken) SetMetadata(meta map[string]interface{}) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	pt.Metadata = string(data)
	return nil
}


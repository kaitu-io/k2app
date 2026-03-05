package center

import "time"

// StatAppOpen tracks app launches for DAU/MAU calculation.
// Device identified by SHA256(UDID), no user_id.
type StatAppOpen struct {
	ID         uint64    `gorm:"primarykey"`
	CreatedAt  time.Time // client time (UTC)
	ReportedAt time.Time `gorm:"autoCreateTime"`
	DeviceHash string    `gorm:"type:varchar(64);not null;index:idx_app_open_dau"`
	OS         string    `gorm:"type:varchar(16);not null;index"`
	AppVersion string    `gorm:"type:varchar(32);not null"`
	Locale     string    `gorm:"type:varchar(8)"`
}

// StatConnection tracks VPN connect/disconnect events.
type StatConnection struct {
	ID               uint64    `gorm:"primarykey"`
	CreatedAt        time.Time // client time (UTC)
	ReportedAt       time.Time `gorm:"autoCreateTime"`
	DeviceHash       string    `gorm:"type:varchar(64);not null;index"`
	OS               string    `gorm:"type:varchar(16);not null;index"`
	AppVersion       string    `gorm:"type:varchar(32);not null"`
	Event            string    `gorm:"type:varchar(16);not null;index"`
	NodeType         string    `gorm:"type:varchar(16);not null"`
	NodeIPv4         string    `gorm:"type:varchar(15);index:idx_conn_node"`
	NodeRegion       string    `gorm:"type:varchar(8)"`
	RuleMode         string    `gorm:"type:varchar(8)"`
	DurationSec      int       `gorm:"not null;default:0"`
	DisconnectReason string    `gorm:"type:varchar(32)"`
}

// StatK2sDownload tracks k2s install script downloads.
// IP hashed with daily-rotating salt for unique count without storing raw IPs long-term.
type StatK2sDownload struct {
	ID        uint64    `gorm:"primarykey"`
	CreatedAt time.Time `gorm:"autoCreateTime"`
	IPHash    string    `gorm:"type:varchar(64);not null;index:idx_k2s_dedup"`
	IPRaw     string    `gorm:"type:varchar(45);not null"`
	UA        string    `gorm:"type:varchar(255)"`
}

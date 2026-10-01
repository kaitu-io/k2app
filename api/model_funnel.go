package center

import "time"

// FunnelEvent：行为事件。只追加。
type FunnelEvent struct {
	ID          uint64    `gorm:"primarykey"`
	OccurredAt  time.Time `gorm:"index:idx_brand_event_time,priority:3;index:idx_anon_time,priority:2;index:idx_user_time,priority:2"`
	ReceivedAt  time.Time `gorm:"autoCreateTime;index"`         // 保留期按它删
	Eid         *string   `gorm:"type:varchar(36);uniqueIndex"` // app 面客户端生成；web 面为 NULL
	Brand       string    `gorm:"type:varchar(16);not null;index:idx_brand_event_time,priority:1"`
	Surface     string    `gorm:"type:varchar(8);not null"` // web|app
	Event       string    `gorm:"type:varchar(32);not null;index:idx_brand_event_time,priority:2"`
	AnonID      string    `gorm:"type:varchar(64);index:idx_anon_time,priority:1"` // sid 或 did；GPC 访客为空
	UserID      uint64    `gorm:"index:idx_user_time,priority:1"`                  // 上报当时的登录用户，0 = 未登录
	Plan        string    `gorm:"type:varchar(64)"`
	Source      string    `gorm:"type:varchar(32)"`  // 入口标签（付费墙来源等）
	Channel     string    `gorm:"type:varchar(16)"`  // stripe|apple|wordgate|nextpay
	Path        string    `gorm:"type:varchar(255)"` // web：去掉 locale 前缀的路径
	RefHost     string    `gorm:"type:varchar(128)"`
	UtmSource   string    `gorm:"type:varchar(64)"`
	UtmMedium   string    `gorm:"type:varchar(64)"`
	UtmCampaign string    `gorm:"type:varchar(64)"`
	Country     string    `gorm:"type:varchar(2)"`
	Device      string    `gorm:"type:varchar(16)"` // desktop|mobile|tablet
	OS          string    `gorm:"type:varchar(16)"` // windows|macos|ios|android|linux|other
	AppVersion  string    `gorm:"type:varchar(32)"`
}

func (FunnelEvent) TableName() string { return "funnel_events" }

// FunnelIdentity：匿名身份 ↔ 用户。一个匿名身份可对多个用户（共用设备），反之亦然。
type FunnelIdentity struct {
	ID        uint64 `gorm:"primarykey"`
	CreatedAt time.Time
	Kind      string `gorm:"type:varchar(4);not null;uniqueIndex:uniq_identity,priority:1"` // sid|did
	AnonID    string `gorm:"type:varchar(64);not null;uniqueIndex:uniq_identity,priority:2"`
	UserID    uint64 `gorm:"not null;uniqueIndex:uniq_identity,priority:3;index"`
	Brand     string `gorm:"type:varchar(16);not null"`
}

func (FunnelIdentity) TableName() string { return "funnel_identities" }

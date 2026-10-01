package center

import "time"

// 访客身份与会话数据模型（客服聊天，替换 Chatwoot）。
// 设计见 docs/superpowers/specs/2026-10-01-support-console-chatwoot-replacement-design.md

// Guest：一个访客（未必登录）。合并后被并入的 guest 以 MergedIntoID 指向保留方，行本身不删。
type Guest struct {
	ID           uint64  `gorm:"primarykey"`
	Brand        string  `gorm:"type:varchar(16);not null"`
	MergedIntoID *uint64 `gorm:"index"`
	Locale       string  `gorm:"type:varchar(16)"`
	Country      string  `gorm:"type:varchar(2)"`
	FirstSeenAt  time.Time
	LastSeenAt   time.Time
	CreatedAt    time.Time
}

func (Guest) TableName() string { return "guests" }

// GuestIdentity：guest 持有的身份线索（cid/sid/email）。
// 同一个值可挂在多个 guest 上（共用设备/邮箱），所以唯一键带 guest_id；按值反查走 idx_identity_lookup。
type GuestIdentity struct {
	ID          uint64 `gorm:"primarykey"`
	GuestID     uint64 `gorm:"not null;uniqueIndex:uniq_guest_identity,priority:1"`
	Brand       string `gorm:"type:varchar(16);not null;index:idx_identity_lookup,priority:1"`
	Kind        string `gorm:"type:varchar(8);not null;uniqueIndex:uniq_guest_identity,priority:2;index:idx_identity_lookup,priority:2"` // cid|sid|email
	Value       string `gorm:"type:varchar(255);not null;uniqueIndex:uniq_guest_identity,priority:3;index:idx_identity_lookup,priority:3"`
	Strength    string `gorm:"type:varchar(8);not null"` // verified|claimed
	FirstSeenAt time.Time
	LastSeenAt  time.Time
}

func (GuestIdentity) TableName() string { return "guest_identities" }

// GuestMerge：guest 合并记录，可撤销。
type GuestMerge struct {
	ID                 uint64 `gorm:"primarykey"`
	FromGuestID        uint64 `gorm:"not null;index"`
	IntoGuestID        uint64 `gorm:"not null;index"`
	Reason             string `gorm:"type:varchar(16);not null"` // same_sid|resume_link|manual
	EvidenceIdentityID *uint64
	ActorID            *uint64
	MovedGuestIDs      string `gorm:"type:text"` // JSON 数组：合并时被改指的 guest id，撤销时据此还原
	CreatedAt          time.Time
	UndoneAt           *time.Time
	UndoneBy           *uint64
}

func (GuestMerge) TableName() string { return "guest_merges" }

// GuestUserLink：guest ↔ 登录用户的关联及其证据。
type GuestUserLink struct {
	ID        uint64 `gorm:"primarykey"`
	GuestID   uint64 `gorm:"not null;uniqueIndex:uniq_guest_user_link,priority:1"`
	UserID    uint64 `gorm:"not null;uniqueIndex:uniq_guest_user_link,priority:2;index"`
	Brand     string `gorm:"type:varchar(16);not null"`
	Evidence  string `gorm:"type:varchar(32);not null;uniqueIndex:uniq_guest_user_link,priority:3"`
	CreatedBy *uint64
	CreatedAt time.Time
}

func (GuestUserLink) TableName() string { return "guest_user_links" }

// Conversation：一次客服会话。主体是 guest 或 user（SubjectKind/SubjectID）。
type Conversation struct {
	ID             uint64 `gorm:"primarykey"`
	UUID           string `gorm:"type:varchar(36);not null;uniqueIndex"`
	Brand          string `gorm:"type:varchar(16);not null;index:idx_conv_subject,priority:1"`
	SubjectKind    string `gorm:"type:varchar(8);not null;index:idx_conv_subject,priority:2"` // guest|user
	SubjectID      uint64 `gorm:"not null;index:idx_conv_subject,priority:3"`
	Status         string `gorm:"type:varchar(8);not null;index:idx_conv_subject,priority:4"` // open|closed
	Handler        string `gorm:"type:varchar(8);not null"`                                   // ai|human
	AssigneeID     *uint64
	EntryPath      string `gorm:"type:varchar(255)"`
	TicketID       *uint64
	LastMessageAt  time.Time
	LastMessageBy  string `gorm:"type:varchar(8)"`
	SlackChannelID string `gorm:"type:varchar(32);index"` // 专属频道 id
	SlackCardTS    string `gorm:"type:varchar(32)"`       // 频道内状态卡的消息 ts
	SlackLobbyTS   string `gorm:"type:varchar(32)"`       // 总览频道里那一行的消息 ts
	CreatedAt      time.Time
	ClosedAt       *time.Time
}

func (Conversation) TableName() string { return "conversations" }

// ConversationMessage：会话内消息。ClientID 是客户端幂等键，SlackTS 是 Slack 镜像回写键，均可空；
// MySQL 唯一索引允许多个 NULL，所以无键的消息可共存。
type ConversationMessage struct {
	ID              uint64 `gorm:"primarykey"`
	ConversationID  uint64 `gorm:"not null;index;uniqueIndex:uniq_conv_client,priority:1"`
	SenderType      string `gorm:"type:varchar(8);not null"` // visitor|ai|staff|system
	SenderID        uint64
	SenderName      string  `gorm:"type:varchar(64)"`
	Kind            string  `gorm:"type:varchar(16);not null"` // text|image|options|option_reply|event|note
	Content         string  `gorm:"type:text"`
	Meta            string  `gorm:"type:text"` // JSON
	ClientID        *string `gorm:"type:varchar(36);uniqueIndex:uniq_conv_client,priority:2"`
	SlackTS         *string `gorm:"type:varchar(32);uniqueIndex"`
	SlackMirroredAt *time.Time
	CreatedAt       time.Time
}

func (ConversationMessage) TableName() string { return "conversation_messages" }

const (
	IdentityCID   = "cid"
	IdentitySID   = "sid"
	IdentityEmail = "email"

	StrengthVerified = "verified"
	StrengthClaimed  = "claimed"

	MergeSameSID    = "same_sid"
	MergeResumeLink = "resume_link"
	MergeManual     = "manual"

	SubjectGuest = "guest"
	SubjectUser  = "user"

	ConvOpen   = "open"
	ConvClosed = "closed"

	HandlerAI    = "ai"
	HandlerHuman = "human"

	SenderVisitor = "visitor"
	SenderAI      = "ai"
	SenderStaff   = "staff"
	SenderSystem  = "system"

	MsgText        = "text"
	MsgImage       = "image"
	MsgOptions     = "options"
	MsgOptionReply = "option_reply"
	MsgEvent       = "event"
	MsgNote        = "note"
)

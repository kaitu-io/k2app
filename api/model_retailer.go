package center

import (
	"time"

	"gorm.io/gorm"
)

// ==================== 分销商沟通记录系统 ====================

// RetailerNote 分销商沟通记录模型
type RetailerNote struct {
	ID        uint64         `gorm:"primarykey" json:"id"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deletedAt,omitempty"`

	// 关联分销商用户
	RetailerID uint64 `gorm:"not null;index:idx_retailer" json:"retailerId"` // 分销商用户ID
	Retailer   *User  `gorm:"foreignKey:RetailerID" json:"retailer,omitempty"`

	// 沟通内容
	Content        string    `gorm:"type:text;not null" json:"content"`      // 沟通内容
	CommunicatedAt time.Time `gorm:"not null" json:"communicatedAt"`         // 沟通时间

	// 跟进追踪
	FollowUpAt  *time.Time `gorm:"index:idx_follow_up" json:"followUpAt,omitempty"`          // 跟进时间（null = 无需跟进）
	IsCompleted *bool      `gorm:"default:false;index:idx_follow_up" json:"isCompleted"`     // 是否已完成跟进

	// 操作人信息
	OperatorID uint64 `gorm:"not null;index" json:"operatorId"` // 创建人ID
	Operator   *User  `gorm:"foreignKey:OperatorID" json:"operator,omitempty"`

	// 跟进人（默认为创建人，可以指派给其他管理员）
	AssigneeID *uint64 `gorm:"index:idx_assignee" json:"assigneeId,omitempty"` // 跟进人ID（null = 创建人跟进）
	Assignee   *User   `gorm:"foreignKey:AssigneeID" json:"assignee,omitempty"`

	// Slack 通知状态
	SlackNotified *bool `gorm:"default:false;index:idx_slack_pending" json:"slackNotified"` // 是否已发送 Slack 通知
}

// TableName 指定表名
func (RetailerNote) TableName() string {
	return "retailer_notes"
}

// NeedFollowUp 是否需要跟进
func (rn *RetailerNote) NeedFollowUp() bool {
	return rn.FollowUpAt != nil && (rn.IsCompleted == nil || !*rn.IsCompleted)
}

// IsOverdue 是否已逾期（跟进时间已过但未完成）
func (rn *RetailerNote) IsOverdue() bool {
	if !rn.NeedFollowUp() {
		return false
	}
	return rn.FollowUpAt.Before(time.Now())
}

// DaysOverdue 逾期天数（负数表示还未到期）
func (rn *RetailerNote) DaysOverdue() int {
	if rn.FollowUpAt == nil {
		return 0
	}
	return int(time.Since(*rn.FollowUpAt).Hours() / 24)
}

// ==================== 数据传输对象 ====================

// DataRetailerNote 沟通记录响应数据
type DataRetailerNote struct {
	ID             uint64  `json:"id"`
	RetailerID     uint64  `json:"retailerId"`
	Content        string  `json:"content"`
	CommunicatedAt int64   `json:"communicatedAt"` // Unix 时间戳
	FollowUpAt     *int64  `json:"followUpAt,omitempty"`
	IsCompleted    bool    `json:"isCompleted"`
	OperatorID     uint64  `json:"operatorId"`
	OperatorName   string  `json:"operatorName,omitempty"` // 操作人名称
	AssigneeID     *uint64 `json:"assigneeId,omitempty"`   // 跟进人ID
	AssigneeName   string  `json:"assigneeName,omitempty"` // 跟进人名称
	CreatedAt      int64   `json:"createdAt"`
	IsOverdue      bool    `json:"isOverdue"`
	DaysOverdue    int     `json:"daysOverdue,omitempty"`
}

// ToDataRetailerNote 转换为响应数据
func ToDataRetailerNote(note *RetailerNote) DataRetailerNote {
	data := DataRetailerNote{
		ID:             note.ID,
		RetailerID:     note.RetailerID,
		Content:        note.Content,
		CommunicatedAt: note.CommunicatedAt.Unix(),
		IsCompleted:    note.IsCompleted != nil && *note.IsCompleted,
		OperatorID:     note.OperatorID,
		AssigneeID:     note.AssigneeID,
		CreatedAt:      note.CreatedAt.Unix(),
		IsOverdue:      note.IsOverdue(),
		DaysOverdue:    note.DaysOverdue(),
	}

	if note.FollowUpAt != nil {
		followUpAt := note.FollowUpAt.Unix()
		data.FollowUpAt = &followUpAt
	}

	// OperatorName and AssigneeName need to be populated in API layer by decrypting LoginIdentify

	return data
}

// ==================== 分销待办事项 ====================

// RetailerTodoItem 分销待办事项响应数据
type RetailerTodoItem struct {
	NoteID        uint64  `json:"noteId"`
	RetailerUUID  string  `json:"retailerUuid"`
	RetailerEmail string  `json:"retailerEmail"`
	Level         int     `json:"level"`
	LevelName     string  `json:"levelName"`
	NoteContent   string  `json:"noteContent"`           // 沟通内容预览
	FollowUpAt    int64   `json:"followUpAt"`
	DaysOverdue   int     `json:"daysOverdue"`           // 逾期天数
	AssigneeID    *uint64 `json:"assigneeId,omitempty"`  // 跟进人ID
	AssigneeName  string  `json:"assigneeName,omitempty"` // 跟进人名称
	OperatorID    uint64  `json:"operatorId"`            // 创建人ID
	OperatorName  string  `json:"operatorName,omitempty"` // 创建人名称
}

// ==================== 分销商列表项 ====================

// AdminRetailerListItem 管理后台分销商列表项
type AdminRetailerListItem struct {
	UUID               string        `json:"uuid"`
	Email              string        `json:"email"`
	Level              int           `json:"level"`
	LevelName          string        `json:"levelName"`
	FirstOrderPercent  int           `json:"firstOrderPercent"`
	RenewalPercent     int           `json:"renewalPercent"`
	PaidUserCount      int           `json:"paidUserCount"`
	Contacts           []ContactInfo `json:"contacts,omitempty"`
	Wallet             *DataWallet   `json:"wallet,omitempty"`
	LastCommunicatedAt *int64        `json:"lastCommunicatedAt,omitempty"`
	HasPendingFollowUp bool          `json:"hasPendingFollowUp"`
	PendingFollowUpCnt int           `json:"pendingFollowUpCount"`        // 待跟进数量
	TotalIncome        int           `json:"totalIncome"`                 // 总收入（分）
	TotalWithdrawn     int           `json:"totalWithdrawn"`              // 已提现（分）
	CreatedAt          *int64        `json:"createdAt,omitempty"`         // 注册时间
	Notes              string        `json:"notes,omitempty"`             // 备注
}

// ==================== 分销商详情 ====================

// AdminRetailerDetailData 管理后台分销商详情
type AdminRetailerDetailData struct {
	UUID             string              `json:"uuid"`
	Email            string              `json:"email"`
	UserDetailLink   string              `json:"userDetailLink"` // 用户详情页链接
	RetailerConfig   *DataRetailerConfig `json:"retailerConfig"`
	Wallet           *DataWallet         `json:"wallet,omitempty"`
	PendingFollowUps int                 `json:"pendingFollowUps"` // 待跟进数量
}

package center

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/go-crypt/crypt"
	"github.com/go-crypt/crypt/algorithm/pbkdf2"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/util"
	"golang.org/x/crypto/bcrypt"

	"gorm.io/gorm"
)

var inviteCodeEncoder *util.NumEncoder

func init() {
	// 此处不可再变更
	// 绝对不可变
	const seed = int64(819238123123123)
	var err error
	inviteCodeEncoder, err = util.NewNumEncoder(6, seed, false)
	if err != nil {
		panic(fmt.Sprintf("Failed to initialize invite code encoder: %v", err))
	}
	if inviteCodeEncoder == nil {
		panic("Invite code encoder is nil after initialization")
	}
}

// VipChangeType VIP变更类型
type VipChangeType string

const (
	VipPurchase      VipChangeType = "purchase"       // 购买充值
	VipInviteReward  VipChangeType = "invite_reward"  // 邀请奖励（邀请人获得）
	VipInvitedReward VipChangeType = "invited_reward" // 被邀请奖励（被邀请人获得）
	VipSystemGrant   VipChangeType = "system_grant"   // 系统发放
)

// User 用户模型
type User struct {
	ID               uint64 `gorm:"primarykey"`
	CreatedAt        time.Time
	UpdatedAt        time.Time
	UUID             string           `gorm:"type:varchar(255);uniqueIndex;not null"`
	DeletedAt        gorm.DeletedAt   `gorm:"index"`
	ExpiredAt        int64            `gorm:"not null;index"`           // 过期时间戳（范围查询，索引有效）
	IsFirstOrderDone *bool            `gorm:"default:false"`            // 是否完成首单（bool索引选择性低，通过复合索引优化）
	IsActivated      *bool            `gorm:"default:false"`            // 账号是否已激活（bool索引选择性低，通过复合索引优化）
	ActivatedAt      int64            `gorm:"not null;default:0;index"` // 账号激活时间戳（Unix秒，0表示未激活）
	InvitedByCodeID  uint64           `gorm:"type:bigint;index"`        // 邀请来源码
	InvitedByCode    *InviteCode      `gorm:"foreignKey:InvitedByCodeID"`
	LoginIdentifies  []LoginIdentify  `gorm:"foreignKey:UserID"`
	Devices          []Device         `gorm:"foreignKey:UserID"`
	InviteCodes      []InviteCode     `gorm:"foreignKey:UserID"`
	IsAdmin          *bool            `gorm:"default:false"`
	Roles            uint64           `gorm:"not null;default:1"` // 角色位掩码，默认 RoleUser=1
	Orders           []Order          `gorm:"foreignKey:UserID"`
	ProHistories     []UserProHistory `gorm:"foreignKey:UserID"`

	// 成员管理：付费委托
	DelegateID *uint64 `gorm:"type:bigint;index"`  // 为我付费的用户ID（为空表示自己付费）
	MaxDevice  int     `gorm:"not null;default:5"` // 最大设备数量限制

	// API访问密钥
	AccessKey  string `gorm:"type:varchar(64);index"` // API访问密钥，用于分销商等API调用
	IsRetailer *bool  `gorm:"default:false"`          // 是否为分销商（只有分销商才能使用AccessKey认证和授予订阅）

	// 语言偏好
	Language string `gorm:"type:varchar(10);not null;default:'en-US'" json:"language"` // 用户语言偏好：en-US, zh-CN, ja 等

	// Password authentication
	PasswordHash           string `gorm:"type:varchar(255)"`    // bcrypt hashed password
	PasswordFailedAttempts int    `gorm:"not null;default:0"`   // Failed login attempts counter
	PasswordLockedUntil    int64  `gorm:"not null;default:0"`   // Unix timestamp when lock expires (0 = not locked)
}

// 获取付费人ID的辅助方法
func (u *User) GetPayerID() *uint64 {
	return u.DelegateID
}

// IsExpired 检查用户会员是否已过期
func (u *User) IsExpired() bool {
	return u.ExpiredAt <= time.Now().Unix()
}

// IsVip 检查用户是否是 VIP
// VIP 定义：只要完成过首单购买（is_first_order_done = true）就算 VIP，不管当前是否过期
// 用途：用于续费优惠、VIP 专属活动等场景
func (u *User) IsVip() bool {
	return u.IsFirstOrderDone != nil && *u.IsFirstOrderDone
}

// IsPro 检查用户是否为 Pro 用户（在有效期内）
// Deprecated: 使用 !IsExpired() 代替
func (u *User) IsPro() bool {
	return !u.IsExpired()
}

// GetDelegatedUsers 获取委托给自己付费的用户列表
func (u *User) GetDelegatedUsers() ([]User, error) {
	var users []User
	err := db.Get().Where(&User{DelegateID: &u.ID}).Find(&users).Error
	return users, err
}

// LoginIdentify 登录身份识别模型
type LoginIdentify struct {
	ID             uint64 `gorm:"primarykey"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DeletedAt      gorm.DeletedAt `gorm:"index"`
	UserID         uint64         `gorm:"not null;index"`
	Type           string         `gorm:"type:varchar(50);not null;default:'email';uniqueIndex:idx_type_index_global"`
	IndexID        string         `gorm:"type:varchar(128);not null;uniqueIndex:idx_type_index_global"`
	EncryptedValue string         `gorm:"type:varchar(5000);not null"`
	User           *User          `gorm:"foreignKey:UserID"`
}

// Device 设备模型
type Device struct {
	ID              uint64 `gorm:"primarykey"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
	UDID            string `gorm:"column:udid;type:varchar(255);uniqueIndex;not null"`
	Remark          string `gorm:"type:varchar(255)"`
	UserID          uint64 `gorm:"not null;index"`
	User            *User  `gorm:"foreignKey:UserID"`
	TokenIssueAt    int64  `gorm:"not null"`
	TokenLastUsedAt int64  `gorm:"not null"`

	// 设备密码（用于 k2oc 协议认证）
	PasswordHash string `gorm:"type:varchar(255)"` // 设备密码哈希

	// App 版本信息（从 User-Agent 解析）
	AppVersion  string `gorm:"type:varchar(32)"` // 应用版本，如 "1.0.0"
	AppPlatform string `gorm:"type:varchar(20)"` // 运行平台，如 "darwin", "windows", "linux", "ios", "android"
	AppArch     string `gorm:"type:varchar(20)"` // CPU架构，如 "amd64", "arm64"

	// 设备详细信息（从 User-Agent 解析）
	OSVersion   string `gorm:"type:varchar(32)"` // 系统版本，如 "14.5", "11", "23H2"
	DeviceModel string `gorm:"type:varchar(64)"` // 设备型号，如 "MacBookPro18,1", "iPhone15,2", "Dell XPS 15"
}

func PasswordHash(password string) (string, error) {
	hash, err := pbkdf2.NewSHA256()
	if err != nil {
		return "", err
	}

	digest, err := hash.Hash(password)
	if err != nil {
		return "", err
	}
	return digest.Encode(), nil
}

func PasswordVerify(password string, passwordHashed string) bool {
	ok, err := crypt.CheckPassword(password, passwordHashed)
	return ok && err == nil
}

// UserPasswordHash hashes a user password using bcrypt
func UserPasswordHash(password string) (string, error) {
	// Use bcrypt with default cost (10)
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// UserPasswordVerify verifies a user password against bcrypt hash
func UserPasswordVerify(password string, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// UserProHistory 用户Pro版本变更历史
type UserProHistory struct {
	ID        uint64 `gorm:"primarykey"`
	CreatedAt time.Time
	UpdatedAt time.Time

	// 关联ID（支付ID、邀请码ID等）
	ReferenceID uint64 `gorm:"index;uniqueIndex:idx_user_reference_type"`

	UserID uint64 `gorm:"not null;index;uniqueIndex:idx_user_reference_type"`
	User   *User  `gorm:"foreignKey:UserID"`

	Type VipChangeType `gorm:"type:varchar(20);not null;index;uniqueIndex:idx_user_reference_type"`
	// 变更天数
	Days int `gorm:"not null"`
	// 变更原因
	Reason string `gorm:"type:varchar(255)"`
}

// InviteCode 邀请码模型
type InviteCode struct {
	ID        uint64 `gorm:"primarykey"`
	CreatedAt time.Time
	UpdatedAt time.Time
	Remark    string `gorm:"type:varchar(255)"`
	UserID    uint64 `gorm:"not null;index"` // 添加用户ID关联和索引
	User      *User  `gorm:"foreignKey:UserID"`
}

// GetCode 获取邀请码
func (ic *InviteCode) GetCode() string {
	code, _ := inviteCodeEncoder.Encode(uint64(ic.ID))
	return code
}

// Link 动态生成邀请链接
func (ic *InviteCode) Link() string {
	baseURL := configInviteBaseURL()
	return fmt.Sprintf("%s/%s", baseURL, ic.GetCode())
}

func InviteCodeID(code string) uint64 {
	id := uint64(inviteCodeEncoder.Decode(code))
	return id
}

// Order 支付记录模型
type Order struct {
	ID                   uint64 `gorm:"primarykey"`
	CreatedAt            time.Time
	UpdatedAt            time.Time
	UUID                 string `gorm:"type:varchar(36);uniqueIndex;not null"`
	Title                string `gorm:"type:varchar(255);not null"`
	OriginAmount         uint64 `gorm:"not null"` // 原价（美分）
	CampaignReduceAmount uint64 `gorm:"not null"` // 通过活动码减免的价格（美分）
	PayAmount            uint64 `gorm:"not null"` // 最后需支付的价格（美分）
	UserID               uint64 `gorm:"not null;index"`
	User                 *User  `gorm:"foreignKey:UserID"`
	IsPaid               *bool  `gorm:"default:false"`
	PaidAt               *time.Time
	WordgateOrderNo      string    `gorm:"type:varchar(255);index"`                 // 关联的 wordgate 订单号
	CampaignCode         *string   `gorm:"type:varchar(50);index"`                  // 使用的优惠活动代码
	Campaign             *Campaign `gorm:"foreignKey:CampaignCode;references:Code"` // 通过Code关联Campaign
	Meta                 string    `gorm:"type:json"`                               // 存储 Plan、forUserIds、forMyself 信息
}

// GetPlan 获取订单的计划信息
func (o *Order) GetPlan() (*Plan, error) {
	var meta struct {
		Plan *Plan `json:"plan"`
	}
	if err := json.Unmarshal([]byte(o.Meta), &meta); err != nil {
		return nil, err
	}
	return meta.Plan, nil
}

// SetPlan 设置订单的计划信息
func (o *Order) SetPlan(plan *Plan) error {
	var meta map[string]interface{}
	if o.Meta != "" {
		if err := json.Unmarshal([]byte(o.Meta), &meta); err != nil {
			return err
		}
	} else {
		meta = make(map[string]interface{})
	}
	meta["plan"] = plan
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	o.Meta = string(data)
	return nil
}

// SetCampaign 设置订单的优惠活动信息
func (o *Order) SetCampaign(campaign *Campaign) error {
	var meta map[string]interface{}
	if o.Meta != "" {
		if err := json.Unmarshal([]byte(o.Meta), &meta); err != nil {
			return err
		}
	} else {
		meta = make(map[string]interface{})
	}
	meta["campaign"] = campaign
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	o.Meta = string(data)
	return nil
}

// GetForUsers 从 Meta 中获取为哪些用户购买的 UUID 列表
func (o *Order) GetForUsers() []string {
	var meta struct {
		ForUserUUIDs []string `json:"forUserUUIDs"`
		// 兼容旧数据的字段
		ForUserIds []int64 `json:"forUserIds"`
	}
	if o.Meta == "" {
		return []string{}
	}
	if err := json.Unmarshal([]byte(o.Meta), &meta); err != nil {
		return []string{}
	}
	// 优先返回新的 UUID 格式
	if len(meta.ForUserUUIDs) > 0 {
		return meta.ForUserUUIDs
	}
	// 兼容处理：将旧的 ID 转换为 UUID（如果需要）
	return []string{}
}

// GetForMyself 从 Meta 中获取是否为自己购买
func (o *Order) GetForMyself() bool {
	var meta struct {
		ForMyself bool `json:"forMyself"`
	}
	if o.Meta == "" {
		return false
	}
	if err := json.Unmarshal([]byte(o.Meta), &meta); err != nil {
		return false
	}
	return meta.ForMyself
}

// SetOrderMeta 设置订单的 Meta 信息，包括 Plan、forUserUUIDs、forMyself (Campaign通过CampaignCode单独存储)
func (o *Order) SetOrderMeta(plan *Plan, campaign *Campaign, forUserUUIDs []string, forMyself bool) error {
	meta := struct {
		Plan         *Plan    `json:"plan"`
		ForUserUUIDs []string `json:"forUserUUIDs"`
		ForMyself    bool     `json:"forMyself"`
	}{
		Plan:         plan,
		ForUserUUIDs: forUserUUIDs,
		ForMyself:    forMyself,
	}
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	o.Meta = string(data)
	return nil
}

// Message 消息模型
type Message struct {
	ID        uint64 `gorm:"primarykey"`
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt gorm.DeletedAt `gorm:"index"`

	UserID  uint64     `gorm:"not null;index"`                              // 接收消息的用户
	Type    string     `gorm:"type:varchar(50);not null;index"`             // 消息类型：device_kick, system_notice 等
	Title   string     `gorm:"type:varchar(255);not null"`                  // 消息标题
	Content string     `gorm:"type:text;not null"`                          // 消息内容
	Status  string     `gorm:"type:varchar(20);not null;default:'pending'"` // 消息状态：pending, sent, failed
	SentAt  *time.Time // 发送时间

	// 消息元数据，用于存储额外的信息
	Metadata string `gorm:"type:json"` // 例如：被踢除设备的UDID、踢除原因等

	User User `gorm:"foreignKey:UserID"`
}

// TunnelProtocol tunnel protocol enum
type TunnelProtocol string

const (
	TunnelProtocolK2    TunnelProtocol = "k2"    // K2 protocol (QUIC, legacy v3)
	TunnelProtocolK2V4  TunnelProtocol = "k2v4"  // K2 protocol version 4 (QUIC, recommended)
	TunnelProtocolK2WSS TunnelProtocol = "k2wss" // K2WSS protocol (WebSocket, legacy)
	TunnelProtocolK2OC  TunnelProtocol = "k2oc"  // K2OC protocol (OpenConnect, WayMaker compatible)
)

// SlaveNode 物理节点模型
// Note: Only active nodes exist in the database. When a node goes offline,
// it should be deleted along with its associated tunnels.
type SlaveNode struct {
	ID        uint64 `gorm:"primarykey"`
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt gorm.DeletedAt `gorm:"index"`

	// 物理节点信息
	Ipv4        string `gorm:"uniqueIndex;type:varchar(20);not null"` // 节点IPv4地址（唯一标识）
	SecretToken string `gorm:"type:varchar(64);not null"`             // 节点认证令牌
	Country     string `gorm:"type:varchar(5);not null"`              // 国家代码（ISO 3166-1 alpha-2）
	Region      string `gorm:"type:varchar(50);not null"`             // 服务器机房位置/区域
	Name        string `gorm:"type:varchar(255);not null"`            // 节点名称
	Ipv6        string `gorm:"type:varchar(20);not null"`             // 节点IPv6地址

	// 关联
	Tunnels []SlaveTunnel `gorm:"foreignKey:NodeID"` // 该物理节点上的隧道
}

// SlaveTunnel tunnel model
type SlaveTunnel struct {
	ID        uint64 `gorm:"primarykey"`
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt gorm.DeletedAt `gorm:"index"`

	// Tunnel configuration
	Domain string `gorm:"uniqueIndex;type:varchar(64);not null"` // Tunnel domain (unique index)
	// Deprecated: Tunnel-level auth removed. Field kept for DB schema compatibility.
	// Can be removed with a DB migration in a future version.
	SecretToken string         `gorm:"type:varchar(64);not null"`
	Name        string         `gorm:"type:varchar(255);not null"`            // Tunnel name
	Protocol     TunnelProtocol `gorm:"type:varchar(10);not null;default:'k2v4'"` // Tunnel protocol (k2v4, k2wss, k2oc)
	Port         int64          `gorm:"not null;default:10001"`                 // Tunnel port
	HopPortStart int64          `gorm:"not null;default:0"`                     // Port hopping range start (0 = disabled)
	HopPortEnd   int64          `gorm:"not null;default:0"`                     // Port hopping range end (0 = disabled)

	// Associated physical node
	NodeID uint64     `gorm:"not null;index"`    // Associated physical node ID
	Node   *SlaveNode `gorm:"foreignKey:NodeID"` // Physical node info

	// Test node flag
	IsTest *bool `gorm:"default:false"` // Whether this is a test node (test nodes only visible to admin users)

	// Capability flags
	HasRelay  *bool `gorm:"default:false"` // Whether this tunnel provides relay/forwarding capability
	HasTunnel *bool `gorm:"default:true"`  // Whether this tunnel provides direct tunnel capability
}

// SlaveNodeLoad 节点负载历史记录
type SlaveNodeLoad struct {
	ID        uint64 `gorm:"primarykey"`
	CreatedAt time.Time
	NodeID    uint64     `gorm:"not null;index"`    // 关联的节点ID
	Load      int        `gorm:"not null"`          // 负载值（CPU使用率百分比）
	Node      *SlaveNode `gorm:"foreignKey:NodeID"` // 关联的节点

	// 网络指标（关键指标，用于节点选择）
	NetworkSpeedMbps  float64 `gorm:"not null;default:0"` // 网络峰值速度 (Mbps)
	BandwidthUpMbps   float64 `gorm:"not null;default:0"` // 上行带宽 (Mbps)
	BandwidthDownMbps float64 `gorm:"not null;default:0"` // 下行带宽 (Mbps)
	NetworkLatencyMs  float64 `gorm:"not null;default:0"` // 网络延迟 (毫秒)
	PacketLossPercent float64 `gorm:"not null;default:0"` // 丢包率 (百分比)

	// 系统资源指标（辅助指标）
	MemoryUsagePercent float64 `gorm:"not null;default:0"` // 内存使用率 (百分比)
	DiskUsagePercent   float64 `gorm:"not null;default:0"` // 磁盘使用率 (百分比)
	ConnectionCount    int     `gorm:"not null;default:0"` // 当前连接数

	// 流量统计（累计值）
	TotalBytesReceived uint64 `gorm:"not null;default:0"` // 总接收字节数
	TotalBytesSent     uint64 `gorm:"not null;default:0"` // 总发送字节数

	// 月度流量限制与追踪（用于计费和负载计算）
	BillingCycleEndAt        int64 `gorm:"not null;default:0"` // 计费周期截止时间戳（Unix秒）
	MonthlyTrafficLimitBytes int64 `gorm:"not null;default:0"` // 月度流量限制（字节），0表示无限制
	UsedTrafficBytes         int64 `gorm:"not null;default:0"` // 当前计费周期已使用流量（字节）
}

// GetTrafficUsagePercent 获取流量使用率百分比 (0-100)
func (l *SlaveNodeLoad) GetTrafficUsagePercent() float64 {
	if l.MonthlyTrafficLimitBytes == 0 {
		return 0 // 无限流量
	}
	return float64(l.UsedTrafficBytes) / float64(l.MonthlyTrafficLimitBytes) * 100
}

// IsTrafficLimitExceeded 检查流量是否超限
func (l *SlaveNodeLoad) IsTrafficLimitExceeded() bool {
	if l.MonthlyTrafficLimitBytes == 0 {
		return false // 无限流量永不超限
	}
	return l.UsedTrafficBytes >= l.MonthlyTrafficLimitBytes
}

// IsBillingCycleExpired 检查计费周期是否已过期
func (l *SlaveNodeLoad) IsBillingCycleExpired() bool {
	if l.BillingCycleEndAt == 0 {
		return false
	}
	return time.Now().Unix() > l.BillingCycleEndAt
}

type AppType string

const (
	AppKaituService AppType = "kaitu-service"
	AppKaituUI      AppType = "kaitu-ui"
)

// SessionAcct 会话计费记录
type SessionAcct struct {
	ID           uint64 `gorm:"primarykey"`
	CreatedAt    time.Time
	UserID       uint64  `gorm:"not null;index"`
	Device       *Device `gorm:"foreignKey:DeviceID"`
	DeviceID     uint64  `gorm:"not null;index"`
	SlaveID      uint64  `gorm:"not null;index"`
	SessionID    string  `gorm:"type:varchar(128);not null;uniqueIndex"`
	InputBytes   uint64  `gorm:"not null;default:0"`
	OutputBytes  uint64  `gorm:"not null;default:0"`
	Seconds      int64   `gorm:"not null;default:0"`
	SliceStartAt int64   `gorm:"not null;index"` // 分片开始时间戳（秒）
	SliceEndAt   int64   `gorm:"not null;index"` // 分片结束时间戳（秒）
}

type Secret struct {
	ID        uint64 `gorm:"primarykey"`
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt gorm.DeletedAt `gorm:"index"`
	TheKey    string         `gorm:"type:varchar(255);not null;uniqueIndex"`
	Value     string         `gorm:"not null"`
	ExpiredAt int64          `gorm:"not null"`
}

type Plan struct {
	ID          uint64    `gorm:"primarykey" json:"id"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	PID         string    `gorm:"column:pid;type:varchar(30);not null;uniqueIndex" json:"pid"` // 套餐标识符
	Label       string    `gorm:"type:varchar(255);not null" json:"label"`                     // 套餐名称
	Price       uint64    `gorm:"not null" json:"price"`                                       // 价格（美分）
	OriginPrice uint64    `gorm:"not null" json:"originPrice"`                                 // 原价（美分）
	Month       int       `gorm:"not null" json:"month"`                                       // 月数
	Highlight   *bool     `gorm:"default:false" json:"highlight"`                              // 是否高亮显示
	IsActive    *bool     `gorm:"default:true" json:"isActive"`                                // 是否激活
}

// EmailMarketingTemplate EDM多语言邮件模板模型
type EmailMarketingTemplate struct {
	ID        uint64         `gorm:"primarykey" json:"id"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
	DeletedAt gorm.DeletedAt `gorm:"index"`

	// 基础信息
	Name        string `gorm:"type:varchar(255);not null" json:"name"`    // 模板名称
	Language    string `gorm:"type:varchar(35);not null" json:"language"` // BCP 47 语言标签，如 en-US, zh-CN
	Subject     string `gorm:"type:varchar(500)" json:"subject"`          // 邮件主题
	Content     string `gorm:"type:text" json:"content"`                  // 邮件内容（HTML格式）
	IsActive    *bool  `gorm:"default:true" json:"isActive"`              // 是否启用
	Description string `gorm:"type:text" json:"description"`              // 模板描述

	// 翻译关系链
	OriginID     *uint64                  `gorm:"index" json:"originId"`                             // 源模板ID，null表示这是原始模板
	Origin       *EmailMarketingTemplate  `gorm:"foreignKey:OriginID" json:"origin,omitempty"`       // 源模板引用
	Translations []EmailMarketingTemplate `gorm:"foreignKey:OriginID" json:"translations,omitempty"` // 翻译版本列表
}

// GetLanguagePreference 获取用户的语言偏好
func (u *User) GetLanguagePreference() string {
	// 如果用户已设置语言偏好，直接返回
	if u.Language != "" {
		return u.Language
	}

	// 如果没有设置，尝试从邮箱域名推测语言
	if len(u.LoginIdentifies) > 0 {
		for _, identity := range u.LoginIdentifies {
			if identity.Type == "email" {
				email := identity.IndexID
				// 根据邮箱域名推测语言偏好
				if strings.HasSuffix(email, "@qq.com") || strings.HasSuffix(email, "@163.com") ||
					strings.HasSuffix(email, "@126.com") || strings.HasSuffix(email, "@sina.com") {
					return "zh-CN"
				} else if strings.HasSuffix(email, "@yahoo.co.jp") || strings.HasSuffix(email, "@gmail.jp") {
					return "ja"
				}
				// 更多域名规则可以在这里添加
				break
			}
		}
	}
	return "en-US" // 默认英语
}

// 分销商等级常量
const (
	RetailerLevelReferrer        = 1 // L1 推荐者
	RetailerLevelRetailer        = 2 // L2 分销商
	RetailerLevelPremiumRetailer = 3 // L3 优质分销商
	RetailerLevelPartner         = 4 // L4 合伙人
)

// RetailerLevelInfo 分销商等级配置信息
type RetailerLevelInfo struct {
	Name             string // 等级名称
	FirstOrderPct    int    // 首单分成百分比
	RenewalPct       int    // 续费分成百分比
	RequiredUsers    int    // 升级所需累计付费用户数
	NeedContentProof bool   // 是否需要内容证明
}

// RetailerLevelConfig 等级配置映射
var RetailerLevelConfig = map[int]RetailerLevelInfo{
	RetailerLevelReferrer:        {"推荐者", 20, 0, 0, false},
	RetailerLevelRetailer:        {"分销商", 25, 10, 10, false},
	RetailerLevelPremiumRetailer: {"优质分销商", 30, 20, 30, true},
	RetailerLevelPartner:         {"合伙人", 30, 30, 100, true},
}

// RetailerConfig 分销商配置模型
type RetailerConfig struct {
	ID        uint64    `gorm:"primarykey" json:"id"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`

	UserID uint64 `gorm:"uniqueIndex;not null" json:"userId"` // 分销商用户ID（唯一）
	User   *User  `gorm:"foreignKey:UserID" json:"user,omitempty"`

	// 等级系统
	Level         int `gorm:"not null;default:1" json:"level"`         // 等级：1=L1推荐者, 2=L2分销商, 3=L3优质分销商, 4=L4合伙人
	PaidUserCount int `gorm:"not null;default:0" json:"paidUserCount"` // 累计带来的付费用户数

	// 分成比例（双字段：首单 + 续费）
	FirstOrderPercent int `gorm:"not null;default:20" json:"firstOrderPercent"` // 首单分成百分比 (1-100)
	RenewalPercent    int `gorm:"not null;default:0" json:"renewalPercent"`     // 续费分成百分比 (1-100)

	// 内容证明（L3/L4升级审核用）
	ContentProof      string     `gorm:"type:text" json:"contentProof,omitempty"`        // JSON: 社媒链接、推广内容等
	ContentVerifiedAt *time.Time `json:"contentVerifiedAt,omitempty"`                    // 内容审核通过时间
	ContentVerifiedBy *uint64    `gorm:"type:bigint" json:"contentVerifiedBy,omitempty"` // 审核人ID

	// 联系方式（JSON数组）
	Contacts string `gorm:"type:text" json:"contacts,omitempty"` // JSON: [{type, value, label}]

	// 备注（运营标记）
	Notes string `gorm:"type:text" json:"notes,omitempty"` // 分销商备注

	// 兼容旧字段（保留以便迁移）
	CashbackPercent int    `gorm:"not null;default:10" json:"cashbackPercent"`                          // 旧：返现百分比（已废弃，使用 FirstOrderPercent）
	CashbackRule    string `gorm:"type:varchar(20);not null;default:'first_order'" json:"cashbackRule"` // 旧：返现规则（已废弃）
}

// GetLevelInfo 获取当前等级的配置信息
func (rc *RetailerConfig) GetLevelInfo() RetailerLevelInfo {
	if info, ok := RetailerLevelConfig[rc.Level]; ok {
		return info
	}
	return RetailerLevelConfig[RetailerLevelReferrer] // 默认返回L1
}

// GetNextLevelInfo 获取下一等级的配置信息，如果已是最高级则返回nil
func (rc *RetailerConfig) GetNextLevelInfo() *RetailerLevelInfo {
	nextLevel := rc.Level + 1
	if info, ok := RetailerLevelConfig[nextLevel]; ok {
		return &info
	}
	return nil
}

// CanAutoUpgrade 检查是否满足自动升级条件（仅L1→L2可自动升级）
func (rc *RetailerConfig) CanAutoUpgrade() bool {
	if rc.Level != RetailerLevelReferrer {
		return false
	}
	nextInfo := RetailerLevelConfig[RetailerLevelRetailer]
	return rc.PaidUserCount >= nextInfo.RequiredUsers
}

// RetailerLevelHistory 分销商等级变更历史
type RetailerLevelHistory struct {
	ID        uint64    `gorm:"primarykey" json:"id"`
	CreatedAt time.Time `json:"createdAt"`

	RetailerConfigID uint64  `gorm:"not null;index" json:"retailerConfigId"` // 分销商配置ID
	OldLevel         int     `gorm:"not null" json:"oldLevel"`               // 原等级
	NewLevel         int     `gorm:"not null" json:"newLevel"`               // 新等级
	Reason           string  `gorm:"type:varchar(100)" json:"reason"`        // 变更原因: auto_upgrade, manual_upgrade, manual_downgrade
	AdminID          *uint64 `gorm:"type:bigint" json:"adminId,omitempty"`   // 管理员ID（手动调整时记录）
}

// Campaign 优惠活动模型
type Campaign struct {
	ID        uint64         `gorm:"primarykey" json:"id"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
	DeletedAt gorm.DeletedAt `gorm:"index"`

	Code        string `gorm:"type:varchar(50);uniqueIndex;not null" json:"code"` // 活动代码（如 FIRST_ORDER_20）
	Name        string `gorm:"type:varchar(255);not null" json:"name"`            // 活动名称
	Type        string `gorm:"type:varchar(20);not null" json:"type"`             // discount/coupon
	Value       uint64 `gorm:"not null" json:"value"`                             // 优惠值：discount时为百分比（如80=8折），coupon时为美分
	StartAt     int64  `gorm:"not null" json:"startAt"`                           // 开始时间
	EndAt       int64  `gorm:"not null" json:"endAt"`                             // 结束时间
	Description string `gorm:"type:text" json:"description"`                      // 活动描述
	IsActive    *bool  `gorm:"default:true" json:"isActive"`                      // 是否启用

	// 匹配条件类型（预定义）
	MatcherType string `gorm:"type:varchar(50)" json:"matcherType"` // first_order, vip, all

	// 统计信息
	UsageCount int64 `gorm:"default:0" json:"usageCount"` // 使用次数
	MaxUsage   int64 `gorm:"default:0" json:"maxUsage"`   // 最大使用次数（0=无限制）
}

// TableName 指定表名
func (Campaign) TableName() string {
	return "campaigns"
}

// ========================= EDM 邮件发送日志 =========================

// EmailSendLogStatus 邮件发送状态
type EmailSendLogStatus string

const (
	EmailSendLogStatusPending EmailSendLogStatus = "pending" // 待发送
	EmailSendLogStatusSent    EmailSendLogStatus = "sent"    // 已发送
	EmailSendLogStatusFailed  EmailSendLogStatus = "failed"  // 发送失败
	EmailSendLogStatusSkipped EmailSendLogStatus = "skipped" // 跳过（幂等性检查）
)

// EmailSendLog 邮件发送记录模型 - 用于追踪每封邮件的发送状态
type EmailSendLog struct {
	ID        uint64    `gorm:"primarykey" json:"id"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`

	// 关联信息
	BatchID    string `gorm:"type:varchar(64);not null;index" json:"batchId"` // 批次ID（asynq task ID）
	TemplateID uint64 `gorm:"not null;index" json:"templateId"`               // 邮件模板ID
	UserID     uint64 `gorm:"not null;index" json:"userId"`                   // 目标用户ID

	// 发送信息
	Email    string             `gorm:"type:varchar(255);not null;index" json:"email"` // 目标邮箱
	Language string             `gorm:"type:varchar(35);not null" json:"language"`     // 使用的语言
	Status   EmailSendLogStatus `gorm:"type:varchar(20);not null;index" json:"status"` // 发送状态

	// 发送结果
	SentAt   *time.Time `json:"sentAt,omitempty"`                 // 发送时间
	ErrorMsg *string    `gorm:"type:text" json:"error,omitempty"` // 错误信息（失败时）

	// 幂等性控制：同一批次中，同一用户同一模板只发送一次
	// 复合唯一索引：(batch_id, template_id, user_id)
}

// TableName 指定表名
func (EmailSendLog) TableName() string {
	return "email_send_logs"
}

// IsIdempotencyKeyExists 检查幂等性键是否存在（防止重复发送）- 同一批次内检查
func IsIdempotencyKeyExists(batchID string, templateID, userID uint64) (bool, error) {
	var count int64
	err := db.Get().Model(&EmailSendLog{}).
		Where("batch_id = ? AND template_id = ? AND user_id = ?", batchID, templateID, userID).
		Count(&count).Error
	return count > 0, err
}

// HasSentTemplateToUserRecently 检查是否在指定时间内向用户发送过该模板（跨批次幂等性检查）
// 用于防止同一模板在短时间内重复发送给同一用户（例如24小时内）
func HasSentTemplateToUserRecently(templateID, userID uint64, withinHours int) (bool, error) {
	since := time.Now().Add(-time.Duration(withinHours) * time.Hour)
	var count int64
	err := db.Get().Model(&EmailSendLog{}).
		Where("template_id = ? AND user_id = ? AND status = ? AND sent_at >= ?",
			templateID, userID, EmailSendLogStatusSent, since).
		Count(&count).Error
	return count > 0, err
}

// ECHKeyStatus ECH 密钥状态枚举
type ECHKeyStatus string

const (
	ECHKeyStatusActive      ECHKeyStatus = "active"       // 当前主密钥
	ECHKeyStatusGracePeriod ECHKeyStatus = "grace_period" // 轮换过渡期（仍可解密）
	ECHKeyStatusRetired     ECHKeyStatus = "retired"      // 已退役（不再使用）
)

// ECHKey ECH 密钥对模型
// 存储 X25519 密钥对，用于 ECH (Encrypted Client Hello) 加密/解密
// 基于 RFC 9180 (HPKE) 和 ECH 协议草案实现
type ECHKey struct {
	ID        uint64         `gorm:"primarykey"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
	DeletedAt gorm.DeletedAt `gorm:"index"`

	// Config ID: 1-255 循环分配，0 保留
	// 同一时刻最多 2 个活跃 ID（当前 + 轮换中的旧密钥）
	ConfigID uint8 `gorm:"not null;index" json:"configId"`

	// 密钥材料（AES-256-GCM 加密存储）
	PrivateKey string `gorm:"type:text;not null" json:"-"`          // Base64(AES-GCM(X25519PrivateKey))
	PublicKey  string `gorm:"type:text;not null" json:"publicKey"`  // Base64(AES-GCM(X25519PublicKey))
	ECHConfig  string `gorm:"type:text;not null" json:"echConfig"`  // Base64(AES-GCM(ECHConfig binary))

	// 密钥状态
	Status ECHKeyStatus `gorm:"type:varchar(20);not null;index;default:'active'" json:"status"`

	// 生命周期时间戳
	ActivatedAt int64  `gorm:"not null" json:"activatedAt"`       // 激活时间（Unix timestamp）
	ExpiresAt   int64  `gorm:"not null;index" json:"expiresAt"`   // 过期时间（进入 grace period）
	RetiredAt   *int64 `gorm:"index" json:"retiredAt,omitempty"`  // 完全退役时间

	// 算法参数（便于未来扩展）
	// KEM: 0x0020 = DHKEM(X25519, HKDF-SHA256)
	// KDF: 0x0001 = HKDF-SHA256
	// AEAD: 0x0001 = AES-128-GCM, 0x0003 = ChaCha20Poly1305
	KEMId  uint16 `gorm:"not null;default:32" json:"kemId"`   // 0x0020
	KDFId  uint16 `gorm:"not null;default:1" json:"kdfId"`    // 0x0001
	AEADId uint16 `gorm:"not null;default:1" json:"aeadId"`   // 0x0001
}

// ========================= Strategy System Models =========================

// StrategyRules stores versioned strategy rules configuration
type StrategyRules struct {
	ID        uint64         `gorm:"primarykey"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
	DeletedAt gorm.DeletedAt `gorm:"index"`

	Version  string `gorm:"type:varchar(50);uniqueIndex;not null" json:"version"` // Version format: YYYY.MM.DD.N
	Content  string `gorm:"type:text;not null" json:"content"`                    // JSON: rules, protocols, default
	IsActive *bool  `gorm:"default:true" json:"isActive"`                         // Only one active version at a time
}

// TelemetryEvent stores individual telemetry events
type TelemetryEvent struct {
	ID        uint64    `gorm:"primarykey"`
	CreatedAt time.Time `json:"createdAt"`

	// Event identification
	EventID   string `gorm:"type:varchar(64);uniqueIndex;not null" json:"eventId"` // UUID from client
	Timestamp int64  `gorm:"not null;index" json:"timestamp"`                      // Unix milliseconds
	EventType string `gorm:"type:varchar(20);not null;index" json:"eventType"`     // connection, session, anomaly, feedback

	// Device association
	DeviceID uint64  `gorm:"not null;index" json:"deviceId"`
	Device   *Device `gorm:"foreignKey:DeviceID"`

	// Event data (JSON)
	Context  string `gorm:"type:text" json:"context,omitempty"`  // Context JSON
	Decision string `gorm:"type:text" json:"decision,omitempty"` // Decision JSON
	Outcome  string `gorm:"type:text" json:"outcome,omitempty"`  // Outcome JSON

	// User satisfaction feedback (1-5 stars, null = no feedback)
	Satisfaction *int `gorm:"type:tinyint" json:"satisfaction,omitempty"`

	// Metadata
	AppVersion string `gorm:"type:varchar(32)" json:"appVersion"` // Client app version
}

// TelemetryRateLimit tracks rate limiting for telemetry uploads
type TelemetryRateLimit struct {
	ID        uint64    `gorm:"primarykey"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`

	DeviceID   uint64 `gorm:"uniqueIndex:idx_device_hour;not null" json:"deviceId"`
	HourBucket int64  `gorm:"uniqueIndex:idx_device_hour;not null;index" json:"hourBucket"` // Unix hour (timestamp / 3600)
	EventCount int    `gorm:"not null;default:0" json:"eventCount"`
}

// ========================= Route Diagnosis Models =========================

// RouteDiagnosisDirection represents the direction of route diagnosis
type RouteDiagnosisDirection string

const (
	// DiagnosisDirectionOutbound represents client → slave direction
	DiagnosisDirectionOutbound RouteDiagnosisDirection = "outbound"
	// DiagnosisDirectionInbound represents slave → client direction
	DiagnosisDirectionInbound RouteDiagnosisDirection = "inbound"
)

// IPRouteInfo stores route diagnosis results for an IP address
// Data is completely independent from SlaveNode - stored by IP directly
// Each IP has two records: one for outbound (client→IP) and one for inbound (IP→client)
type IPRouteInfo struct {
	ID        uint64    `gorm:"primarykey"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`

	// IP address being diagnosed (independent, not linked to SlaveNode)
	IP string `gorm:"uniqueIndex:idx_ip_direction;type:varchar(45);not null" json:"ip"`

	// Direction: "outbound" (client→IP via Alibaba probes) or "inbound" (IP→client)
	Direction RouteDiagnosisDirection `gorm:"uniqueIndex:idx_ip_direction;type:varchar(10);not null" json:"direction"`

	// Route matrix JSON: {"china_telecom:guangdong": "cn2_gia", "china_unicom:beijing": "as9929", ...}
	RouteMatrix string `gorm:"type:text;not null" json:"routeMatrix"`

	// Diagnosis metadata
	ProbeCount   int       `gorm:"not null" json:"probeCount"`   // Total number of probes attempted
	SuccessCount int       `gorm:"not null" json:"successCount"` // Number of successful probes
	DiagnosedAt  time.Time `gorm:"not null" json:"diagnosedAt"`  // When diagnosis was performed
}

// GetRouteMap parses RouteMatrix JSON into a map
func (r *IPRouteInfo) GetRouteMap() (map[string]string, error) {
	var routeMap map[string]string
	if r.RouteMatrix == "" {
		return make(map[string]string), nil
	}
	if err := json.Unmarshal([]byte(r.RouteMatrix), &routeMap); err != nil {
		return nil, fmt.Errorf("failed to parse route matrix: %w", err)
	}
	return routeMap, nil
}

// SetRouteMap serializes a map to RouteMatrix JSON
func (r *IPRouteInfo) SetRouteMap(routeMap map[string]string) error {
	data, err := json.Marshal(routeMap)
	if err != nil {
		return fmt.Errorf("failed to serialize route matrix: %w", err)
	}
	r.RouteMatrix = string(data)
	return nil
}

// ========================= Cloud Instance Management =========================

// CloudInstance represents a VPS instance from cloud providers
// Linked to SlaveNode via ip_address field (no foreign key, query-time join)
type CloudInstance struct {
	ID        uint64         `gorm:"primarykey"`
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt gorm.DeletedAt `gorm:"index"`

	// Provider identification
	Provider    string `gorm:"type:varchar(20);not null;index"`                              // aliyun_swas, aws_lightsail, bandwagon
	AccountName string `gorm:"type:varchar(50);not null;index"`                              // Reference to config file account
	InstanceID  string `gorm:"type:varchar(100);not null;uniqueIndex:idx_provider_instance"` // Provider-specific instance ID

	// Instance details
	Name        string `gorm:"type:varchar(100)"`              // Instance name/hostname from provider
	IPAddress   string `gorm:"type:varchar(45);not null;index"` // Current public IPv4 (for SlaveNode join)
	IPv6Address string `gorm:"type:varchar(100)"`               // IPv6 address (if available)
	Region      string `gorm:"type:varchar(50);not null"`       // Provider region/datacenter

	// Traffic & billing
	TrafficUsedBytes  int64 `gorm:"not null;default:0"` // Used traffic in current cycle
	TrafficTotalBytes int64 `gorm:"not null;default:0"` // Total traffic allowance
	TrafficResetAt    int64 `gorm:"not null;default:0"` // Next traffic reset (Unix timestamp)
	ExpiresAt         int64 `gorm:"not null;default:0"` // Instance expiration (Unix timestamp, 0=auto-renew)

	// Sync status
	// Note: Instance online status is determined by associated SlaveNode existence
	LastSyncedAt int64  `gorm:"not null;default:0"` // Last successful sync (Unix timestamp)
	SyncError    string `gorm:"type:text"`          // Last sync error message
}

// ========================= Batch Script Execution =========================

// SlaveBatchScript script template for batch execution (encrypted storage)
type SlaveBatchScript struct {
	ID              uint64 `gorm:"primarykey;autoIncrement"`
	CreatedAt       int64  `gorm:"autoCreateTime:milli"`
	UpdatedAt       int64  `gorm:"autoUpdateTime:milli"`
	Name            string `gorm:"size:128;not null;index"` // Script name for identification
	Description     string `gorm:"size:512"`                 // Script description
	Content         string `gorm:"type:text;not null"`       // Encrypted script content (via secretEncrypt)
	ExecuteWithSudo bool   `gorm:"default:false;not null"`   // Execute script with sudo privileges
}

// SlaveBatchTask batch execution task
type SlaveBatchTask struct {
	ID           uint64 `gorm:"primarykey;autoIncrement"`
	CreatedAt    int64  `gorm:"autoCreateTime:milli"`
	UpdatedAt    int64  `gorm:"autoUpdateTime:milli"`
	ScriptID     uint64 `gorm:"not null;index"`                        // Associated script
	NodeIDs      string `gorm:"type:text;not null"`                    // JSON array of node IDs: [1,2,3]
	ScheduleType string `gorm:"size:16;not null"`                      // "once" | "cron"
	ExecuteAt    *int64 `gorm:"index"`                                 // Execute time for one-time tasks (milliseconds timestamp)
	CronExpr     string `gorm:"size:64"`                               // Cron expression: "0 2 * * *"
	Status       string `gorm:"size:16;not null;index"`                // "pending" | "running" | "paused" | "completed" | "failed"
	CurrentIndex int    `gorm:"default:0"`                             // Current node index (0-based)
	TotalNodes   int    `gorm:"not null"`                              // Total number of nodes
	CreatedBy    uint64 `gorm:"index"`                                 // Creator user ID
	CompletedAt  *int64                                                // Completion time
	AsynqTaskID  string `gorm:"type:varchar(128);index"`               // Asynq task ID for tracking
	ParentTaskID *uint64 `gorm:"index"`                                // Parent task ID for retry tracking
	IsEnabled    bool   `gorm:"default:true;not null"`                 // Whether scheduled task is enabled
}

// SlaveBatchTaskResult execution result for a single node
type SlaveBatchTaskResult struct {
	ID         uint64 `gorm:"primarykey;autoIncrement"`
	CreatedAt  int64  `gorm:"autoCreateTime:milli"`
	TaskID     uint64 `gorm:"not null;index:idx_task_node"`       // Associated task
	NodeID     uint64 `gorm:"not null;index:idx_task_node"`       // Associated node (SlaveNode.ID)
	NodeIndex  int    `gorm:"not null"`                            // Execution order (0-based)
	Status     string `gorm:"size:16;not null"`                    // "success" | "failed" | "skipped"
	Stdout     string `gorm:"type:text"`                           // Standard output
	Stderr     string `gorm:"type:text"`                           // Standard error
	ExitCode   int    `gorm:"default:-1"`                          // Exit code (-1 = not executed)
	Error      string `gorm:"type:text"`                           // Error message (SSH failure, etc.)
	StartedAt  *int64                                              // Start time
	EndedAt    *int64                                              // End time
	RetryCount int    `gorm:"default:0"`                           // Number of retry attempts
}

// SlaveBatchScriptVersion script version history for auditing
type SlaveBatchScriptVersion struct {
	ID        uint64 `gorm:"primarykey;autoIncrement"`
	CreatedAt int64  `gorm:"autoCreateTime:milli"`
	ScriptID  uint64 `gorm:"not null;index"`              // Associated script
	Version   int    `gorm:"not null"`                    // Version number
	Content   string `gorm:"type:text;not null"`          // Encrypted script content
	CreatedBy uint64 `gorm:"index"`                       // Creator user ID
}

// CloudOperationLog tracks async cloud operations
type CloudOperationLog struct {
	ID          uint64 `gorm:"primarykey;autoIncrement"`
	CreatedAt   int64  `gorm:"autoCreateTime:milli"`
	InstanceID  uint64 `gorm:"not null;index"`            // Associated cloud instance
	Operation   string `gorm:"size:50;not null"`          // change_ip, delete, create, sync
	Status      string `gorm:"size:20;not null;index"`    // pending, running, completed, failed
	AsynqTaskID string `gorm:"type:varchar(128);index"`   // Asynq task ID
	StartedAt   int64  `gorm:"not null"`                  // Operation start time
	CompletedAt *int64                                    // Operation completion time
	Error       string `gorm:"type:text"`                 // Error message if failed
}


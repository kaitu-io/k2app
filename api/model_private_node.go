package center

import (
	"fmt"

	"gorm.io/gorm"
)

// 专属节点订阅生命周期状态（见 spec §6.1 状态机）
const (
	PNStatusPending       = "pending"       // 订单已付，待开通入队
	PNStatusProvisioning  = "provisioning"  // VPS 创建中
	PNStatusActive        = "active"        // 正常服务
	PNStatusGrace         = "grace"         // 期满后宽限期，路由器仍可用
	PNStatusSuspended     = "suspended"     // 停机保 IP，路由器断连
	PNStatusDeprovisioned = "deprovisioned" // 终态，VPS 已销毁
	PNStatusFailed        = "failed"        // 开通失败
)

// 生命周期窗口（秒）。服务硬切点 = ExpiresAt + privateNodeGraceSeconds，由时间戳派生，
// 不依赖 cron 是否及时重贴标签 —— cron 停摆也不会泄漏永久免费服务。
const (
	privateNodeGraceSeconds   int64 = 7 * 86400  // 期满后宽限期：路由器仍可用，每日提醒
	privateNodeSuspendSeconds int64 = 14 * 86400 // 宽限结束后停机保 IP 期：路由器断连
)

// IP 类型
const (
	IPTypeResidential    = "residential"
	IPTypeNonResidential = "non_residential"
)

// PrivateNodeSubscription 专属节点订阅（商业对象）。
// 与现有 Subscription 表、User.ExpiredAt 零耦合：独立表、独立时钟。
type PrivateNodeSubscription struct {
	ID        uint64 `gorm:"primarykey" json:"id"`
	CreatedAt int64  `gorm:"autoCreateTime" json:"createdAt"`
	UpdatedAt int64  `gorm:"autoUpdateTime" json:"updatedAt"`

	// 归属
	UserID  uint64 `gorm:"not null;index" json:"userId"` // 主人
	PlanID  uint64 `gorm:"not null;index" json:"planId"` // 专属节点套餐（Plan.Product=private_node）
	OrderID uint64 `gorm:"uniqueIndex" json:"orderId"`    // 触发开通的订单（一单一 sub，幂等）

	// 基础设施绑定（开通后回填）
	CloudInstanceID *uint64 `gorm:"index" json:"cloudInstanceId,omitempty"` // → CloudInstance.ID
	SlaveNodeID     *uint64 `gorm:"index" json:"slaveNodeId,omitempty"`     // → SlaveNode.ID

	// 套餐属性 / 购买时选择
	// 购买时快照（与 PlanSpec 解耦，套餐日后可改不影响已购）
	Region            string `gorm:"type:varchar(50);not null" json:"region"`
	IPType            string `gorm:"type:varchar(20);not null" json:"ipType"` // residential | non_residential
	TrafficTotalBytes int64  `gorm:"not null" json:"trafficTotalBytes"`       // 流量配额，如 2TB

	// 生命周期（独立时钟，不碰 User.ExpiredAt）
	Status       string `gorm:"type:varchar(20);not null;index" json:"status"`
	PurchasedAt  int64  `gorm:"not null" json:"purchasedAt"`
	ExpiresAt    int64  `gorm:"not null;index" json:"expiresAt"` // 订阅期满（Unix 秒）
	GraceUntil   int64  `gorm:"not null;default:0" json:"graceUntil"`
	SuspendUntil int64  `gorm:"not null;default:0" json:"suspendUntil"`

	// 开通可观测
	ProvisionAttempts  int    `gorm:"not null;default:0" json:"provisionAttempts"`
	LastProvisionError string `gorm:"type:text" json:"-"`

	// 开通声明令牌：注入 VPS cloud-init，节点自注册时回传以认领归属（见 spec §7.4）。
	ProvisionClaimToken string `gorm:"type:varchar(64);index" json:"-"`
}

// IsServiceable 判定订阅当前是否应提供服务。
// 服务可用性以时间戳为权威：active/grace 均服务到 ExpiresAt+宽限期为止，与 cron 是否
// 已把 active 重贴为 grace 无关（cron 漏跑也不会泄漏永久免费服务）。suspended/
// deprovisioned/failed/pending/provisioning 一律不可服务。now 为 Unix 秒。
func (s *PrivateNodeSubscription) IsServiceable(now int64) bool {
	switch s.Status {
	case PNStatusActive, PNStatusGrace:
		return now < s.ExpiresAt+privateNodeGraceSeconds
	}
	return false
}

// PrivateNodePlanSpec 专属节点套餐的开通参数，与通用 Plan 解耦。
type PrivateNodePlanSpec struct {
	ID                uint64 `gorm:"primarykey" json:"id"`
	PlanID            uint64 `gorm:"uniqueIndex;not null" json:"planId"`        // → Plan.ID (Kind=private_node)
	Provider          string `gorm:"type:varchar(30);not null" json:"provider"` // aws_lightsail | ...
	IPType            string `gorm:"type:varchar(20);not null" json:"ipType"`
	AllowedRegions    string `gorm:"type:text" json:"allowedRegions"`   // JSON 数组：可选地区
	ImageID           string `gorm:"type:varchar(100)" json:"imageId"`  // 预构建镜像（含 k2s）
	BundleID          string `gorm:"type:varchar(100)" json:"bundleId"` // provider 实例规格
	TrafficTotalBytes int64  `gorm:"not null" json:"trafficTotalBytes"` // 流量配额
	// 所选 provider bundle 的月度自带流量（字节）。Model A 不变式的对照基准：
	// 卖出额度必须严格小于它，否则用户跑满会让我们吃 provider overage。
	BundleTransferBytes int64 `gorm:"not null;default:0" json:"bundleTransferBytes"`
}

// validatePrivateNodeQuotaInvariant 校验 Model A 成本安全不变式：卖出额度必须严格
// 小于 provider bundle 自带额度。硬上限是卖出额度的 100%（节点离线时 EpochHardCeilingBytes
// = TrafficTotalBytes），故用 >= 而非 95%。纯整数算术，无外部依赖。
func validatePrivateNodeQuotaInvariant(trafficTotalBytes, bundleTransferBytes int64) error {
	if trafficTotalBytes <= 0 {
		return fmt.Errorf("private node plan spec: trafficTotalBytes must be > 0, got %d", trafficTotalBytes)
	}
	if bundleTransferBytes <= 0 {
		return fmt.Errorf("private node plan spec: bundleTransferBytes must be > 0 (record the provider bundle's included allowance), got %d", bundleTransferBytes)
	}
	if trafficTotalBytes >= bundleTransferBytes {
		return fmt.Errorf("private node plan spec: sold quota %d >= bundle allowance %d — would expose us to provider overage; provision a larger bundle", trafficTotalBytes, bundleTransferBytes)
	}
	return nil
}

// BeforeSave 在每次 insert/update 前强制不变式。PrivateNodePlanSpec 无创建 handler，
// 仅靠直插 DB / 脚本 / 测试创建，故 hook 是唯一能覆盖所有路径的守卫点。
func (s *PrivateNodePlanSpec) BeforeSave(tx *gorm.DB) error {
	return validatePrivateNodeQuotaInvariant(s.TrafficTotalBytes, s.BundleTransferBytes)
}

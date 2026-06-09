package center

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
	UserID  uint64 `gorm:"not null;index" json:"userId"`  // 主人
	PlanID  uint64 `gorm:"not null;index" json:"planId"`  // 专属节点套餐（Plan.Kind=private_node）
	OrderID uint64 `gorm:"index" json:"orderId"`          // 触发开通的订单

	// 基础设施绑定（开通后回填）
	CloudInstanceID *uint64 `gorm:"index" json:"cloudInstanceId,omitempty"` // → CloudInstance.ID
	SlaveNodeID     *uint64 `gorm:"index" json:"slaveNodeId,omitempty"`     // → SlaveNode.ID

	// 套餐属性 / 购买时选择
	Region            string `gorm:"type:varchar(50);not null" json:"region"`
	IPType            string `gorm:"type:varchar(20);not null" json:"ipType"`   // residential | non_residential
	TrafficTotalBytes int64  `gorm:"not null" json:"trafficTotalBytes"`         // 流量配额，如 2TB

	// 生命周期（独立时钟，不碰 User.ExpiredAt）
	Status       string `gorm:"type:varchar(20);not null;index" json:"status"`
	PurchasedAt  int64  `gorm:"not null" json:"purchasedAt"`
	ExpiresAt    int64  `gorm:"not null;index" json:"expiresAt"`    // 订阅期满（Unix 秒）
	GraceUntil   int64  `gorm:"not null;default:0" json:"graceUntil"`
	SuspendUntil int64  `gorm:"not null;default:0" json:"suspendUntil"`

	// 开通可观测
	ProvisionAttempts  int    `gorm:"not null;default:0" json:"provisionAttempts"`
	LastProvisionError string `gorm:"type:text" json:"-"`
}

// IsServiceable 判定订阅当前是否应提供服务（active 或 宽限期内）。
// now 为 Unix 秒；显式传入便于纯函数测试。
func (s *PrivateNodeSubscription) IsServiceable(now int64) bool {
	switch s.Status {
	case PNStatusActive:
		return true
	case PNStatusGrace:
		return now < s.GraceUntil
	}
	return false
}

// PrivateNodePlanSpec 专属节点套餐的开通参数，与通用 Plan 解耦。
type PrivateNodePlanSpec struct {
	ID                uint64 `gorm:"primarykey" json:"id"`
	PlanID            uint64 `gorm:"uniqueIndex;not null" json:"planId"` // → Plan.ID (Kind=private_node)
	Provider          string `gorm:"type:varchar(30);not null" json:"provider"` // aws_lightsail | ...
	IPType            string `gorm:"type:varchar(20);not null" json:"ipType"`
	AllowedRegions    string `gorm:"type:text" json:"allowedRegions"` // JSON 数组：可选地区
	ImageID           string `gorm:"type:varchar(100)" json:"imageId"`  // 预构建镜像（含 k2s）
	BundleID          string `gorm:"type:varchar(100)" json:"bundleId"` // provider 实例规格
	TrafficTotalBytes int64  `gorm:"not null" json:"trafficTotalBytes"` // 流量配额
}

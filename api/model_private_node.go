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
	IPTypeUnknown        = "unknown" // 尚未上报/未知/非法归一化目标
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
	OrderID uint64 `gorm:"uniqueIndex" json:"orderId"`   // 触发开通的订单（一单一 sub，幂等）

	// 基础设施绑定（开通后回填）
	CloudInstanceID *uint64 `gorm:"index" json:"cloudInstanceId,omitempty"` // → CloudInstance.ID
	SlaveNodeID     *uint64 `gorm:"index" json:"slaveNodeId,omitempty"`     // → SlaveNode.ID
	// BoundIpv4 是节点身份的**持久连接键**：首次认领时记录该节点的 IPv4。节点重启走
	// create 路径(unregister 硬删行后)且一次性 token 已消费时，按此 IP 重新认领归属
	// —— 它是唯一能扛过 delete/recreate 的标识。IP 同时充当防劫持闸：别的 IP 持偷来的
	// token 也对不上。deprovision 时须清空(连同 SlaveNodeID/CloudInstanceID)，防 IP 回收误绑。
	BoundIpv4 string `gorm:"type:varchar(45);index" json:"boundIpv4,omitempty"`

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

// PrivateNodePlanSpec 专属节点套餐的业务参数，与通用 Plan 解耦。
// 只表达业务意图：卖多少流量 + 是否住宅 + 可选地区。provider / bundle / image 等
// "怎么部署"的实现细节不在此——由运维/agent 在部署时自行决定。成本安全护栏
// （所选 bundle 自带流量须 > 卖出额度）归属运维/主机，代码不再校验。
type PrivateNodePlanSpec struct {
	ID                uint64 `gorm:"primarykey" json:"id"`
	PlanID            uint64 `gorm:"uniqueIndex;not null" json:"planId"`      // → Plan.ID (Product=private_node)
	IPType            string `gorm:"type:varchar(20);not null" json:"ipType"` // residential | non_residential
	AllowedRegions    string `gorm:"type:text" json:"allowedRegions"`         // JSON 数组：可选地区
	TrafficTotalBytes int64  `gorm:"not null" json:"trafficTotalBytes"`       // 卖出流量配额
}

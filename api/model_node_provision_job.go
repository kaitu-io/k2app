package center

const (
	NPJStatusQueued       = "queued"       // 待 agent 认领
	NPJStatusClaimed      = "claimed"      // 已认领，agent 工作中
	NPJStatusProvisioning = "provisioning" // 建机/部署中（agent report）
	NPJStatusSucceeded    = "succeeded"    // 节点已自注册激活
	NPJStatusFailed       = "failed"       // agent 上报失败
)

// NodeProvisionJob 专属节点开通工作项：Center 发出，外部 AI agent 认领并完成建机+部署。
// sub.status 仍是 Center 的权威视图（provisioning→active 由节点自注册/超时清扫驱动）；
// 本表 status 只反映 agent 的工作进度（运维可见性）。
type NodeProvisionJob struct {
	ID        uint64 `gorm:"primarykey" json:"id"`
	CreatedAt int64  `gorm:"autoCreateTime" json:"createdAt"`
	UpdatedAt int64  `gorm:"autoUpdateTime" json:"updatedAt"`

	SubID  uint64 `gorm:"uniqueIndex;not null" json:"subId"` // 一 sub 一 job（幂等）
	Status string `gorm:"type:varchar(20);not null;index" json:"status"`

	// 租约：agent 原子认领
	Holder        string `gorm:"type:varchar(128)" json:"holder"`
	LeasedAt      int64  `gorm:"not null;default:0" json:"leasedAt"`
	LeaseDeadline int64  `gorm:"not null;default:0;index" json:"leaseDeadline"`

	// spec 快照（emit 时从 PrivateNodePlanSpec + sub 拍）
	Region            string `gorm:"type:varchar(50);not null" json:"region"`
	BundleID          string `gorm:"type:varchar(100)" json:"bundleId"`
	ImageID           string `gorm:"type:varchar(100)" json:"imageId"`
	ComposeVariant    string `gorm:"type:varchar(32);not null;default:'private'" json:"composeVariant"`
	K2Version         string `gorm:"type:varchar(32)" json:"k2Version"`
	TrafficTotalBytes int64  `gorm:"not null" json:"trafficTotalBytes"`
	IPType            string `gorm:"type:varchar(20);not null" json:"ipType"`
	Domain            string `gorm:"type:varchar(255)" json:"domain"`

	// result：agent 回填
	InstanceID string `gorm:"type:varchar(128)" json:"instanceId"`
	IPv4       string `gorm:"type:varchar(45)" json:"ipv4"`
	LastError  string `gorm:"type:text" json:"lastError,omitempty"`
}

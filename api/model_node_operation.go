package center

import (
	"encoding/json"

	"gorm.io/gorm"
)

// 动作类型。provision/change_ip/stop/destroy 在本期启用;upgrade_quota 预留不启用
// (家庭 2T→4T 升档是纯 DB 改配额,不进队列)。
const (
	NodeOpProvision = "provision"
	NodeOpChangeIP  = "change_ip"
	NodeOpStop      = "stop"
	NodeOpDestroy   = "destroy"
)

// 状态机。
const (
	NodeOpQueued     = "queued"
	NodeOpClaimed    = "claimed"
	NodeOpInProgress = "in_progress"
	NodeOpDone       = "done"
	NodeOpFailed     = "failed"
	NodeOpCanceled   = "canceled"
)

// nodeOpOpenStatuses = 占用 (sub, action) 槽位的"未结"状态,用于幂等去重与续费取消。
var nodeOpOpenStatuses = []string{NodeOpQueued, NodeOpClaimed, NodeOpInProgress}

// NodeOperation 专属节点运维任务:Center 派发,人工(未来 agent)认领并外部执行后回上报。
// 执行是外部人工动作(console/SSH);本表只记录意图 + 进度 + 结果(运维可见性)。
// sub.status 仍是订阅生命周期的权威视图,与本表 status 解耦。
//
// 去重不变式:同一 (sub_id, action) 至多一条未结(nodeOpOpenStatuses)记录。idx_op_sub_action_status
// 是非唯一索引(MariaDB 无便捷的 partial-unique),去重靠应用层 FOR UPDATE 锁 sub 行后 check-before-insert
// (见 dispatchNodeOperation / createNodeOperationChecked)。**任何新增的写入路径都必须经此锁**,否则
// 绕过去重会叠出并发重复任务。
type NodeOperation struct {
	ID        uint64 `gorm:"primarykey" json:"id"`
	CreatedAt int64  `gorm:"autoCreateTime" json:"createdAt"`
	UpdatedAt int64  `gorm:"autoUpdateTime" json:"updatedAt"`

	Action          string  `gorm:"type:varchar(20);not null;index:idx_op_sub_action_status,priority:2" json:"action"`
	SubID           uint64  `gorm:"not null;index:idx_op_sub_action_status,priority:1" json:"subId"`
	CloudInstanceID *uint64 `gorm:"index" json:"cloudInstanceId"`
	Status          string  `gorm:"type:varchar(20);not null;index:idx_op_sub_action_status,priority:3;index" json:"status"`

	// 租约:认领(人工与未来 agent 共用)
	Holder        string `gorm:"type:varchar(128)" json:"holder"`
	LeasedAt      int64  `gorm:"not null;default:0" json:"leasedAt"`
	LeaseDeadline int64  `gorm:"not null;default:0;index" json:"leaseDeadline"`

	Params    string `gorm:"type:json" json:"params"`              // 动作专属输入(typed payload marshal)
	Result    string `gorm:"type:json" json:"result"`              // 动作专属结果
	LastError string `gorm:"type:text" json:"lastError,omitempty"`

	CreatedBy   string `gorm:"type:varchar(64);not null" json:"createdBy"` // system:order | system:lifecycle | admin:<email>
	CompletedAt int64  `gorm:"not null;default:0" json:"completedAt"`
}

// BeforeSave 把空的 Params/Result 归一成 "{}"。json 列(MariaDB = LONGTEXT +
// CHECK json_valid)拒绝空串,而 Go 非指针 string 零值就是空串;在此统一兜底,
// 使所有 create/update 路径无需逐处手填 "{}"。
func (op *NodeOperation) BeforeSave(*gorm.DB) error {
	if op.Params == "" {
		op.Params = "{}"
	}
	if op.Result == "" {
		op.Result = "{}"
	}
	return nil
}

// ProvisionParams 是 action=provision 的 Params JSON(原 NodeProvisionJob 的 spec 快照)。
type ProvisionParams struct {
	Region            string `json:"region"`
	BundleID          string `json:"bundleId"`
	ImageID           string `json:"imageId"`
	ComposeVariant    string `json:"composeVariant"`
	K2Version         string `json:"k2Version"`
	TrafficTotalBytes int64  `json:"trafficTotalBytes"`
	IPType            string `json:"ipType"`
	Domain            string `json:"domain"`
}

// mustJSON marshal 任意 payload 为字符串(失败返 "{}",不 panic)。
func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

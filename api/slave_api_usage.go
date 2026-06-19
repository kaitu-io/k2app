package center

import (
	"time"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

const (
	trafficStopThresholdNum = 95 // 95% → stop（整数百分比避免浮点）
	trafficStopThresholdDen = 100
	// usageReportIntervalSec is defined in logic_node_usage.go as the single
	// source of truth for report cadence (A2). A3 rewrites this endpoint to
	// the pure recorder and must not redeclare it.
	trafficEpochPeriodSec = 30 * 86400 // MVP 月度周期；TrafficResetAt 由 Center 推进
)

// NodeUsageRequest 节点累计流量上报（非增量，对丢包/重复/乱序鲁棒）。
type NodeUsageRequest struct {
	EpochID         int64 `json:"epoch_id"`
	CumulativeBytes int64 `json:"cumulative_bytes"`
	Seq             int64 `json:"seq"`
	Ts              int64 `json:"ts"`
}

// NodeUsageResponse Center 裁决 + epoch 身份下发。
type NodeUsageResponse struct {
	Verdict               string `json:"verdict"` // serve | stop
	EpochID               int64  `json:"epoch_id"`
	QuotaTotal            int64  `json:"quota_total"`
	QuotaUsed             int64  `json:"quota_used"`
	EpochHardCeilingBytes int64  `json:"epoch_hard_ceiling_bytes"` // 节点离线时本地强制上限
	NextReportInterval    int64  `json:"next_report_interval"`
}

// api_slave_node_report_usage 处理 POST /slave/usage：节点上报累计流量，Center 存
// max(已见) + 算 95% 阈值 + 回 serve/stop 裁决 + epoch 身份。lazy epoch reset on
// heartbeat（无需额外 cron）。
func api_slave_node_report_usage(c *gin.Context) {
	node := ReqSlaveNode(c)
	if node == nil {
		Error(c, ErrorNotLogin, "node context required")
		return
	}
	var req NodeUsageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, "bad usage payload")
		return
	}

	// 找该节点的 CloudInstance（IP 撞回收取最新行）。
	var ci CloudInstance
	err := db.Get().Where("ip_address = ?", node.Ipv4).Order("id DESC").First(&ci).Error
	if err != nil {
		// 无 CloudInstance（如共享节点或尚未 sync）→ 不计量，放行。
		Success(c, &NodeUsageResponse{Verdict: "serve", NextReportInterval: usageReportIntervalSec})
		return
	}

	now := time.Now().Unix()
	updates := map[string]any{}

	// 1. lazy epoch reset：到期 → Center bump epoch + 清零 used + 推进 TrafficResetAt。
	if ci.TrafficResetAt > 0 && now >= ci.TrafficResetAt {
		ci.TrafficEpoch++
		ci.TrafficUsedBytes = 0
		ci.TrafficResetAt = now + trafficEpochPeriodSec
		updates["traffic_epoch"] = ci.TrafficEpoch
		updates["traffic_used_bytes"] = int64(0)
		updates["traffic_reset_at"] = ci.TrafficResetAt
	}

	// 2. 上报归并：仅当节点 epoch == 当前 epoch 才采纳累计值（取 max，幂等抗乱序/重复）。
	//    epoch 不符（节点落后于刚 reset）→ 不采纳其 cumulative，只把当前 epoch 回给节点令其清零。
	if req.EpochID == ci.TrafficEpoch && req.CumulativeBytes > ci.TrafficUsedBytes {
		ci.TrafficUsedBytes = req.CumulativeBytes
		updates["traffic_used_bytes"] = ci.TrafficUsedBytes
	}

	if len(updates) > 0 {
		if err := db.Get().Model(&CloudInstance{}).Where("id = ?", ci.ID).Updates(updates).Error; err != nil {
			log.Errorf(c, "[USAGE] persist ci=%d: %v", ci.ID, err)
		}
	}

	// 3. 裁决：used/total >= 95% → stop。total==0（未配额）→ serve。
	//    int64 整数算术：2TB*100 不溢出 int64。
	verdict := "serve"
	if ci.TrafficTotalBytes > 0 &&
		ci.TrafficUsedBytes*trafficStopThresholdDen >= ci.TrafficTotalBytes*trafficStopThresholdNum {
		verdict = "stop"
	}
	Success(c, &NodeUsageResponse{
		Verdict:               verdict,
		EpochID:               ci.TrafficEpoch,
		QuotaTotal:            ci.TrafficTotalBytes,
		QuotaUsed:             ci.TrafficUsedBytes,
		EpochHardCeilingBytes: ci.TrafficTotalBytes,
		NextReportInterval:    usageReportIntervalSec,
	})
}

package center

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// usageReportIntervalSec + the offline-window constants live in logic_node_usage.go
// (same package) — single source of truth for the report cadence. Do NOT redeclare
// it here; the response below returns that constant verbatim.

// NodeUsageRequest — node-reported cumulative usage (robust to loss/dup/reorder).
// JSON tags MUST match docker/sidecar NodeUsageRequest exactly.
type NodeUsageRequest struct {
	EpochID         int64 `json:"epoch_id"`          // node BillingCycleEndAt (node owns)
	CumulativeBytes int64 `json:"cumulative_bytes"`  // used in current epoch
	QuotaTotalBytes int64 `json:"quota_total_bytes"` // node .env limit (0 = unlimited)
	Seq             int64 `json:"seq"`
	Ts              int64 `json:"ts"`
}

// NodeUsageResponse — Center is a pure recorder now: ack only. No verdict /
// quota / epoch downstream (the node is the authority).
type NodeUsageResponse struct {
	NextReportInterval int64 `json:"next_report_interval"`
}

// api_slave_node_report_usage records POST /slave/usage into NodeUsage (keyed by
// NodeID). Pure recorder: follow node epoch, max used within epoch, adopt
// node-sourced quota, stamp last_report_at. No cutoff verdict (node-side
// authority). All nodes report; no private gate.
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

	now := time.Now().Unix()
	var u NodeUsage
	err := db.Get().Where("node_id = ?", node.ID).First(&u).Error
	if err != nil {
		// First report for this node → create.
		u = NodeUsage{NodeID: node.ID, Epoch: req.EpochID, UsedBytes: req.CumulativeBytes,
			QuotaTotalBytes: req.QuotaTotalBytes, LastReportAt: now}
		if cerr := db.Get().Create(&u).Error; cerr != nil {
			log.Errorf(c, "[USAGE] create node_usage node=%d: %v", node.ID, cerr)
		}
		// G2 (spec §8.5): a node serving with no cap is a silent cost risk. Fires
		// once per fresh node (this create branch runs once per NodeID).
		if req.QuotaTotalBytes == 0 {
			log.Warnf(c, "[USAGE] node=%d ip=%s reporting with NO quota limit (uncapped)", node.ID, node.Ipv4)
			go sendCloudSlackNotification(c.Request.Context(), "Node Uncapped",
				fmt.Sprintf("node=%d ip=%s first report has QuotaTotalBytes=0 (no cap set)", node.ID, node.Ipv4))
		}
		Success(c, &NodeUsageResponse{NextReportInterval: usageReportIntervalSec})
		return
	}

	updates := map[string]any{"quota_total_bytes": req.QuotaTotalBytes, "last_report_at": now}
	switch {
	case req.EpochID > u.Epoch: // node entered a new billing cycle → follow + reset
		updates["epoch"] = req.EpochID
		updates["used_bytes"] = req.CumulativeBytes
	case req.EpochID == u.Epoch && req.CumulativeBytes > u.UsedBytes: // same epoch → max
		updates["used_bytes"] = req.CumulativeBytes
	} // req.EpochID < u.Epoch (stale/reorder): leave used untouched
	if uerr := db.Get().Model(&NodeUsage{}).Where("node_id = ?", node.ID).Updates(updates).Error; uerr != nil {
		log.Errorf(c, "[USAGE] update node_usage node=%d: %v", node.ID, uerr)
	}

	Success(c, &NodeUsageResponse{NextReportInterval: usageReportIntervalSec})
}

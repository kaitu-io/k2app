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
// ipv4, the durable key). Pure recorder: follow node epoch, max used within epoch, adopt
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

	// Fail-safe: a node with no ipv4 (e.g. a future IPv6-only node) must NOT
	// write a row — an empty key collides on the unique index across all such
	// nodes. IPv6-only is unsupported stack-wide today (auth + registration also
	// key by ipv4); until that cross-cutting rework, skip + alarm here.
	if node.Ipv4 == "" {
		log.Warnf(c, "[USAGE] node=%d has empty ipv4; skipping usage write (IPv6-only unsupported)", node.ID)
		go sendCloudSlackNotification(c.Request.Context(), "Node Missing IPv4",
			fmt.Sprintf("node=%d reported usage with empty ipv4 — usage not recorded", node.ID))
		Success(c, &NodeUsageResponse{NextReportInterval: usageReportIntervalSec})
		return
	}

	var u NodeUsage
	err := db.Get().Where("ipv4 = ?", node.Ipv4).First(&u).Error
	if err != nil {
		// First report for this node IP → try create.
		created := NodeUsage{Ipv4: node.Ipv4, NodeID: node.ID, Epoch: req.EpochID,
			UsedBytes: req.CumulativeBytes, QuotaTotalBytes: req.QuotaTotalBytes, LastReportAt: now}
		if cerr := db.Get().Create(&created).Error; cerr == nil {
			// Genuine first report. G2 (spec §8.5): serving with no cap = silent cost risk.
			if req.QuotaTotalBytes == 0 {
				log.Warnf(c, "[USAGE] node=%d ip=%s reporting with NO quota limit (uncapped)", node.ID, node.Ipv4)
				go sendCloudSlackNotification(c.Request.Context(), "Node Uncapped",
					fmt.Sprintf("node=%d ip=%s first report has QuotaTotalBytes=0 (no cap set)", node.ID, node.Ipv4))
			}
			Success(c, &NodeUsageResponse{NextReportInterval: usageReportIntervalSec})
			return
		}
		// Lost the create race with a concurrent first report (unique ipv4). Re-read
		// and fall through to the update path so this report's bytes aren't dropped.
		if rerr := db.Get().Where("ipv4 = ?", node.Ipv4).First(&u).Error; rerr != nil {
			log.Errorf(c, "[USAGE] create+reread node_usage ip=%s: %v", node.Ipv4, rerr)
			Success(c, &NodeUsageResponse{NextReportInterval: usageReportIntervalSec})
			return
		}
	}

	updates := map[string]any{"quota_total_bytes": req.QuotaTotalBytes, "last_report_at": now, "node_id": node.ID}
	switch {
	case req.EpochID > u.Epoch: // node entered a new billing cycle → follow + reset
		updates["epoch"] = req.EpochID
		updates["used_bytes"] = req.CumulativeBytes
	case req.EpochID == u.Epoch && req.CumulativeBytes > u.UsedBytes: // same epoch → max
		updates["used_bytes"] = req.CumulativeBytes
	} // req.EpochID < u.Epoch (stale/reorder): leave used untouched
	if uerr := db.Get().Model(&NodeUsage{}).Where("ipv4 = ?", node.Ipv4).Updates(updates).Error; uerr != nil {
		log.Errorf(c, "[USAGE] update node_usage ip=%s: %v", node.Ipv4, uerr)
	}

	Success(c, &NodeUsageResponse{NextReportInterval: usageReportIntervalSec})
}

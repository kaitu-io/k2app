package center

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"

	"github.com/kaitu-io/k2app/api/cloudprovider"
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

// NodeUsageResponse — Center is a recorder plus an upward-only corrector. No
// verdict / quota / epoch downstream (the node keeps cutoff authority), but for
// AWS Lightsail nodes the response may carry the provider-billed usage as a
// one-way ratchet bound: the node adopts it only when it exceeds its own meter
// (sidecar AdoptAuthoritativeUsed), so a correction can only cut earlier —
// never re-open a cut node. JSON tags MUST match the sidecar struct exactly.
type NodeUsageResponse struct {
	NextReportInterval int64 `json:"next_report_interval"`
	// AuthoritativeUsedBytes: provider-billed usage for the node's current cycle
	// (0 / absent = no correction). Only sent when it exceeds the self-report —
	// see awsAuthoritativeUsedBytes for the gates.
	AuthoritativeUsedBytes int64 `json:"authoritative_used_bytes,omitempty"`
}

// awsAuthoritativeUsedBytes returns the cloud provider's billed usage for the
// node's CURRENT cycle when it exceeds the node's self-report, else 0. AWS
// Lightsail only: it is the one provider that keeps serving (and billing) past
// the allowance, so its synced figure acts as the correction upper bound. Gates
// (any miss → 0, i.e. no correction — always fail-open):
//   - the node IP maps to a synced aws_lightsail CloudInstance
//   - shared-pool instance (dedicated lines carry sold quota, not the bundle)
//   - the synced figure belongs to the SAME calendar-month cycle the node is
//     reporting (Lightsail resets on the 1st, UTC): both the node epoch and the
//     instance's TrafficResetAt must equal the upcoming month boundary —
//     otherwise a figure synced just before rollover would inflate a fresh cycle
//   - the sync is fresh (a stale figure is only ever an undercount of the
//     current cycle, but gate anyway so a wedged sync can't correct off garbage)
func awsAuthoritativeUsedBytes(ipv4 string, req NodeUsageRequest) int64 {
	const maxSyncStaleSec = 2 * 60 * 60 // sync cron defaults to every 30min

	now := time.Now().UTC()
	monthEnd := time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, time.UTC).Unix()
	if req.EpochID != monthEnd {
		return 0 // node cycle isn't the current calendar month — don't correct
	}

	var ci CloudInstance
	if err := db.Get().
		Where("provider = ? AND ip_address = ?", cloudprovider.ProviderAWSLightsail, ipv4).
		First(&ci).Error; err != nil {
		return 0 // not an AWS Lightsail node (or transient DB miss)
	}
	if ci.TrafficResetAt != monthEnd {
		return 0 // synced figure is from a previous cycle (pre-rollover sync)
	}
	if now.Unix()-ci.LastSyncedAt > maxSyncStaleSec {
		return 0
	}
	if isPrivateCloudInstance(ci.ID) {
		return 0
	}
	if ci.TrafficUsedBytes <= req.CumulativeBytes {
		return 0 // node meter is already at or above the provider figure
	}
	return ci.TrafficUsedBytes
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
			// First report still gets the AWS correction — a freshly onboarded
			// node is exactly the one whose meter starts at 0.
			respondUsageWithCorrection(c, node, req)
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

	respondUsageWithCorrection(c, node, req)
}

// respondUsageWithCorrection finishes a recorded report: attaches the AWS
// provider-authoritative figure when applicable (raising the stored record to
// it as well) and sends the ack. Shared by the first-report and update paths.
func respondUsageWithCorrection(c *gin.Context, node *SlaveNode, req NodeUsageRequest) {
	resp := &NodeUsageResponse{NextReportInterval: usageReportIntervalSec}
	if auth := awsAuthoritativeUsedBytes(node.Ipv4, req); auth > 0 {
		resp.AuthoritativeUsedBytes = auth
		// Keep the record from lagging the provider too: raise used_bytes to the
		// authoritative figure for the same epoch. Conditional so a concurrent
		// higher report is never lowered.
		if uerr := db.Get().Model(&NodeUsage{}).
			Where("ipv4 = ? AND epoch = ? AND used_bytes < ?", node.Ipv4, req.EpochID, auth).
			Update("used_bytes", auth).Error; uerr != nil {
			log.Errorf(c, "[USAGE] raise node_usage to authoritative ip=%s: %v", node.Ipv4, uerr)
		}
		log.Warnf(c, "[USAGE] provider-authoritative correction ip=%s self=%d provider=%d",
			node.Ipv4, req.CumulativeBytes, auth)
	}
	Success(c, resp)
}

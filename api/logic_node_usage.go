package center

import (
	db "github.com/wordgate/qtoolkit/db"
)

// quotaCutoffReserveBytes is the single cutoff/hide reserve (spec I-Cutoff-Rule):
// cut/hide when used >= limit - 500MiB. Node enforcer and Center use the SAME
// value. limit == 0 means unlimited → never cut (guard in isNodeOverQuota).
const quotaCutoffReserveBytes int64 = 500 << 20 // 500 MiB

// usageReportIntervalSec is the canonical report cadence Center hands every node
// back as NextReportInterval (A3) AND the unit the offline window derives from.
// Single source of truth for "how often a node reports": the node obeys the
// server-returned NextReportInterval (B3), so this one constant sets the rate.
const usageReportIntervalSec = 60

// nodeOfflineReportCycles is the consecutive missed-report count before a node
// is offline-derived (spec §5.2, N=3).
const nodeOfflineReportCycles = 3

// nodeOfflineSeconds is DERIVED, never hand-tuned: N cycles × the report
// interval. Change usageReportIntervalSec and the offline window tracks it — no
// silent drift between "how often we expect a report" and "when we give up"
// (spec §5.2 ≈180s). isNodeOffline computes the verdict dynamically from
// LastReportAt + now; this is only the threshold. Center cannot physically pause
// an unreachable node — the real cut is node-side fail-closed (§4.1); offline
// gates visibility + alarm only.
const nodeOfflineSeconds = nodeOfflineReportCycles * usageReportIntervalSec

// isNodeOverQuota is the single over-quota/hide rule for ALL nodes (no
// shared/private branch). limit == 0 (unlimited) is never over.
func isNodeOverQuota(u *NodeUsage) bool {
	if u == nil || u.QuotaTotalBytes <= 0 {
		return false
	}
	return u.UsedBytes >= u.QuotaTotalBytes-quotaCutoffReserveBytes
}

// isNodeOffline reports whether a node has gone silent past the offline window.
// LastReportAt == 0 (never reported) is NOT offline here — "node serving but
// uncapped/never-reported" is G2's concern, not offline hiding.
func isNodeOffline(u *NodeUsage, now int64) bool {
	if u == nil || u.LastReportAt == 0 {
		return false
	}
	return now-u.LastReportAt > nodeOfflineSeconds
}

// getNodeUsagesByIPs batch-loads NodeUsage rows keyed by the durable ipv4 key.
// Missing rows are simply absent from the map (caller treats absent as "no usage
// data" → neutral 0.5 / not hidden). Keyed by ipv4 (not SlaveNode.ID) so a node
// re-registering with a new id keeps its usage row matched.
func getNodeUsagesByIPs(ips []string) map[string]*NodeUsage {
	out := make(map[string]*NodeUsage, len(ips))
	if len(ips) == 0 {
		return out
	}
	var rows []NodeUsage
	if err := db.Get().Where("ipv4 IN ?", ips).Find(&rows).Error; err != nil {
		return out
	}
	for i := range rows {
		out[rows[i].Ipv4] = &rows[i]
	}
	return out
}

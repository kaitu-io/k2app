package center

import "math"

// usageSensitivityFloor is the minimum usage-sensitivity weight w(t), applied
// at the very start of a billing cycle (timeRatio == 0). A small non-zero floor
// means a brand-new cycle is NOT completely blind to usage — a node that burns
// most of its quota on day 1 still loses a little score (e.g. 100% used → 0.85)
// — but the floor is low enough that early usage is largely forgiven. True
// exhaustion is caught by the hard cutoff / hide path (isNodeOverQuota), not by
// the score.
const usageSensitivityFloor = 0.15

// usageSensitivityGamma is the exponent on timeRatio in the weight ramp. γ=2
// (quadratic) keeps the first half of the cycle generous and concentrates the
// rise in sensitivity into the back half, so "越靠后越敏感" without punishing
// healthy mid-cycle usage.
const usageSensitivityGamma = 2.0

// ComputeRecommendScore returns a unified "how recommendable is this tunnel
// right now" signal in [0.0, 1.0]. Higher = better.
//
// This is the single authority for tunnel scoring. Both /api/tunnels and
// /api/subs route every tunnel through this function so the Dashboard UI
// indicator and the daemon's weighted-random Pick agree on which nodes are
// good picks.
//
// Non-cloud nodes (no CloudInstance, inst == nil) return 0.5 — a neutral score
// that keeps them eligible for weighted pick without either favoring or
// blacklisting them. Returning 0 would effectively exclude them from
// pickWeighted (weight=0 is treated as disqualified unless all candidates are
// 0).
//
// Formula — time-gated usage sensitivity:
//
//	w(t) := usageSensitivityFloor + (1 − usageSensitivityFloor) · t^γ
//	score := 1 − trafficRatio · w(timeRatio)
//	clamp to [0, 1]
//
// w(t) is the weight given to usage. It rises from the floor (~0.15) at the
// start of the cycle to 1.0 at the end. Two consequences, both intentional:
//
//   - ∂score/∂trafficRatio = −w(t): the score's sensitivity to usage GROWS as
//     the cycle progresses. Late-cycle near-cap nodes are punished hard; early
//     nodes are largely forgiven. ("时间越靠后，约束越敏感")
//   - at fixed usage, score DECREASES as t rises: the further from the cycle
//     end, the higher the score. ("离周期结束越久，推荐分数越高")
//
// This replaces the earlier pacing model (score = (1 − (trafficRatio −
// timeRatio))/2 with a warmup window + headroom credit). The pacing model
// penalized early heavy usage and — its blind spot — kept recommending a
// near-cap node at month-end because it had "paced well". The new model is
// generous early and steers load away from late near-cap nodes.
//
// Examples (30-day billing cycle, γ=2):
//
//	day 1,  0%  used:  w=0.151 → score 1.00 (🟢)
//	day 1,  80% used:  w=0.151 → score 0.88 (🟢, forgiven — cutoff is the net)
//	day 15, 60% used:  w=0.363 → score 0.78 (🟢)
//	day 30, 50% used:  w=1.000 → score 0.50 (🟡)
//	day 30, 90% used:  w=1.000 → score 0.10 (🔴, steered away)
func ComputeRecommendScore(inst *DataTunnelInstance) float64 {
	if inst == nil {
		return 0.5
	}

	t := inst.TimeRatio
	w := usageSensitivityFloor + (1.0-usageSensitivityFloor)*math.Pow(t, usageSensitivityGamma)
	score := 1.0 - inst.TrafficRatio*w

	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}
	return score
}

// shouldHideTunnelForUser is the single hide decision for /api/tunnels and
// /api/subs. Admins see everything (triage); non-admins are shielded from
// over-quota or offline nodes so weighted-pick never lands on a dead/overage
// target. `now` is Unix seconds.
func shouldHideTunnelForUser(u *NodeUsage, isAdmin bool, now int64) bool {
	if isAdmin {
		return false
	}
	return isNodeOverQuota(u) || isNodeOffline(u, now)
}

// buildTunnelInstanceDataFromUsage builds the scoring DTO from NodeUsage.
// nil usage → nil (ComputeRecommendScore(nil)=0.5 neutral). TimeRatio uses the
// node-reported billing-cycle end (Epoch). Mirrors the old buildTunnelInstanceData
// by also stamping the DTO's own RecommendScore.
func buildTunnelInstanceDataFromUsage(u *NodeUsage) *DataTunnelInstance {
	if u == nil {
		return nil
	}
	trafficRatio := 0.0
	if u.QuotaTotalBytes > 0 {
		trafficRatio = float64(u.UsedBytes) / float64(u.QuotaTotalBytes)
		if trafficRatio > 1 {
			trafficRatio = 1
		}
	}
	timeRatio := calculateTimeRatio(u.Epoch)
	d := &DataTunnelInstance{
		TrafficTotalBytes: u.QuotaTotalBytes,
		TrafficRatio:      trafficRatio,
		BillingCycleEndAt: u.Epoch,
		TimeRatio:         timeRatio,
		BudgetScore:       trafficRatio - timeRatio,
	}
	d.RecommendScore = ComputeRecommendScore(d)
	return d
}

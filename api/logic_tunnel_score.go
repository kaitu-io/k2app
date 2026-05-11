package center

import "math"

// warmupWindow is the fraction of the billing cycle treated as "early"
// (the first 20% — about 6 days of a 30-day cycle). Inside this window the
// trafficRatio − timeRatio projection is statistically noisy, so we attenuate
// the over-pace penalty and add a headroom credit. After this point the score
// matches the original formula exactly.
const warmupWindow = 0.20

// earlyHeadroomLift is the maximum lift applied to score from unused budget
// during a brand-new cycle (timeRatio == 0). Decays linearly to 0 by
// warmupWindow. With trafficRatio == 0 and timeRatio == 0 this contributes
// 0.30, putting a fresh node solidly in the green band.
const earlyHeadroomLift = 0.30

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
// Formula:
//
//   - budgetScore := trafficRatio − timeRatio                  // [-1, +1]
//   - warmup       := min(1, timeRatio / warmupWindow)          // 0..1
//   - In the warmup window, attenuate over-pace (positive budgetScore) by
//     warmup; under-pace (negative budgetScore) keeps full strength because
//     it is a positive signal that does not need dampening.
//   - score        := (1 − adjustedBudget) / 2
//   - Add an early-cycle headroom credit that decays to 0 by warmupWindow:
//     (1 − warmup) × (1 − trafficRatio) × earlyHeadroomLift
//   - Clamp to [0, 1].
//
// Examples (30-day billing cycle):
//
//	day 15, 60% used:  warmup=1.00 → score 0.45 (🟡, unchanged)
//	day 30, 50% used:  warmup=1.00 → score 0.75 (🟢, unchanged)
//	day 1,  20% used:  warmup=0.17, headroom +0.20 → score 0.69 (🟢)
//	day 1,  0%  used:  warmup=0.17, headroom +0.25 → score 0.77 (🟢)
//	day 3,  80% used:  warmup=0.50, headroom +0.03 → score 0.36 (🟡, softened)
func ComputeRecommendScore(inst *DataTunnelInstance) float64 {
	if inst == nil {
		return 0.5
	}

	budgetScore := inst.TrafficRatio - inst.TimeRatio
	warmup := math.Min(1.0, inst.TimeRatio/warmupWindow)

	adj := budgetScore
	if adj > 0 {
		adj *= warmup
	}

	score := (1.0 - adj) / 2.0
	score += (1.0 - warmup) * (1.0 - inst.TrafficRatio) * earlyHeadroomLift

	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}
	return score
}

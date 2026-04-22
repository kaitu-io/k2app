package center

// ComputeRecommendScore returns a unified "how recommendable is this tunnel right
// now" signal in [0.0, 1.0]. Higher = better.
//
// This is the single authority for tunnel scoring. Both /api/tunnels and
// /api/subs route every tunnel through this function so the Dashboard UI
// indicator and the daemon's weighted-random Pick agree on which nodes are
// good picks.
//
// Non-cloud nodes (no CloudInstance, inst == nil) return 0.5 — a neutral score
// that keeps them eligible for weighted pick without either favoring or
// blacklisting them. Returning 0 would effectively exclude them from pickWeighted
// (weight=0 is treated as disqualified unless all candidates are 0).
//
// Formula: collapses the signed budget_score (trafficRatio - timeRatio, [-1, +1])
// into a "higher is better" score in [0, 1]:
//
//	recommendScore = (1 - budgetScore) / 2
//
// Examples (with a 30-day billing cycle):
//
//	day 15, 60% traffic used: budgetScore = 0.60 - 0.50 = +0.10 → score 0.45 (🟡)
//	day 15, 40% traffic used: budgetScore = 0.40 - 0.50 = -0.10 → score 0.55 (🟡)
//	day 3,  80% traffic used: budgetScore = 0.80 - 0.10 = +0.70 → score 0.15 (🔴)
//	day 30, 50% traffic used: budgetScore = 0.50 - 1.00 = -0.50 → score 0.75 (🟢)
func ComputeRecommendScore(inst *DataTunnelInstance) float64 {
	if inst == nil {
		return 0.5
	}
	score := (1.0 - inst.BudgetScore) / 2.0
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}
	return score
}

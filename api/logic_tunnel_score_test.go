package center

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestComputeRecommendScore_NilInstance(t *testing.T) {
	// Non-cloud nodes (no CloudInstance) must return the neutral 0.5 so they
	// remain eligible in pickWeighted without being favored or blacklisted.
	assert.Equal(t, 0.5, ComputeRecommendScore(nil))
}

func TestComputeRecommendScore_Formula(t *testing.T) {
	// Table mirrors the worked examples the user and I walked through in the
	// design discussion — each row is a "30-day billing cycle" scenario where
	// budgetScore = trafficRatio - timeRatio.
	cases := []struct {
		name        string
		budgetScore float64
		wantScore   float64
	}{
		// Corner: lots of budget left → ideal pick
		{"ideal, brand-new node", -1.0, 1.0},
		// Halfway, perfectly on pace → neutral
		{"on pace", 0.0, 0.5},
		// User's worked example: day 15/30 with 60% traffic → budget +0.10 → 0.45
		{"day 15/30, 60% used", 0.10, 0.45},
		// Halfway with consumption matching → also neutral
		{"day 15/30, 50% used", 0.0, 0.5},
		// Healthy: used 40% at the halfway mark
		{"day 15/30, 40% used", -0.10, 0.55},
		// Very healthy: used 20% at the halfway mark
		{"day 15/30, 20% used", -0.30, 0.65},
		// Month end with half the budget still untouched
		{"day 30/30, 50% used", -0.50, 0.75},
		// Burning budget fast: month start (10%) but already 80% used
		{"day 3/30, 80% used", 0.70, 0.15},
		// Corner: fully over budget
		{"worst, fully over budget", 1.0, 0.0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inst := &DataTunnelInstance{BudgetScore: tc.budgetScore}
			got := ComputeRecommendScore(inst)
			assert.InDelta(t, tc.wantScore, got, 1e-9)
		})
	}
}

func TestComputeRecommendScore_ClampsOutOfRangeInputs(t *testing.T) {
	// Defensive clamping: if an upstream bug produces a budgetScore outside
	// [-1, +1], we still return a valid [0, 1] score rather than propagating
	// the error downstream.
	assert.Equal(t, 1.0, ComputeRecommendScore(&DataTunnelInstance{BudgetScore: -2.0}))
	assert.Equal(t, 0.0, ComputeRecommendScore(&DataTunnelInstance{BudgetScore: 5.0}))
}

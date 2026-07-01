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

func TestComputeRecommendScore_EarlyCycleGenerous(t *testing.T) {
	// Time-gated sensitivity model: early in the cycle the usage-sensitivity
	// weight w(t) = 0.15 + 0.85·t² is near its floor, so even a node that has
	// burned a meaningful slice of quota scores high — there is a whole month
	// of runway ahead and the hard cutoff (isNodeOverQuota) is the real safety
	// net. Real example: 200 GB cap, 40 GB used (20%) on day 1 (3.3% time
	// elapsed). w(0.033)=0.150926, score=1−0.20·0.150926≈0.9698 — solid green.
	inst := &DataTunnelInstance{TrafficRatio: 0.20, TimeRatio: 0.033}
	got := ComputeRecommendScore(inst)
	assert.GreaterOrEqual(t, got, 0.6,
		"early-cycle low-usage tunnel must land in the green band")
	assert.InDelta(t, 0.9698, got, 0.001,
		"w(0.033)=0.150926, score=1−0.20·0.150926")
}

func TestComputeRecommendScore_Formula(t *testing.T) {
	// Each row is a concrete (trafficRatio, timeRatio) scenario in a 30-day
	// billing cycle. The model is score = 1 − trafficRatio·w(timeRatio), with
	// w(t) = usageSensitivityFloor + (1−usageSensitivityFloor)·t²
	//      = 0.15 + 0.85·t².
	//
	// Two properties the model encodes (vs the retired pacing model):
	//   - ∂score/∂trafficRatio = −w(t): the penalty per unit of usage GROWS as
	//     the cycle progresses → "越靠后越敏感".
	//   - at fixed usage, score DECREASES as t rises → "离周期结束越久(越早)分数越高".
	cases := []struct {
		name         string
		trafficRatio float64
		timeRatio    float64
		wantScore    float64
	}{
		// === Cycle end (t=1.0, w=1.0): score tracks usage directly ===
		// Brand-new node at month-end — ideal pick, full quota untouched
		{"day 30/30, 0% used", 0.00, 1.00, 1.00},
		// Month-end with half the budget still untouched → neutral
		{"day 30/30, 50% used", 0.50, 1.00, 0.50},
		// Month-end near cap — strongly de-recommended (fixes old blind spot
		// where the pacing model called this "well paced" at score 0.55)
		{"day 30/30, 90% used", 0.90, 1.00, 0.10},
		// Month-end fully exhausted
		{"day 30/30, 100% used", 1.00, 1.00, 0.00},

		// === Mid cycle (t=0.5, w=0.3625): generous, not penalized ===
		{"day 15/30, 50% used", 0.50, 0.50, 0.81875},
		{"day 15/30, 60% used", 0.60, 0.50, 0.7825},
		{"day 15/30, 40% used", 0.40, 0.50, 0.855},
		{"day 15/30, 20% used", 0.20, 0.50, 0.9275},
		// Mid-cycle fully exhausted is still only mildly penalized — absolute
		// exhaustion is caught by the hard-cutoff hide, not the score
		{"day 15/30, 100% used", 1.00, 0.50, 0.6375},

		// === Three-quarter cycle (t=0.75, w=0.628125): sensitivity ramping ===
		{"day 23/30, 60% used", 0.60, 0.75, 0.623125},

		// === Early cycle (t small, w near floor 0.15): very generous ===
		// t=0.2 (day 6): w=0.184
		{"day 6/30, 60% used", 0.60, 0.20, 0.8896},
		// Day 3 (t=0.10): even 80% used stays green — runway is long, cutoff
		// is the safety net
		{"day 3/30, 80% used", 0.80, 0.10, 0.87324},
		// Day 1 (t=0.033), 20% used (40 GB on a 200 GB cap)
		{"day 1/30, 20% used (200G cap, 40G used)", 0.20, 0.033, 0.96982},
		// Brand-new node, day 1, no traffic
		{"day 1/30, 0% used", 0.00, 0.033, 1.00},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inst := &DataTunnelInstance{
				TrafficRatio: tc.trafficRatio,
				TimeRatio:    tc.timeRatio,
			}
			got := ComputeRecommendScore(inst)
			assert.InDelta(t, tc.wantScore, got, 1e-3)
		})
	}
}

func TestComputeRecommendScore_ClampsOutOfRangeInputs(t *testing.T) {
	// Defensive clamping: if an upstream bug produces ratios outside [0, 1],
	// we still return a valid [0, 1] score rather than propagating the error
	// downstream. Negative usage would push score above 1; usage far over the
	// cap late in the cycle would push it below 0.
	assert.Equal(t, 1.0, ComputeRecommendScore(&DataTunnelInstance{TrafficRatio: -1.0, TimeRatio: 0.5}))
	assert.Equal(t, 0.0, ComputeRecommendScore(&DataTunnelInstance{TrafficRatio: 2.0, TimeRatio: 1.0}))
}

// NOTE: the old CloudInstance-by-IP hide helpers (isTunnelOverQuota,
// isPrivateTunnelExhausted, shouldHideTunnelForUser(*CloudInstance,bool)) were
// retired in favor of the unified NodeUsage rule. The hide decision is now
// pinned by TestHideRule_UnifiedReserve (api_tunnel_overquota_test.go) and the
// NodeUsage read-model tests (logic_node_usage*_test.go).

// TestShouldHideTunnelForUser_AdminBypassAndOffline pins the composed call-site
// decision now sourced from NodeUsage: admins always see everything; non-admins
// are shielded from over-quota OR offline nodes; nil usage is never hidden.
func TestShouldHideTunnelForUser_AdminBypassAndOffline(t *testing.T) {
	now := int64(1_000_000)
	over := &NodeUsage{QuotaTotalBytes: 1 << 40, UsedBytes: 1 << 40}
	offline := &NodeUsage{QuotaTotalBytes: 0, LastReportAt: now - nodeOfflineSeconds - 1}
	healthy := &NodeUsage{QuotaTotalBytes: 1 << 40, UsedBytes: 1 << 30, LastReportAt: now}

	assert.True(t, shouldHideTunnelForUser(over, false, now), "non-admin over-quota hidden")
	assert.True(t, shouldHideTunnelForUser(offline, false, now), "non-admin offline hidden")
	assert.False(t, shouldHideTunnelForUser(healthy, false, now), "non-admin healthy visible")
	assert.False(t, shouldHideTunnelForUser(nil, false, now), "nil usage never hidden")

	assert.False(t, shouldHideTunnelForUser(over, true, now), "admin sees over-quota")
	assert.False(t, shouldHideTunnelForUser(offline, true, now), "admin sees offline")
}

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

func TestComputeRecommendScore_EarlyCycleBugCase(t *testing.T) {
	// Regression test for the bug where a node with plenty of remaining budget
	// gets a yellow/orange score on day 1 of a 30-day cycle just because the
	// trafficRatio − timeRatio projection is statistically meaningless that
	// early. Real example: 200 GB cap, 40 GB used (20%) on day 1 (3.3% time
	// elapsed) — projection says "panic", reality says "still 80% headroom on
	// day 1, very recommendable". After the fix this lands in the green band
	// (≥ 0.6).
	inst := &DataTunnelInstance{TrafficRatio: 0.20, TimeRatio: 0.033}
	got := ComputeRecommendScore(inst)
	assert.GreaterOrEqual(t, got, 0.6,
		"early-cycle low-usage tunnel must land in the green band")
	assert.InDelta(t, 0.687, got, 0.005,
		"matches design proposal: warmup=0.165, adj=0.028, headroom=0.200, score≈0.687")
}

func TestComputeRecommendScore_Formula(t *testing.T) {
	// Each row is a concrete (trafficRatio, timeRatio) scenario in a 30-day
	// billing cycle. Late-cycle rows (timeRatio ≥ 0.20) hit the unmodified
	// path and match the original formula score = (1 − (trafficRatio −
	// timeRatio)) / 2. Early-cycle rows exercise the warmup attenuation +
	// headroom credit added to fix the day-1 false-orange bug.
	cases := []struct {
		name         string
		trafficRatio float64
		timeRatio    float64
		wantScore    float64
	}{
		// === Late cycle (warmup = 1.0, headroom credit = 0): unchanged ===
		// Brand-new node at month-end — ideal pick, full month of headroom
		{"day 30/30, 0% used", 0.00, 1.00, 1.00},
		// Halfway, perfectly on pace
		{"day 15/30, 50% used", 0.50, 0.50, 0.50},
		// Halfway, 60% used (slightly over pace)
		{"day 15/30, 60% used", 0.60, 0.50, 0.45},
		// Halfway, 40% used (slightly under pace)
		{"day 15/30, 40% used", 0.40, 0.50, 0.55},
		// Halfway, 20% used (well under pace)
		{"day 15/30, 20% used", 0.20, 0.50, 0.65},
		// Month-end with half the budget still untouched
		{"day 30/30, 50% used", 0.50, 1.00, 0.75},
		// Halfway, fully exhausted — genuinely bad
		{"day 15/30, 100% used", 1.00, 0.50, 0.25},
		// Cycle done, fully exhausted — neutral (cycle wrapping)
		{"day 30/30, 100% used", 1.00, 1.00, 0.50},

		// === Warmup boundary (timeRatio = 0.20, warmup = 1.0 exactly) ===
		// On pace at the warmup boundary: identical to late-cycle behavior
		{"day 6/30, 20% used (warmup boundary)", 0.20, 0.20, 0.50},

		// === Inside warmup window (timeRatio < 0.20) ===
		// Day 3 of 30 (timeR=0.10), 80% used: under old formula this was a deep
		// red 0.15. New formula softens to 0.355 (still warning) because the
		// projection signal is half-strength inside the warmup window — but
		// absolute usage of 80% still keeps it well below green.
		{"day 3/30, 80% used", 0.80, 0.10, 0.355},
		// User bug case: day 1, 20% used (40 GB on a 200 GB cap)
		{"day 1/30, 20% used (200G cap, 40G used)", 0.20, 0.033, 0.6866},
		// Brand-new node, day 1, no traffic: should be very recommendable
		{"day 1/30, 0% used", 0.00, 0.033, 0.7670},
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
	// downstream.
	assert.Equal(t, 0.0, ComputeRecommendScore(&DataTunnelInstance{TrafficRatio: 2.0, TimeRatio: 0.5}))
	assert.Equal(t, 1.0, ComputeRecommendScore(&DataTunnelInstance{TrafficRatio: 0.0, TimeRatio: 2.0}))
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

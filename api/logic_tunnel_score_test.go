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

func TestIsTunnelOverQuota_NilInstance(t *testing.T) {
	// Non-cloud nodes have no quota information — never filter them out.
	assert.False(t, isTunnelOverQuota(nil))
}

func TestIsTunnelOverQuota_ZeroTotal(t *testing.T) {
	// TrafficTotalBytes == 0 means unlimited / unconfigured. Must not be
	// treated as "over quota" (would hide every such node).
	inst := &CloudInstance{TrafficUsedBytes: 999_000_000_000, TrafficTotalBytes: 0}
	assert.False(t, isTunnelOverQuota(inst))
}

func TestIsTunnelOverQuota_HalfUsed(t *testing.T) {
	// 50% used — well below the 95% threshold.
	inst := &CloudInstance{
		TrafficUsedBytes:  500 * 1024 * 1024 * 1024,
		TrafficTotalBytes: 1024 * 1024 * 1024 * 1024,
	}
	assert.False(t, isTunnelOverQuota(inst))
}

func TestIsTunnelOverQuota_JustBelowThreshold(t *testing.T) {
	// 94% used — under the 95% buffer threshold.
	inst := &CloudInstance{
		TrafficUsedBytes:  940 * 1024 * 1024 * 1024,
		TrafficTotalBytes: 1000 * 1024 * 1024 * 1024,
	}
	assert.False(t, isTunnelOverQuota(inst))
}

func TestIsTunnelOverQuota_ExactlyAtThreshold(t *testing.T) {
	// 95% used — at the threshold. Must trigger to keep the buffer
	// meaningful (worker_cloud sync is cron-lagged, so 95% snapshot may
	// already be 100%+ in reality).
	inst := &CloudInstance{
		TrafficUsedBytes:  950 * 1024 * 1024 * 1024,
		TrafficTotalBytes: 1000 * 1024 * 1024 * 1024,
	}
	assert.True(t, isTunnelOverQuota(inst))
}

func TestIsTunnelOverQuota_Overused(t *testing.T) {
	// Real au-1 numbers from 2026-05-19: 1513 GB used / 1024 GB quota.
	// Every additional byte is overage billed by AWS.
	inst := &CloudInstance{
		TrafficUsedBytes:  1513 * 1024 * 1024 * 1024,
		TrafficTotalBytes: 1024 * 1024 * 1024 * 1024,
	}
	assert.True(t, isTunnelOverQuota(inst))
}

func TestShouldHideTunnelForUser_NonAdminOverQuota(t *testing.T) {
	// Non-admin user must not see an over-quota node.
	overUsed := &CloudInstance{
		TrafficUsedBytes:  1024 * 1024 * 1024 * 1024,
		TrafficTotalBytes: 1024 * 1024 * 1024 * 1024,
	}
	assert.True(t, shouldHideTunnelForUser(overUsed, false))
}

func TestShouldHideTunnelForUser_NonAdminUnderQuota(t *testing.T) {
	underUsed := &CloudInstance{
		TrafficUsedBytes:  100 * 1024 * 1024 * 1024,
		TrafficTotalBytes: 1024 * 1024 * 1024 * 1024,
	}
	assert.False(t, shouldHideTunnelForUser(underUsed, false))
}

func TestShouldHideTunnelForUser_NonAdminNonCloud(t *testing.T) {
	// Non-cloud nodes (nil instance) are never hidden — no quota to enforce.
	assert.False(t, shouldHideTunnelForUser(nil, false))
}

func TestShouldHideTunnelForUser_AdminOverQuota(t *testing.T) {
	// Admin sees over-quota nodes (debugging path stays intact).
	overUsed := &CloudInstance{
		TrafficUsedBytes:  1513 * 1024 * 1024 * 1024,
		TrafficTotalBytes: 1024 * 1024 * 1024 * 1024,
	}
	assert.False(t, shouldHideTunnelForUser(overUsed, true))
}

func TestShouldHideTunnelForUser_AdminUnderQuota(t *testing.T) {
	underUsed := &CloudInstance{
		TrafficUsedBytes:  100 * 1024 * 1024 * 1024,
		TrafficTotalBytes: 1024 * 1024 * 1024 * 1024,
	}
	assert.False(t, shouldHideTunnelForUser(underUsed, true))
}

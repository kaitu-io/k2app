package sidecar

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// netDev builds a /proc/net/dev body for one physical interface eth0 with the
// given rx/tx byte counters (other columns are filler the parser ignores).
func netDev(rx, tx uint64) string {
	return "Inter-|   Receive                                                |  Transmit\n" +
		" face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed\n" +
		"    lo:  100000     500    0    0    0     0          0         0   100000     500    0    0    0     0       0          0\n" +
		fmt.Sprintf("  eth0: %d    2000    0    0    0     0          0         0   %d    1500    0    0    0     0       0          0\n", rx, tx)
}

// newTestTM builds a TrafficMonitor wired to a fake proc root, bypassing
// NewTrafficMonitor's hostProcPath() so tests are hermetic.
func newTestTM(t *testing.T, rx, tx uint64, statePath string) *TrafficMonitor {
	t.Helper()
	root := writeProcNetDev(t, netDev(rx, tx))
	tm := &TrafficMonitor{
		billingStartDate:  "2025-01-01",
		billingCycleEndAt: 1782864000, // 2026-07-01: in the future so checkAndResetCycle is a no-op
		trafficLimitGB:    0,
		procPath:          root,
		statePath:         statePath,
		primaryInterface:  "eth0",
	}
	return tm
}

// TestUsedTrafficBytes_MaxDirection verifies usage is the HIGHER of inbound and
// outbound deltas (AWS bills the larger direction), NOT their sum.
func TestUsedTrafficBytes_MaxDirection(t *testing.T) {
	// baseline rx=1000, tx=1000; now rx=1000+300, tx=1000+900 → rxDelta=300, txDelta=900
	tm := newTestTM(t, 1300, 1900, filepath.Join(t.TempDir(), "s.state"))
	tm.cycleStartRx = 1000
	tm.cycleStartTx = 1000

	stats, err := tm.GetTrafficStats()
	require.NoError(t, err)
	assert.Equal(t, int64(900), stats.UsedTrafficBytes, "used = max(300,900), not sum 1200")
}

// TestSetUsage_AnchorsToGivenUsed verifies SetUsage records the declared usage as
// the cycle's prior-used floor and anchors the per-direction baseline at the
// current NIC (so the live delta starts at 0), and persists it so a restart keeps
// it.
func TestSetUsage_AnchorsToGivenUsed(t *testing.T) {
	const gib = 1024 * 1024 * 1024
	// node already pushed rx=300GiB, tx=920GiB on its NIC this cycle.
	statePath := filepath.Join(t.TempDir(), "s.state")
	tm := newTestTM(t, 300*gib, 920*gib, statePath)
	tm.billingCycleEndAt = 1782864000

	require.NoError(t, tm.SetUsage(920)) // declare "920 GiB already used"

	stats, err := tm.GetTrafficStats()
	require.NoError(t, err)
	assert.Equal(t, int64(920*gib), stats.UsedTrafficBytes, "meter reads the declared 920GiB right after SetUsage")

	// new model: prior holds the declared usage; baselines anchor at the CURRENT
	// NIC counters (live delta starts at 0), persisted for a clean restart.
	st := loadTrafficState(statePath)
	assert.Equal(t, int64(1782864000), st.BillingCycleEndAt)
	assert.Equal(t, uint64(920*gib), st.PriorUsedBytes)
	assert.Equal(t, uint64(300*gib), st.CycleStartRx)
	assert.Equal(t, uint64(920*gib), st.CycleStartTx)
}

// TestSetUsage_FreshNode_AboveNIC is the mid-cycle-join case the old baseline math
// could NOT express: declare usage far above what this node's NIC has ever seen.
// (A node created on the 23rd, NIC ~1GiB, must report "751GiB already used" so a
// full-month LIMIT=1000 caps the remaining month at ~250GiB.)
func TestSetUsage_FreshNode_AboveNIC(t *testing.T) {
	const gib = 1024 * 1024 * 1024
	statePath := filepath.Join(t.TempDir(), "s.state")
	tm := newTestTM(t, 1*gib, 1*gib, statePath) // fresh NIC: only ~1GiB lifetime
	tm.billingCycleEndAt = 1782864000

	require.NoError(t, tm.SetUsage(751)) // 751GiB ≫ 1GiB NIC counter

	stats, err := tm.GetTrafficStats()
	require.NoError(t, err)
	assert.Equal(t, int64(751*gib), stats.UsedTrafficBytes, "meter reports the declared 751GiB even though the NIC only saw 1GiB")

	st := loadTrafficState(statePath)
	assert.Equal(t, uint64(751*gib), st.PriorUsedBytes)
	assert.Equal(t, uint64(1*gib), st.CycleStartRx) // anchored at NIC, NOT clamped to 0
	assert.Equal(t, uint64(1*gib), st.CycleStartTx)
}

// TestUsedTrafficBytes_PriorPlusDelta verifies live traffic accrues ON TOP of the
// prior-used floor: used = priorUsedBytes + max(rxΔ, txΔ).
func TestUsedTrafficBytes_PriorPlusDelta(t *testing.T) {
	const gib = 1024 * 1024 * 1024
	tm := newTestTM(t, 100*gib+500, 100*gib+200, filepath.Join(t.TempDir(), "s.state"))
	tm.cycleStartRx = 100 * gib
	tm.cycleStartTx = 100 * gib
	tm.priorUsedBytes = 700 * gib

	stats, err := tm.GetTrafficStats()
	require.NoError(t, err)
	// delta = max(500,200)=500 → used = 700GiB + 500
	assert.Equal(t, int64(700*gib+500), stats.UsedTrafficBytes)
}

// TestCycleReset_ClearsPriorUsed verifies a billing-cycle rollover zeroes the
// prior-used floor and re-anchors to the current NIC, so the next month starts at
// 0 (the onboarding seed is never carried forward).
func TestCycleReset_ClearsPriorUsed(t *testing.T) {
	const gib = 1024 * 1024 * 1024
	statePath := filepath.Join(t.TempDir(), "s.state")
	tm := newTestTM(t, 5*gib, 5*gib, statePath)
	tm.priorUsedBytes = 700 * gib // seeded last cycle
	tm.cycleStartRx = 5 * gib
	tm.cycleStartTx = 5 * gib
	tm.billingCycleEndAt = 1 // already ended → next stats read forces a reset

	stats, err := tm.GetTrafficStats()
	require.NoError(t, err)
	assert.Equal(t, int64(0), stats.UsedTrafficBytes, "new cycle resets used to 0")
	assert.Equal(t, uint64(0), tm.priorUsedBytes, "prior seed cleared on reset")

	st := loadTrafficState(statePath)
	assert.Equal(t, uint64(0), st.PriorUsedBytes)
}

// TestTrafficState_RoundTrip_PerDirection verifies the per-direction baseline
// round-trips through the state file.
func TestTrafficState_RoundTrip_PerDirection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "traffic.state")
	require.NoError(t, saveTrafficState(path, trafficState{BillingCycleEndAt: 1700000000, CycleStartRx: 111, CycleStartTx: 222}))
	st := loadTrafficState(path)
	assert.Equal(t, int64(1700000000), st.BillingCycleEndAt)
	assert.Equal(t, uint64(111), st.CycleStartRx)
	assert.Equal(t, uint64(222), st.CycleStartTx)
}

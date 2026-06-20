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

// TestSetUsage_AnchorsToGivenUsed verifies SetUsage rewrites the baseline so the
// meter immediately reports the requested used value (mid-cycle onboarding), and
// persists it so a restart keeps it.
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

	// persisted: a fresh load gets the same baselines (survives restart, no reset)
	st := loadTrafficState(statePath)
	assert.Equal(t, int64(1782864000), st.BillingCycleEndAt)
	assert.Equal(t, uint64(920*gib-920*gib), st.CycleStartTx) // tx 920GiB - 920GiB declared = 0
	assert.Equal(t, uint64(0), st.CycleStartRx)               // rx 300GiB < 920GiB → clamp 0
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

package sidecar

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeBillingMode(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", BillingModeMax},    // legacy default: non-AWS fleet nodes set no mode
		{"max", BillingModeMax},
		{"MAX", BillingModeMax},
		{"sum", BillingModeSum},
		{" Sum ", BillingModeSum},
		{"bogus", BillingModeSum}, // typo'd config falls fail-closed (over-count, never under)
	}
	for _, c := range cases {
		assert.Equal(t, c.want, normalizeBillingMode(c.in), "input %q", c.in)
	}
}

// TestUsedTrafficBytes_SumMode: with billingMode=sum the deltas ADD (AWS
// Lightsail counts both directions against the allowance). Same counters as
// TestUsedTrafficBytes_MaxDirection, different mode → different answer.
func TestUsedTrafficBytes_SumMode(t *testing.T) {
	tm := newTestTM(t, 1300, 1900, filepath.Join(t.TempDir(), "s.state"))
	tm.billingMode = BillingModeSum
	tm.cycleStartRx = 1000
	tm.cycleStartTx = 1000

	stats, err := tm.GetTrafficStats()
	require.NoError(t, err)
	assert.Equal(t, int64(1200), stats.UsedTrafficBytes, "sum mode: 300+900, not max 900")
}

// TestCounterResetFold: a VM reboot zeroes the kernel NIC counters; the meter
// must fold the last known usage into the prior floor instead of silently
// dropping the cycle's usage (delta would clamp to 0 against the old baseline).
func TestCounterResetFold(t *testing.T) {
	const gib = 1024 * 1024 * 1024
	statePath := filepath.Join(t.TempDir(), "s.state")
	// live counters rx=5, tx=7 — far BELOW the pre-reboot baseline of 100GiB
	tm := newTestTM(t, 5, 7, statePath)
	tm.billingMode = BillingModeSum
	tm.cycleStartRx = 100 * gib
	tm.cycleStartTx = 100 * gib
	tm.lastUsedBytes = 40 * gib // last persisted computed usage before the reboot

	stats, err := tm.GetTrafficStats()
	require.NoError(t, err)
	// folded prior 40GiB + fresh deltas (rx 5 + tx 7 from the new anchor... the
	// fold anchors AT the live counters, so deltas are 0 on this same read)
	assert.Equal(t, int64(40*gib), stats.UsedTrafficBytes, "pre-reboot usage survives the counter reset")
	assert.Equal(t, uint64(40*gib), tm.priorUsedBytes)
	assert.Equal(t, uint64(5), tm.cycleStartRx, "re-anchored at live counters")
	assert.Equal(t, uint64(7), tm.cycleStartTx)

	st := loadTrafficState(statePath)
	assert.Equal(t, uint64(40*gib), st.PriorUsedBytes, "fold persisted")
}

// TestGetTrafficStats_PersistsLastUsed: each successful read snapshots the
// computed usage into the state file (rate-limited; the first read always
// persists because lastPersistAt starts at zero).
func TestGetTrafficStats_PersistsLastUsed(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "s.state")
	tm := newTestTM(t, 1300, 1900, statePath)
	tm.cycleStartRx = 1000
	tm.cycleStartTx = 1000

	_, err := tm.GetTrafficStats()
	require.NoError(t, err)
	st := loadTrafficState(statePath)
	assert.Equal(t, uint64(900), st.LastUsedBytes, "max(300,900) snapshotted")

	// Within the rate-limit window a second read must NOT rewrite the file.
	require.NoError(t, saveTrafficState(statePath, trafficState{BillingCycleEndAt: 42}))
	_, err = tm.GetTrafficStats()
	require.NoError(t, err)
	assert.Equal(t, int64(42), loadTrafficState(statePath).BillingCycleEndAt,
		"state file untouched inside the persist interval")
}

func TestAdoptAuthoritativeUsed_Ratchet(t *testing.T) {
	const gib = 1024 * 1024 * 1024
	statePath := filepath.Join(t.TempDir(), "s.state")
	tm := newTestTM(t, 10*gib, 12*gib, statePath)
	tm.billingMode = BillingModeSum
	tm.cycleStartRx = 0
	tm.cycleStartTx = 0
	// local meter reads 10+12 = 22GiB

	// Raise: provider says 30GiB > local 22GiB → adopt.
	adopted, err := tm.AdoptAuthoritativeUsed(30*gib, futureCycleEnd)
	require.NoError(t, err)
	assert.True(t, adopted)
	stats, err := tm.GetTrafficStats()
	require.NoError(t, err)
	assert.Equal(t, int64(30*gib), stats.UsedTrafficBytes, "meter ratcheted up to the provider figure")
	assert.Equal(t, uint64(30*gib), loadTrafficState(statePath).PriorUsedBytes, "adoption persisted")

	// Never lower: provider figure below the (now raised) local meter → no-op.
	adopted, err = tm.AdoptAuthoritativeUsed(25*gib, futureCycleEnd)
	require.NoError(t, err)
	assert.False(t, adopted)
	stats, _ = tm.GetTrafficStats()
	assert.Equal(t, int64(30*gib), stats.UsedTrafficBytes, "ratchet is one-way")

	// Epoch mismatch (cycle rolled over between report and response) → ignored.
	adopted, err = tm.AdoptAuthoritativeUsed(500*gib, futureCycleEnd-1)
	require.NoError(t, err)
	assert.False(t, adopted)
	stats, _ = tm.GetTrafficStats()
	assert.Equal(t, int64(30*gib), stats.UsedTrafficBytes, "stale-epoch correction never applies")

	// Zero / negative figures are ignored outright.
	adopted, err = tm.AdoptAuthoritativeUsed(0, futureCycleEnd)
	require.NoError(t, err)
	assert.False(t, adopted)
}

package sidecar

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTrafficState_RoundTrip verifies the cycle baseline persists and restores
// (so a restart does NOT reset in-cycle usage to 0).
func TestTrafficState_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "traffic.state")

	require.NoError(t, saveTrafficState(path, trafficState{BillingCycleEndAt: 1700000000, CycleStartRx: 12345, CycleStartTx: 67890, PriorUsedBytes: 999}))
	st := loadTrafficState(path)
	assert.Equal(t, int64(1700000000), st.BillingCycleEndAt)
	assert.Equal(t, uint64(12345), st.CycleStartRx)
	assert.Equal(t, uint64(67890), st.CycleStartTx)
	assert.Equal(t, uint64(999), st.PriorUsedBytes)

	// legacy state file (no prior_used_bytes) → field defaults to 0 (old behavior)
	require.NoError(t, os.WriteFile(path, []byte(`{"billing_cycle_end_at":1700000000,"cycle_start_rx":1,"cycle_start_tx":2}`), 0o600))
	legacy := loadTrafficState(path)
	assert.Equal(t, uint64(0), legacy.PriorUsedBytes)

	// missing file → zero value, no panic
	assert.Equal(t, trafficState{}, loadTrafficState(filepath.Join(dir, "nope.state")))
	_ = os.Remove
}

// TestTrafficStats_FailClosedOnNoInterface verifies a NIC read failure propagates
// as an error and never returns a silent zero-usage success.
func TestTrafficStats_FailClosedOnNoInterface(t *testing.T) {
	tm := &TrafficMonitor{billingStartDate: "2025-01-01", primaryInterface: "doesnotexist0", procPath: t.TempDir()}
	// procPath has no net/dev → readInterfaceBytes errors → GetTrafficStats errors
	_, err := tm.GetTrafficStats()
	assert.Error(t, err, "no readable host NIC must error, never silently return 0")
}

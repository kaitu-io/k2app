package sidecar

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// fakeDocker records pause/unpause and simulates existence + paused state.
type fakeDocker struct {
	mu     sync.Mutex
	paused map[string]bool
	exists map[string]bool
	pauseN int
	unpauN int
}

func newFakeDocker(names ...string) *fakeDocker {
	d := &fakeDocker{paused: map[string]bool{}, exists: map[string]bool{}}
	for _, n := range names {
		d.exists[n] = true
	}
	return d
}
func (d *fakeDocker) State(_ context.Context, n string) (bool, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.paused[n], d.exists[n], nil
}
func (d *fakeDocker) Pause(_ context.Context, n string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.paused[n] = true
	d.pauseN++
	return nil
}
func (d *fakeDocker) Unpause(_ context.Context, n string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.paused[n] = false
	d.unpauN++
	return nil
}
func (d *fakeDocker) isPaused(n string) bool { d.mu.Lock(); defer d.mu.Unlock(); return d.paused[n] }
func (d *fakeDocker) setPaused(n string, p bool) { d.mu.Lock(); defer d.mu.Unlock(); d.paused[n] = p }

func newTestEnforcer(t *testing.T, src statsSource, d dockerController) *enforcer {
	t.Helper()
	return newEnforcerFromStats(src, d, filepath.Join(t.TempDir(), "cutoff.state"), []string{"k2s"}, time.Second)
}

func TestEnforcer_CutsAtReserve(t *testing.T) {
	src := &fakeStats{stats: TrafficStats{
		MonthlyTrafficLimitBytes: 2 << 40,
		UsedTrafficBytes:         (2 << 40) - quotaCutoffReserveBytes, // exactly at threshold
	}}
	d := newFakeDocker("k2s")
	e := newTestEnforcer(t, src, d)
	e.reconcileOnce()
	assert.True(t, d.isPaused("k2s"), "used >= limit - reserve 掐断")
}

func TestEnforcer_UnlimitedNeverCuts(t *testing.T) {
	src := &fakeStats{stats: TrafficStats{MonthlyTrafficLimitBytes: 0, UsedTrafficBytes: 9 << 40}}
	d := newFakeDocker("k2s")
	e := newTestEnforcer(t, src, d)
	e.reconcileOnce()
	assert.False(t, d.isPaused("k2s"), "无限额永不掐")
	assert.Equal(t, 0, d.pauseN)
}

func TestEnforcer_UnderReserveNoCut(t *testing.T) {
	src := &fakeStats{stats: TrafficStats{MonthlyTrafficLimitBytes: 2 << 40, UsedTrafficBytes: 1 << 30}}
	d := newFakeDocker("k2s")
	e := newTestEnforcer(t, src, d)
	e.reconcileOnce()
	assert.False(t, d.isPaused("k2s"), "远低于阈值不掐")
}

func TestEnforcer_FailClosedAfter3MeterErrors(t *testing.T) {
	src := &fakeStats{stats: TrafficStats{MonthlyTrafficLimitBytes: 2 << 40, UsedTrafficBytes: 1 << 30}}
	d := newFakeDocker("k2s")
	e := newTestEnforcer(t, src, d)

	// First a SUCCESS read so lastLimit > 0 (there is a quota to protect).
	e.reconcileOnce()
	assert.False(t, d.isPaused("k2s"))

	// Flip to meter errors: 1st + 2nd error → not yet cut.
	src.set(TrafficStats{}, errMeter)
	e.reconcileOnce()
	assert.False(t, d.isPaused("k2s"), "1 次 meter error 不掐")
	e.reconcileOnce()
	assert.False(t, d.isPaused("k2s"), "2 次 meter error 不掐")

	// 3rd consecutive error → fail-closed cut.
	e.reconcileOnce()
	assert.True(t, d.isPaused("k2s"), "3 次连续 meter error → fail-closed 掐")

	// Good read under limit → recover.
	src.set(TrafficStats{MonthlyTrafficLimitBytes: 2 << 40, UsedTrafficBytes: 1 << 30}, nil)
	e.reconcileOnce()
	assert.False(t, d.isPaused("k2s"), "恢复读数后解除")
}

func TestEnforcer_UnlimitedMeterErrorNeverCuts(t *testing.T) {
	// Limit 0 seen once (lastLimit stays 0), then errors → never cut.
	src := &fakeStats{stats: TrafficStats{MonthlyTrafficLimitBytes: 0, UsedTrafficBytes: 1 << 30}}
	d := newFakeDocker("k2s")
	e := newTestEnforcer(t, src, d)
	e.reconcileOnce()
	src.set(TrafficStats{}, errMeter)
	for i := 0; i < 5; i++ {
		e.reconcileOnce()
	}
	assert.False(t, d.isPaused("k2s"), "无限额节点 meter error 也无可掐")
}

func TestEnforcer_SelfHealsResurrectedContainer(t *testing.T) {
	src := &fakeStats{stats: TrafficStats{MonthlyTrafficLimitBytes: 2 << 40, UsedTrafficBytes: 2 << 40}}
	d := newFakeDocker("k2s")
	e := newTestEnforcer(t, src, d)
	e.reconcileOnce()
	assert.True(t, d.isPaused("k2s"))
	d.setPaused("k2s", false) // docker 守护重启复活
	e.reconcileOnce()
	assert.True(t, d.isPaused("k2s"), "下一 tick 自愈重掐")
}

func TestEnforcer_RestartReappliesPersistedCut(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cutoff.state")
	assert.NoError(t, saveCutoffState(path, cutoffState{Cut: true}))
	// Meter errors so the enforcer can't immediately recompute a clear; with
	// meterFails < threshold the persisted cut (desired = e.cut = true) holds.
	src := &fakeStats{err: errMeter}
	d := newFakeDocker("k2s")
	e := newEnforcerFromStats(src, d, path, []string{"k2s"}, time.Second)
	e.reconcileOnce()
	assert.True(t, d.isPaused("k2s"), "重启读 state,meter 暂不可用也保持掐断")
}

// reconcileOnce must be race-free under concurrent calls (no lock-order issues).
func TestEnforcer_ConcurrentReconcileNoRace(t *testing.T) {
	src := &fakeStats{stats: TrafficStats{MonthlyTrafficLimitBytes: 1000, UsedTrafficBytes: 100}}
	d := newFakeDocker("k2s")
	e := newTestEnforcer(t, src, d)
	done := make(chan struct{})
	go func() {
		for i := 0; i < 2000; i++ {
			src.set(TrafficStats{MonthlyTrafficLimitBytes: 1000, UsedTrafficBytes: int64(i % 1500)}, nil)
		}
		close(done)
	}()
	for i := 0; i < 2000; i++ {
		e.reconcileOnce()
	}
	<-done
}

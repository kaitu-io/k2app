package sidecar

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// set is a thread-safe setter for the shared fakeMeter (defined in usage_reporter_test.go).
func (f *fakeMeter) set(v int64) { f.mu.Lock(); defer f.mu.Unlock(); f.value = v }

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

func newTestEnforcer(t *testing.T, m nicMeter, d dockerController) *enforcer {
	t.Helper()
	return newEnforcer(m, d, filepath.Join(t.TempDir(), "cutoff.state"), []string{"k2s"}, time.Second)
}

func TestEnforcer_CutsAtQuota(t *testing.T) {
	m := &fakeMeter{}
	d := newFakeDocker("k2s")
	e := newTestEnforcer(t, m, d)
	e.SetQuota(1, 1000, 0)
	m.set(999)
	e.reconcile(context.Background())
	assert.False(t, d.isPaused("k2s"), "999<1000 不掐")
	m.set(1000)
	e.reconcile(context.Background())
	assert.True(t, d.isPaused("k2s"), "到 1000 掐")
}

func TestEnforcer_AnchorsToCenterUsed(t *testing.T) {
	m := &fakeMeter{}
	d := newFakeDocker("k2s")
	e := newTestEnforcer(t, m, d)
	m.set(0)
	e.SetQuota(1, 1000, 950) // SetQuota 内部快照 meter=0
	m.set(51)
	e.reconcile(context.Background())
	assert.True(t, d.isPaused("k2s"), "950+51>=1000 掐")
}

func TestEnforcer_UncutsOnNewEpoch(t *testing.T) {
	m := &fakeMeter{}
	d := newFakeDocker("k2s")
	e := newTestEnforcer(t, m, d)
	e.SetQuota(1, 1000, 0)
	m.set(1000)
	e.reconcile(context.Background())
	assert.True(t, d.isPaused("k2s"))
	m.set(0)
	e.SetQuota(2, 1000, 0)
	e.reconcile(context.Background())
	assert.False(t, d.isPaused("k2s"), "新周期自动恢复")
}

func TestEnforcer_NoQuotaIsNoop(t *testing.T) {
	m := &fakeMeter{}
	d := newFakeDocker("k2s")
	e := newTestEnforcer(t, m, d)
	m.set(999999)
	e.reconcile(context.Background())
	assert.False(t, d.isPaused("k2s"), "无配额不掐")
	assert.Equal(t, 0, d.pauseN)
}

func TestEnforcer_RestartReappliesPersistedCut(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cutoff.state")
	assert.NoError(t, saveCutoffState(path, cutoffState{EpochID: 5, Cut: true}))
	m := &fakeMeter{}
	d := newFakeDocker("k2s")
	e := newEnforcer(m, d, path, []string{"k2s"}, time.Second)
	e.reconcile(context.Background())
	assert.True(t, d.isPaused("k2s"), "重启读 state,无配额也保持掐断")
}

func TestEnforcer_SelfHealsResurrectedContainer(t *testing.T) {
	m := &fakeMeter{}
	d := newFakeDocker("k2s")
	e := newTestEnforcer(t, m, d)
	e.SetQuota(1, 1000, 0)
	m.set(1000)
	e.reconcile(context.Background())
	assert.True(t, d.isPaused("k2s"))
	d.setPaused("k2s", false) // docker 守护重启复活
	e.reconcile(context.Background())
	assert.True(t, d.isPaused("k2s"), "下一 tick 自愈重掐")
}

// 修复 #2:epoch 边界 meter 已 rebaseline(归零)但 SetQuota 未到时,anchor 陈旧,
// 必须保持当前状态——绝不在陈旧 anchor 上把一条干净线误掐。
func TestEnforcer_HoldsOnStaleAnchorNoFalseCut(t *testing.T) {
	m := &fakeMeter{}
	d := newFakeDocker("k2s")
	e := newTestEnforcer(t, m, d)
	m.set(500)
	e.SetQuota(1, 1000, 500) // 50%,未掐;meterAtReport=500
	e.reconcile(context.Background())
	assert.False(t, d.isPaused("k2s"), "50% 不掐")
	m.set(0) // epoch 翻转:reporter 已 Rebaseline,SetQuota 尚未到
	e.reconcile(context.Background())
	assert.False(t, d.isPaused("k2s"), "陈旧 anchor 不得误掐干净线")
	e.SetQuota(2, 1000, 0) // 新 epoch 配额到达
	e.reconcile(context.Background())
	assert.False(t, d.isPaused("k2s"), "新周期仍不掐")
}

// 修复 #1:SetQuota 与 reconcile 并发不得死锁/竞争(ABBA 锁序)。-race 下高频并发。
func TestEnforcer_ConcurrentSetQuotaReconcileNoDeadlock(t *testing.T) {
	m := &fakeMeter{value: 100}
	d := newFakeDocker("k2s")
	e := newTestEnforcer(t, m, d)
	done := make(chan struct{})
	go func() {
		for i := 0; i < 2000; i++ {
			e.SetQuota(int64(i%3), 1000, int64(i%1200))
		}
		close(done)
	}()
	for i := 0; i < 2000; i++ {
		m.set(int64(i % 1500))
		e.reconcile(context.Background())
	}
	<-done
}

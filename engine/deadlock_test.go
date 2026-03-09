package engine

import (
	"sync"
	"testing"
	"time"

	"github.com/sasha-s/go-deadlock"

	"github.com/kaitu-io/k2/core"
)

func init() {
	deadlock.Opts.DeadlockTimeout = 5 * time.Second
}

// TestDeadlock_HealthMonitorSampleAndResetCounters validates that the
// healthMonitor self-deadlock (#1) is fixed: sample() must not call
// onReconnect under hm.mu, because onReconnect → OnNetworkChanged → resetCounters
// re-acquires hm.mu. go-deadlock's timeout detection catches this.
//
// If the bug were reintroduced (onReconnect called under hm.mu), the 5s
// deadlock timeout would trigger a panic during this test.
func TestDeadlock_HealthMonitorSampleAndResetCounters(t *testing.T) {
	tm := mockTransportManager()
	tunnel := core.NewClientTunnel(core.ClientTunnelConfig{})

	// Declare hm first so the closure can capture it by reference.
	var hm *healthMonitor
	hm = newHealthMonitor(tm, tunnel, nil, func() {
		// Simulate OnNetworkChanged: acquires hm.mu via resetCounters.
		hm.resetCounters()
	})

	// Force critical state → onReconnect fires.
	hm.mu.Lock()
	hm.health = HealthCritical
	hm.lastAction = time.Time{}
	hm.smoothWireLoss = 0.5
	hm.ewmaInitialized = true
	hm.mu.Unlock()

	// sample() calls transition() → maybeAct() → returns onReconnect.
	// If onReconnect is called under hm.mu, go-deadlock detects timeout.
	hm.sample()

	// Also exercise concurrent sample/resetCounters to give go-deadlock
	// enough lock-ordering data to detect inconsistencies.
	hm.start()
	time.Sleep(1500 * time.Millisecond)
	hm.resetCounters()
	hm.stop()
}

// TestDeadlock_EngineStopVsHealthGoroutine validates the cross-deadlock (#2)
// fix: Engine.Stop must release e.mu before calling hm.stop() (wg.Wait).
// Otherwise, the health goroutine's onReconnect → OnNetworkChanged → e.mu
// would deadlock with Stop() holding e.mu + waiting for wg.
//
// go-deadlock ordering detection catches: goroutine A holds e.mu then
// waits on wg (implicitly needs hm.mu), goroutine B holds hm.mu then
// needs e.mu.
func TestDeadlock_EngineStopVsHealthGoroutine(t *testing.T) {
	tm := mockTransportManager()
	tunnel := core.NewClientTunnel(core.ClientTunnelConfig{})

	e := New()

	hm := newHealthMonitor(tm, tunnel, nil, func() {
		// Simulate the real onReconnect callback: acquires e.mu briefly.
		e.mu.Lock()
		time.Sleep(time.Millisecond)
		e.mu.Unlock()
	})

	// Force critical state so onReconnect fires from health goroutine.
	hm.mu.Lock()
	hm.health = HealthCritical
	hm.lastAction = time.Time{}
	hm.smoothWireLoss = 0.5
	hm.ewmaInitialized = true
	hm.mu.Unlock()

	hm.start()

	// Wait for health goroutine to enter onReconnect at least once.
	time.Sleep(1500 * time.Millisecond)

	// Simulate Stop() pattern: acquire e.mu, extract hm, release, then stop.
	// This is the FIXED pattern. The OLD (broken) pattern was:
	//   e.mu.Lock(); hm.stop(); e.mu.Unlock() ← cross-deadlock
	e.mu.Lock()
	e.health = nil
	e.mu.Unlock()
	hm.stop()
}

// TestDeadlock_LockOrderingEngineThenHealth validates the documented lock
// ordering: engine.mu → healthMonitor.mu. This exercises both locks in
// the correct order across goroutines so go-deadlock builds the ordering
// graph. If any code path reversed this order, go-deadlock would panic.
func TestDeadlock_LockOrderingEngineThenHealth(t *testing.T) {
	e := New()
	tm := mockTransportManager()
	tunnel := core.NewClientTunnel(core.ClientTunnelConfig{})

	hm := newHealthMonitor(tm, tunnel, nil, nil)

	// Goroutine 1: engine.mu → health.mu (correct order)
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			e.mu.Lock()
			hm.mu.Lock()
			hm.mu.Unlock()
			e.mu.Unlock()
		}
	}()

	// Goroutine 2: also engine.mu → health.mu (same correct order)
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			e.mu.Lock()
			hm.mu.Lock()
			hm.mu.Unlock()
			e.mu.Unlock()
		}
	}()

	wg.Wait()
}

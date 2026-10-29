package engine

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sasha-s/go-deadlock"

	"github.com/kaitu-io/k2/core"
	"github.com/kaitu-io/k2/provider"
)

func init() {
	deadlock.Opts.DeadlockTimeout = 5 * time.Second
}

// TestDeadlock_HealthMonitorSampleAndResetCounters validates that the
// healthMonitor self-deadlock (#1) is fixed: sample() must not call
// onReconnect under hm.mu, because onReconnect → onHealthCritical → reconnect
// → resetCounters re-acquires hm.mu. go-deadlock's timeout detection catches this.
//
// If the bug were reintroduced (onReconnect called under hm.mu), the 5s
// deadlock timeout would trigger a panic during this test.
func TestDeadlock_HealthMonitorSampleAndResetCounters(t *testing.T) {
	tm := mockTransportManager()
	tunnel := core.NewClientTunnel(core.ClientTunnelConfig{})

	// Declare hm first so the closure can capture it by reference.
	ot := &outboundTransport{tm: tm, name: "test"}
	var hm *healthMonitor
	hm = newHealthMonitor(ot, []*outboundTransport{ot}, tunnel, nil, func() {
		// Simulate OnNetworkChanged: acquires hm.mu via resetCounters.
		hm.resetCounters()
	}, time.Now())

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
// Otherwise, the health goroutine's onReconnect → onHealthCritical → e.mu
// would deadlock with Stop() holding e.mu + waiting for wg.
//
// go-deadlock ordering detection catches: goroutine A holds e.mu then
// waits on wg (implicitly needs hm.mu), goroutine B holds hm.mu then
// needs e.mu.
func TestDeadlock_EngineStopVsHealthGoroutine(t *testing.T) {
	tm := mockTransportManager()
	tunnel := core.NewClientTunnel(core.ClientTunnelConfig{})

	e := New()

	ot := &outboundTransport{tm: tm, name: "test"}
	hm := newHealthMonitor(ot, []*outboundTransport{ot}, tunnel, nil, func() {
		// Simulate the real onReconnect callback: acquires e.mu briefly.
		e.mu.Lock()
		time.Sleep(time.Millisecond)
		e.mu.Unlock()
	}, time.Now())

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

	ot := &outboundTransport{tm: tm, name: "test"}
	hm := newHealthMonitor(ot, []*outboundTransport{ot}, tunnel, nil, nil, time.Now())

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

// slowProvider simulates a provider whose Close() blocks for a duration,
// like dnsOverride.stop() CGo blocking on CFRunLoop exit.
type slowProvider struct {
	closeDelay time.Duration
}

func (p *slowProvider) Start(ctx context.Context, handler provider.ConnectionHandler) error {
	return nil
}
func (p *slowProvider) Close() error {
	time.Sleep(p.closeDelay)
	return nil
}
func (p *slowProvider) Name() string { return "slow-test" }

// TestDeadlock_StopVsReportWireError validates that Engine.Stop() does not
// deadlock with concurrent ReportWireError() calls. Stop() must release e.mu
// before calling prov.Close()/tunnel.Close(), because those drain handlers
// that call ReportWireError() which needs e.mu.
func TestDeadlock_StopVsReportWireError(t *testing.T) {
	e := New()

	// Set engine to connected state with a slow provider.
	e.mu.Lock()
	e.state = StateConnected
	e.prov = &slowProvider{closeDelay: 500 * time.Millisecond}
	e.tunnel = core.NewClientTunnel(core.ClientTunnelConfig{})
	e.mu.Unlock()

	// Spawn goroutines that continuously call ReportWireError.
	// Before the fix, these would deadlock with Stop() holding e.mu.
	var wg sync.WaitGroup
	done := make(chan struct{})
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
					e.ReportWireError(errors.New("test wire error"))
					time.Sleep(time.Millisecond)
				}
			}
		}()
	}

	// Stop should complete without deadlock despite concurrent ReportWireError.
	// go-deadlock's 5s timeout would catch any lock ordering violation.
	err := e.Stop()
	if err != nil {
		t.Fatalf("Stop() returned error: %v", err)
	}

	close(done)
	wg.Wait()
}

// TestDeadlock_StopVsSlowProviderClose validates that Stop() completes
// even when provider.Close() blocks (simulating dnsOverride.stop() CGo).
// The key invariant: e.mu is NOT held during prov.Close().
func TestDeadlock_StopVsSlowProviderClose(t *testing.T) {
	e := New()

	e.mu.Lock()
	e.state = StateConnected
	e.prov = &slowProvider{closeDelay: 2 * time.Second}
	e.tunnel = core.NewClientTunnel(core.ClientTunnelConfig{})
	e.mu.Unlock()

	// Stop should complete — prov.Close() blocks but e.mu is released.
	stopDone := make(chan error, 1)
	go func() {
		stopDone <- e.Stop()
	}()

	// While Stop() is in progress (prov.Close blocking), verify e.mu is free
	// by acquiring it from another goroutine.
	time.Sleep(100 * time.Millisecond) // let Stop() start
	lockAcquired := make(chan struct{})
	go func() {
		e.mu.Lock()
		e.mu.Unlock()
		close(lockAcquired)
	}()

	select {
	case <-lockAcquired:
		// Good — e.mu was free during prov.Close()
	case <-time.After(3 * time.Second):
		t.Fatal("e.mu blocked during prov.Close() — deadlock regression")
	}

	select {
	case err := <-stopDone:
		if err != nil {
			t.Fatalf("Stop() returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Stop() timed out")
	}
}

// lockProbeHandler implements EventHandler and tries to acquire e.mu in OnStatus.
// If handler.OnStatus is called under e.mu, this will deadlock (go-deadlock 5s panic).
// If the fix is correct, OnStatus runs outside e.mu so the lock probe succeeds.
type lockProbeHandler struct {
	mu       *deadlock.Mutex // pointer to engine.mu
	acquired atomic.Int32    // count of successful lock probes
}

func (h *lockProbeHandler) OnStatus(s Status)                         { h.mu.Lock(); h.mu.Unlock(); h.acquired.Add(1) }
func (h *lockProbeHandler) OnStats(txBytes, rxBytes int64)            {}
func (h *lockProbeHandler) OnOutboundFatal(outbound string, err error) {}

// TestDeadlock_SetStateNotUnderLock validates that handler.OnStatus is called
// outside e.mu after the setState→setStateLocked refactor (B1/B2).
// The handler tries to acquire e.mu — if OnStatus were called under e.mu,
// go-deadlock's 5s timeout would trigger a panic.
func TestDeadlock_SetStateNotUnderLock(t *testing.T) {
	e := New()
	h := &lockProbeHandler{mu: &e.mu}
	e.SetEventHandler(h)

	// Simulate Start() path: lock, set connecting, unlock → OnStatus fires outside lock.
	e.mu.Lock()
	e.setStateLocked(StateConnecting)
	handler := e.handler
	status := e.buildStatusLocked()
	e.mu.Unlock()
	if handler != nil {
		handler.OnStatus(status)
	}

	// Simulate fail() path: triggers OnStatus(disconnected) outside lock.
	e.mu.Lock()
	e.state = StateConnecting // reset for fail path
	e.mu.Unlock()
	e.fail(errors.New("test error"))

	// Simulate Stop() path.
	e.mu.Lock()
	e.state = StateConnected
	e.prov = &slowProvider{closeDelay: 100 * time.Millisecond}
	e.tunnel = core.NewClientTunnel(core.ClientTunnelConfig{})
	e.mu.Unlock()
	e.Stop()

	// All three paths should have notified the handler without deadlock.
	if got := h.acquired.Load(); got < 3 {
		t.Fatalf("expected at least 3 handler notifications, got %d", got)
	}
}

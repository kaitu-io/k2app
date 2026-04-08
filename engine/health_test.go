package engine

import (
	"sync"
	"testing"
	"time"

	"github.com/kaitu-io/k2/core"
	"github.com/kaitu-io/k2/wire"
)

// mockTransportManager returns a TransportManager with no transports set.
// WireStats() returns zero values; ResetConnections() is a no-op.
func mockTransportManager() *wire.TransportManager {
	return wire.NewTransportManager()
}

func TestHealthMonitor_StartStop(t *testing.T) {
	tm := mockTransportManager()
	tunnel := core.NewClientTunnel(core.ClientTunnelConfig{})

	var count int
	ot := &outboundTransport{tm: tm, name: "test"}
	hm := newHealthMonitor(ot, []*outboundTransport{ot}, tunnel, func(s HealthSnapshot) { count++ }, nil, time.Now())
	hm.start()
	time.Sleep(2500 * time.Millisecond)
	hm.stop()

	if count < 2 {
		t.Fatalf("expected at least 2 snapshots, got %d", count)
	}
}

func TestHealthMonitor_HealthyByDefault(t *testing.T) {
	tm := mockTransportManager()
	tunnel := core.NewClientTunnel(core.ClientTunnelConfig{})

	var last HealthSnapshot
	ot := &outboundTransport{tm: tm, name: "test"}
	hm := newHealthMonitor(ot, []*outboundTransport{ot}, tunnel, func(s HealthSnapshot) { last = s }, nil, time.Now())
	hm.start()
	time.Sleep(1500 * time.Millisecond)
	hm.stop()

	if last.Health != HealthHealthy {
		t.Fatalf("expected healthy, got %s", last.Health)
	}
}

func TestHealthMonitor_HistoryOrder(t *testing.T) {
	tm := mockTransportManager()
	tunnel := core.NewClientTunnel(core.ClientTunnelConfig{})

	ot := &outboundTransport{tm: tm, name: "test"}
	hm := newHealthMonitor(ot, []*outboundTransport{ot}, tunnel, nil, nil, time.Now())
	hm.start()
	time.Sleep(3500 * time.Millisecond)
	hm.stop()

	history := hm.History()
	if len(history) < 3 {
		t.Fatalf("expected at least 3 history entries, got %d", len(history))
	}

	// Verify oldest-first ordering.
	for i := 1; i < len(history); i++ {
		if history[i].Timestamp.Before(history[i-1].Timestamp) {
			t.Fatalf("history not ordered: [%d]=%v > [%d]=%v",
				i-1, history[i-1].Timestamp, i, history[i].Timestamp)
		}
	}
}

func TestHealthTransitions(t *testing.T) {
	// Test the assess + transition logic directly.
	// assess() reads smoothWireLoss from hm, so we set it directly
	// to unit-test the decision logic independent of EWMA.
	tm := mockTransportManager()
	tunnel := core.NewClientTunnel(core.ClientTunnelConfig{})
	ot := &outboundTransport{tm: tm, name: "test"}
	hm := newHealthMonitor(ot, []*outboundTransport{ot}, tunnel, nil, nil, time.Now())

	// Start healthy.
	if hm.health != HealthHealthy {
		t.Fatalf("initial health: got %s, want healthy", hm.health)
	}

	// Simulate 5 consecutive high-loss samples → degraded.
	for i := 0; i < 5; i++ {
		hm.smoothWireLoss = 0.10
		level := hm.assess(wire.WireStats{Transport: "quic"})
		hm.transition(level, false)
	}
	if hm.health != HealthDegraded {
		t.Fatalf("after 5 high-loss: got %s, want degraded", hm.health)
	}

	// Simulate 5 consecutive very-high-loss → critical.
	// 4 consecutive should NOT trigger critical (filters MI boundary noise ~2-3s).
	for i := 0; i < 4; i++ {
		hm.smoothWireLoss = 0.30
		level := hm.assess(wire.WireStats{Transport: "quic"})
		hm.transition(level, false)
	}
	if hm.health == HealthCritical {
		t.Fatalf("after 4 very-high-loss: got critical too early (need 5)")
	}
	// 5th sample triggers critical.
	hm.smoothWireLoss = 0.30
	level := hm.assess(wire.WireStats{Transport: "quic"})
	hm.transition(level, false)
	if hm.health != HealthCritical {
		t.Fatalf("after 5 very-high-loss: got %s, want critical", hm.health)
	}

	// Simulate 10 consecutive healthy samples → healthy.
	for i := 0; i < 10; i++ {
		hm.smoothWireLoss = 0.01
		level := hm.assess(wire.WireStats{Transport: "quic"})
		hm.transition(level, false)
	}
	if hm.health != HealthHealthy {
		t.Fatalf("after 10 healthy: got %s, want healthy", hm.health)
	}
}

func TestHealthCooldown(t *testing.T) {
	tm := mockTransportManager()
	tunnel := core.NewClientTunnel(core.ClientTunnelConfig{})

	var actionCount int
	ot := &outboundTransport{tm: tm, name: "test"}
	hm := newHealthMonitor(ot, []*outboundTransport{ot}, tunnel, nil, func() { actionCount++ }, time.Now())

	// Force into critical state.
	hm.health = HealthCritical

	// First action should return onReconnect.
	action := hm.maybeAct(false)
	if action == nil {
		t.Fatal("expected action, got nil")
	}
	action()
	if actionCount != 1 {
		t.Fatalf("expected 1 reconnect, got %d", actionCount)
	}

	// Immediate retry should be blocked by cooldown.
	action = hm.maybeAct(false)
	if action != nil {
		t.Fatal("cooldown should block action")
	}

	// After cooldown expires, should fire again.
	hm.lastAction = time.Now().Add(-actionCooldown - time.Second)
	action = hm.maybeAct(false)
	if action == nil {
		t.Fatal("expected action after cooldown, got nil")
	}
	action()
	if actionCount != 2 {
		t.Fatalf("expected 2 reconnects, got %d", actionCount)
	}
}

func TestHealthTransitions_Fallback(t *testing.T) {
	tm := mockTransportManager()
	tunnel := core.NewClientTunnel(core.ClientTunnelConfig{})
	ot := &outboundTransport{tm: tm, name: "test"}
	hm := newHealthMonitor(ot, []*outboundTransport{ot}, tunnel, nil, nil, time.Now())

	// Fallback stats → assess returns degraded (not critical).
	for i := 0; i < 5; i++ {
		level := hm.assess(wire.WireStats{Transport: "tcpws", WireFallback: true})
		hm.transition(level, false)
	}
	if hm.health != HealthDegraded {
		t.Fatalf("fallback should be degraded, got %s", hm.health)
	}

	// Fallback recovery: QUIC comes back with good stats → healthy after 10 samples.
	for i := 0; i < 10; i++ {
		hm.smoothWireLoss = 0.01
		level := hm.assess(wire.WireStats{Transport: "quic"})
		hm.transition(level, false)
	}
	if hm.health != HealthHealthy {
		t.Fatalf("should be healthy after QUIC recovery, got %s", hm.health)
	}
}

func TestHealthEscalation(t *testing.T) {
	tm := mockTransportManager()
	tunnel := core.NewClientTunnel(core.ClientTunnelConfig{})

	var reconnectCount int
	ot := &outboundTransport{tm: tm, name: "test"}
	hm := newHealthMonitor(ot, []*outboundTransport{ot}, tunnel, nil, func() { reconnectCount++ }, time.Now())

	// Every maybeAct in critical state returns onReconnect (no hop escalation).
	hm.health = HealthCritical
	hm.lastAction = time.Time{} // bypass cooldown
	action := hm.maybeAct(false)
	if action != nil {
		action()
	}

	if reconnectCount != 1 {
		t.Fatalf("expected 1 reconnect, got %d", reconnectCount)
	}
}

func TestHealthMonitor_ResetCounters(t *testing.T) {
	tm := mockTransportManager()
	tunnel := core.NewClientTunnel(core.ClientTunnelConfig{})
	ot := &outboundTransport{tm: tm, name: "test"}
	hm := newHealthMonitor(ot, []*outboundTransport{ot}, tunnel, nil, nil, time.Now())

	// Drive into non-default state.
	hm.health = HealthCritical
	hm.degradedStreak = 5
	hm.criticalStreak = 3
	hm.healthyStreak = 0
	hm.smoothWireLoss = 0.5
	hm.ewmaInitialized = true

	hm.resetCounters()

	if hm.health != HealthHealthy {
		t.Fatalf("health = %s, want %s", hm.health, HealthHealthy)
	}
	if hm.degradedStreak != 0 || hm.criticalStreak != 0 || hm.healthyStreak != 0 {
		t.Fatalf("streaks not zeroed: degraded=%d critical=%d healthy=%d",
			hm.degradedStreak, hm.criticalStreak, hm.healthyStreak)
	}
	if hm.smoothWireLoss != 0 {
		t.Fatalf("smoothWireLoss = %f, want 0", hm.smoothWireLoss)
	}
	if hm.ewmaInitialized {
		t.Fatal("ewmaInitialized should be false after reset")
	}
}

// ============================================================================
// Tests for EWMA smoothing
// ============================================================================

func TestHealthMonitor_EWMASmoothing(t *testing.T) {
	tm := mockTransportManager()
	tunnel := core.NewClientTunnel(core.ClientTunnelConfig{})
	ot := &outboundTransport{tm: tm, name: "test"}
	hm := newHealthMonitor(ot, []*outboundTransport{ot}, tunnel, nil, nil, time.Now())

	// First sample initializes EWMA
	hm.smoothWireLoss = 0
	hm.ewmaInitialized = false

	// Simulate calling sample-like EWMA logic manually
	// (since sample() calls tm.WireStats() which returns zeros)
	ws := wire.WireStats{Transport: "quic", PacketLossRate: 0.0, Throughput: 100000}
	applyEWMA(hm, ws)

	if hm.smoothWireLoss != 0.0 {
		t.Errorf("First sample: smoothWireLoss should be 0.0, got %f", hm.smoothWireLoss)
	}

	// Single spike: lossRate=0.96 from 0.0 baseline
	ws2 := wire.WireStats{Transport: "quic", PacketLossRate: 0.96, Throughput: 50000}
	applyEWMA(hm, ws2)

	// EWMA α=0.3: smoothWireLoss = 0.3 * 0.96 + 0.7 * 0.0 = 0.288
	if hm.smoothWireLoss < 0.27 || hm.smoothWireLoss > 0.30 {
		t.Errorf("After spike: smoothWireLoss should be ~0.288, got %f", hm.smoothWireLoss)
	}

	// After spike ends: 5 samples of 0.0 loss → smoothWireLoss decays
	for i := 0; i < 5; i++ {
		applyEWMA(hm, wire.WireStats{Transport: "quic", PacketLossRate: 0.0, Throughput: 100000})
	}
	if hm.smoothWireLoss > 0.05 {
		t.Errorf("After decay: smoothWireLoss should be < 0.05, got %f", hm.smoothWireLoss)
	}
}

func TestHealthMonitor_EWMAPreventsFalseCritical(t *testing.T) {
	// After Layer 1 (ARC Snapshot fix), the health monitor sees moderate
	// residual noise (e.g., 0.15 loss) instead of extreme MI boundary spikes.
	// EWMA should prevent this moderate noise from escalating to CRITICAL.
	//
	// Without EWMA: 5 samples of 0.15 → degraded, then throughputDrop → CRITICAL
	// With EWMA: smoothWireLoss ramps slowly, never reaches veryHighLoss threshold
	tm := mockTransportManager()
	tunnel := core.NewClientTunnel(core.ClientTunnelConfig{})
	ot := &outboundTransport{tm: tm, name: "test"}
	hm := newHealthMonitor(ot, []*outboundTransport{ot}, tunnel, nil, nil, time.Now())

	// Initialize with healthy baseline
	hm.smoothWireLoss = 0.01
	hm.ewmaInitialized = true

	for i := 0; i < 10; i++ {
		level := hm.assess(wire.WireStats{Transport: "quic"})
		hm.transition(level, false)
	}
	if hm.health != HealthHealthy {
		t.Fatalf("Should be healthy after baseline, got %s", hm.health)
	}

	// Inject 5-second spike of moderate loss (0.15) — realistic post-Layer-1 noise
	for i := 0; i < 5; i++ {
		ws := wire.WireStats{Transport: "quic", PacketLossRate: 0.15, Throughput: 50000}
		applyEWMA(hm, ws)
		level := hm.assess(wire.WireStats{Transport: "quic"})
		hm.transition(level, false)
	}

	// smoothWireLoss after 5 samples: converges toward 0.15 but from 0.01 baseline:
	// ~0.052, ~0.081, ~0.102, ~0.116, ~0.126
	// All are highLoss (>0.05) but never veryHighLoss (>0.25)
	// So health becomes degraded at most, NEVER critical
	if hm.health == HealthCritical {
		t.Errorf("EWMA should prevent false CRITICAL from moderate 5-sample spike, health=%s, smoothWireLoss=%f",
			hm.health, hm.smoothWireLoss)
	}
	if hm.smoothWireLoss > 0.25 {
		t.Errorf("smoothWireLoss should be < 0.25 after 5 samples of 0.15 from baseline 0.01, got %f", hm.smoothWireLoss)
	}
}

// ============================================================================
// Tests for corrective actions
// ============================================================================

func TestHealthMonitor_CriticalTriggersReconnect(t *testing.T) {
	// When health goes critical, maybeAct should return onReconnect directly.
	// No port hop escalation — wire layer owns hop decisions.
	tm := mockTransportManager()
	tunnel := core.NewClientTunnel(core.ClientTunnelConfig{})

	var reconnected bool
	ot := &outboundTransport{tm: tm, name: "test"}
	hm := newHealthMonitor(ot, []*outboundTransport{ot}, tunnel, nil, func() { reconnected = true }, time.Now())
	hm.health = HealthCritical

	// First maybeAct should return onReconnect (not a port hop)
	action := hm.maybeAct(false)
	if action == nil {
		t.Fatal("expected reconnect action, got nil")
	}
	action()
	if !reconnected {
		t.Fatal("maybeAct should return onReconnect directly, not port hop")
	}
}

// ============================================================================
// Deadlock regression tests
// ============================================================================

func TestHealthMonitor_MaybeActReturnsAction(t *testing.T) {
	// Regression: maybeAct must return the action without executing it.
	// Before fix, maybeAct called onReconnect() inline while hm.mu was held,
	// causing self-deadlock when onReconnect → OnNetworkChanged → resetCounters → hm.mu.Lock().
	tm := mockTransportManager()
	tunnel := core.NewClientTunnel(core.ClientTunnelConfig{})

	var called bool
	ot := &outboundTransport{tm: tm, name: "test"}
	hm := newHealthMonitor(ot, []*outboundTransport{ot}, tunnel, nil, func() { called = true }, time.Now())
	hm.health = HealthCritical

	// maybeAct should return onReconnect without calling it.
	action := hm.maybeAct(false)
	if action == nil {
		t.Fatal("expected reconnect action")
	}
	if called {
		t.Fatal("onReconnect should not be called yet (deferred)")
	}

	// Execute the returned action.
	action()
	if !called {
		t.Fatal("onReconnect should be called after executing returned action")
	}
}

func TestHealthMonitor_TransitionReturnsDeferredAction(t *testing.T) {
	// Regression: transition must return the action, not execute it inline.
	tm := mockTransportManager()
	tunnel := core.NewClientTunnel(core.ClientTunnelConfig{})

	var called bool
	ot := &outboundTransport{tm: tm, name: "test"}
	hm := newHealthMonitor(ot, []*outboundTransport{ot}, tunnel, nil, func() { called = true }, time.Now())

	// Force critical state.
	hm.health = HealthCritical
	hm.lastAction = time.Time{} // bypass cooldown

	action := hm.transition(HealthCritical, false)
	if action == nil {
		t.Fatal("transition should return action for full reconnect")
	}
	if called {
		t.Fatal("onReconnect must not be called inside transition")
	}

	action()
	if !called {
		t.Fatal("onReconnect should fire when returned action is executed")
	}
}

func TestHealthMonitor_NoConcurrentDeadlock(t *testing.T) {
	// Regression: sample() + stop() must not deadlock.
	// Before fix, Stop() held e.mu and called hm.stop() (wg.Wait),
	// while sample()'s onReconnect callback needed e.mu → cross-deadlock.
	//
	// This test simulates the scenario: onReconnect acquires a shared mutex
	// (simulating e.mu), while stop() is called from a goroutine that
	// also holds that mutex.
	tm := mockTransportManager()
	tunnel := core.NewClientTunnel(core.ClientTunnelConfig{})

	var sharedMu sync.Mutex
	ot := &outboundTransport{tm: tm, name: "test"}
	hm := newHealthMonitor(ot, []*outboundTransport{ot}, tunnel, nil, func() {
		// Simulate OnNetworkChanged acquiring e.mu briefly.
		sharedMu.Lock()
		time.Sleep(time.Millisecond)
		sharedMu.Unlock()
	}, time.Now())

	// Force critical state so onReconnect will fire.
	hm.mu.Lock()
	hm.health = HealthCritical
	hm.lastAction = time.Time{}
	hm.smoothWireLoss = 0.5
	hm.ewmaInitialized = true
	hm.mu.Unlock()

	hm.start()

	// Deadline: if this test doesn't complete in 5s, it's a deadlock.
	done := make(chan struct{})
	go func() {
		// Wait for health goroutine to potentially enter onReconnect.
		time.Sleep(1500 * time.Millisecond)

		// Simulate Stop() pattern: acquire shared lock, then stop health.
		// Before fix, this would deadlock if health goroutine was inside onReconnect.
		sharedMu.Lock()
		hm.stop()
		sharedMu.Unlock()
		close(done)
	}()

	select {
	case <-done:
		// Success — no deadlock.
	case <-time.After(5 * time.Second):
		t.Fatal("deadlock: stop() blocked while health goroutine holds shared lock")
	}
}

func TestWakeDetector(t *testing.T) {
	wd := wakeDetector{}
	base := time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)

	// First call: initialize, no action.
	gap, reconnect := wd.checkWake(base, HealthHealthy, "quic", 100)
	if gap != 0 || reconnect {
		t.Fatalf("first call: got gap=%v reconnect=%v, want 0/false", gap, reconnect)
	}

	// Normal tick (1s gap): no action.
	gap, reconnect = wd.checkWake(base.Add(1*time.Second), HealthHealthy, "quic", 101)
	if gap != 1*time.Second || reconnect {
		t.Fatalf("normal tick: got gap=%v reconnect=%v, want 1s/false", gap, reconnect)
	}

	// Short sleep (10s): DIAG log emitted but no reconnect.
	gap, reconnect = wd.checkWake(base.Add(11*time.Second), HealthHealthy, "quic", 111)
	if gap != 10*time.Second || reconnect {
		t.Fatalf("short sleep: got gap=%v reconnect=%v, want 10s/false", gap, reconnect)
	}

	// Below boundary (29s): no reconnect.
	gap, reconnect = wd.checkWake(base.Add(40*time.Second), HealthHealthy, "quic", 140)
	if gap != 29*time.Second || reconnect {
		t.Fatalf("below boundary: got gap=%v reconnect=%v, want 29s/false", gap, reconnect)
	}

	// Exactly 30s boundary: reconnect.
	gap, reconnect = wd.checkWake(base.Add(70*time.Second), HealthHealthy, "quic", 170)
	if gap != 30*time.Second || !reconnect {
		t.Fatalf("boundary: got gap=%v reconnect=%v, want 30s/true", gap, reconnect)
	}

	// Long sleep (60s): reconnect.
	gap, reconnect = wd.checkWake(base.Add(130*time.Second), HealthHealthy, "quic", 230)
	if gap != 60*time.Second || !reconnect {
		t.Fatalf("long sleep: got gap=%v reconnect=%v, want 60s/true", gap, reconnect)
	}

	// Negative gap (NTP rollback): no action.
	gap, reconnect = wd.checkWake(base.Add(120*time.Second), HealthHealthy, "quic", 220)
	if reconnect {
		t.Fatalf("NTP rollback: got reconnect=true, want false (gap=%v)", gap)
	}
}

func TestWakeDetector_CallbackOutsideLock(t *testing.T) {
	// Verify onWakeReconnect is called outside hm.mu — same pattern as
	// deadlock_test.go:TestHealthMonitor_NoDeadlock.
	tm := mockTransportManager()
	tunnel := core.NewClientTunnel(core.ClientTunnelConfig{})
	ot := &outboundTransport{tm: tm, name: "test"}
	hm := newHealthMonitor(ot, []*outboundTransport{ot}, tunnel, nil, nil, time.Now())

	// Simulate a callback that acquires hm.mu (like resetCounters does).
	var called bool
	hm.wake.onWakeReconnect = func() {
		// If called under hm.mu, this Lock() will deadlock (non-reentrant).
		hm.mu.Lock()
		called = true
		hm.mu.Unlock()
	}

	// Manually set lastWall to simulate a 60s sleep.
	hm.mu.Lock()
	hm.wake.lastWall = time.Now().Add(-60 * time.Second)
	hm.mu.Unlock()

	// Run one sample — should detect wake and call onWakeReconnect outside lock.
	done := make(chan struct{})
	go func() {
		hm.sample()
		close(done)
	}()

	select {
	case <-done:
		if !called {
			t.Fatal("onWakeReconnect was not called")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("deadlock: sample() blocked — onWakeReconnect likely called under hm.mu")
	}
}

// applyEWMA applies EWMA smoothing to healthMonitor fields.
// This is the logic that sample() applies before calling assess().
func applyEWMA(hm *healthMonitor, ws wire.WireStats) {
	if !hm.ewmaInitialized {
		hm.smoothWireLoss = ws.PacketLossRate
		hm.ewmaInitialized = true
	} else {
		hm.smoothWireLoss = ewmaAlpha*ws.PacketLossRate + (1-ewmaAlpha)*hm.smoothWireLoss
	}
}

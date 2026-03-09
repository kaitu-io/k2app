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
	hm := newHealthMonitor(tm, tunnel, func(s HealthSnapshot) { count++ }, nil)
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
	hm := newHealthMonitor(tm, tunnel, func(s HealthSnapshot) { last = s }, nil)
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

	hm := newHealthMonitor(tm, tunnel, nil, nil)
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
	// assess() reads smoothLoss/smoothThroughput from hm, so we set them
	// directly to unit-test the decision logic independent of EWMA.
	tm := mockTransportManager()
	tunnel := core.NewClientTunnel(core.ClientTunnelConfig{})
	hm := newHealthMonitor(tm, tunnel, nil, nil)

	// Start healthy.
	if hm.health != HealthHealthy {
		t.Fatalf("initial health: got %s, want healthy", hm.health)
	}

	// Simulate 5 consecutive high-loss samples → degraded.
	for i := 0; i < 5; i++ {
		hm.smoothLoss = 0.10
		hm.smoothThroughput = 0
		level := hm.assess(wire.WireStats{Transport: "quic"})
		hm.transition(level)
	}
	if hm.health != HealthDegraded {
		t.Fatalf("after 5 high-loss: got %s, want degraded", hm.health)
	}

	// Simulate 5 consecutive very-high-loss → critical.
	// 4 consecutive should NOT trigger critical (filters MI boundary noise ~2-3s).
	hm.peakThroughput = 100e6
	for i := 0; i < 4; i++ {
		hm.smoothLoss = 0.30
		hm.smoothThroughput = 10e6
		level := hm.assess(wire.WireStats{Transport: "quic"})
		hm.transition(level)
	}
	if hm.health == HealthCritical {
		t.Fatalf("after 4 very-high-loss: got critical too early (need 5)")
	}
	// 5th sample triggers critical.
	hm.smoothLoss = 0.30
	hm.smoothThroughput = 10e6
	level := hm.assess(wire.WireStats{Transport: "quic"})
	hm.transition(level)
	if hm.health != HealthCritical {
		t.Fatalf("after 5 very-high-loss: got %s, want critical", hm.health)
	}

	// Simulate 10 consecutive healthy samples → healthy.
	for i := 0; i < 10; i++ {
		hm.smoothLoss = 0.01
		hm.smoothThroughput = 80e6
		level := hm.assess(wire.WireStats{Transport: "quic"})
		hm.transition(level)
	}
	if hm.health != HealthHealthy {
		t.Fatalf("after 10 healthy: got %s, want healthy", hm.health)
	}
}

func TestHealthCooldown(t *testing.T) {
	tm := mockTransportManager()
	tunnel := core.NewClientTunnel(core.ClientTunnelConfig{})

	var actionCount int
	hm := newHealthMonitor(tm, tunnel, nil, func() { actionCount++ })

	// Force into critical state.
	hm.health = HealthCritical

	// First action should return a port hop func.
	action := hm.maybeAct()
	if action == nil {
		t.Fatal("expected action, got nil")
	}
	action() // execute deferred action
	if hm.hopAttempts != 1 {
		t.Fatalf("expected 1 hop attempt, got %d", hm.hopAttempts)
	}

	// Immediate retry should be blocked by cooldown.
	action = hm.maybeAct()
	if action != nil {
		t.Fatal("cooldown should block action")
	}
	if hm.hopAttempts != 1 {
		t.Fatalf("cooldown should block: expected 1, got %d", hm.hopAttempts)
	}

	// After cooldown expires, should fire again.
	hm.lastAction = time.Now().Add(-actionCooldown - time.Second)
	action = hm.maybeAct()
	if action == nil {
		t.Fatal("expected action after cooldown, got nil")
	}
	action()
	if hm.hopAttempts != 2 {
		t.Fatalf("expected 2 hop attempts, got %d", hm.hopAttempts)
	}
}

func TestHealthTransitions_Fallback(t *testing.T) {
	tm := mockTransportManager()
	tunnel := core.NewClientTunnel(core.ClientTunnelConfig{})
	hm := newHealthMonitor(tm, tunnel, nil, nil)

	// Fallback stats → assess returns degraded (not critical).
	for i := 0; i < 5; i++ {
		level := hm.assess(wire.WireStats{Transport: "tcpws", Fallback: true})
		hm.transition(level)
	}
	if hm.health != HealthDegraded {
		t.Fatalf("fallback should be degraded, got %s", hm.health)
	}

	// Fallback recovery: QUIC comes back with good stats → healthy after 10 samples.
	for i := 0; i < 10; i++ {
		hm.smoothLoss = 0.01
		hm.smoothThroughput = 80e6
		level := hm.assess(wire.WireStats{Transport: "quic"})
		hm.transition(level)
	}
	if hm.health != HealthHealthy {
		t.Fatalf("should be healthy after QUIC recovery, got %s", hm.health)
	}
}

func TestHealthEscalation(t *testing.T) {
	tm := mockTransportManager()
	tunnel := core.NewClientTunnel(core.ClientTunnelConfig{})

	var reconnectCount int
	hm := newHealthMonitor(tm, tunnel, nil, func() { reconnectCount++ })

	// Simulate 4 hop attempts (>3 triggers escalation to reconnect).
	hm.health = HealthCritical
	for i := 0; i < 4; i++ {
		hm.lastAction = time.Time{} // bypass cooldown
		action := hm.maybeAct()
		if action != nil {
			action() // execute deferred action
		}
	}

	if reconnectCount != 1 {
		t.Fatalf("expected 1 reconnect after >3 hops, got %d", reconnectCount)
	}
}

func TestHealthMonitor_ResetCounters(t *testing.T) {
	tm := mockTransportManager()
	tunnel := core.NewClientTunnel(core.ClientTunnelConfig{})
	hm := newHealthMonitor(tm, tunnel, nil, nil)

	// Drive into non-default state.
	hm.health = HealthCritical
	hm.degradedStreak = 5
	hm.criticalStreak = 3
	hm.healthyStreak = 0
	hm.hopAttempts = 2
	hm.peakThroughput = 100.0
	hm.smoothLoss = 0.5
	hm.smoothThroughput = 50000
	hm.ewmaInitialized = true

	hm.resetCounters()

	if hm.health != HealthHealthy {
		t.Fatalf("health = %s, want %s", hm.health, HealthHealthy)
	}
	if hm.degradedStreak != 0 || hm.criticalStreak != 0 || hm.healthyStreak != 0 {
		t.Fatalf("streaks not zeroed: degraded=%d critical=%d healthy=%d",
			hm.degradedStreak, hm.criticalStreak, hm.healthyStreak)
	}
	if hm.hopAttempts != 0 {
		t.Fatalf("hopAttempts = %d, want 0", hm.hopAttempts)
	}
	if hm.peakThroughput != 0 {
		t.Fatalf("peakThroughput = %f, want 0", hm.peakThroughput)
	}
	if hm.smoothLoss != 0 {
		t.Fatalf("smoothLoss = %f, want 0", hm.smoothLoss)
	}
	if hm.smoothThroughput != 0 {
		t.Fatalf("smoothThroughput = %f, want 0", hm.smoothThroughput)
	}
	if hm.ewmaInitialized {
		t.Fatal("ewmaInitialized should be false after reset")
	}
}

// ============================================================================
// Tests for EWMA smoothing + peakThroughput decay
// ============================================================================

func TestHealthMonitor_EWMASmoothing(t *testing.T) {
	tm := mockTransportManager()
	tunnel := core.NewClientTunnel(core.ClientTunnelConfig{})
	hm := newHealthMonitor(tm, tunnel, nil, nil)

	// First sample initializes EWMA
	hm.smoothLoss = 0
	hm.smoothThroughput = 0
	hm.ewmaInitialized = false

	// Simulate calling sample-like EWMA logic manually
	// (since sample() calls tm.WireStats() which returns zeros)
	ws := wire.WireStats{Transport: "quic", LossRate: 0.0, Throughput: 100000}
	applyEWMA(hm, ws)

	if hm.smoothLoss != 0.0 {
		t.Errorf("First sample: smoothLoss should be 0.0, got %f", hm.smoothLoss)
	}
	if hm.smoothThroughput != 100000 {
		t.Errorf("First sample: smoothThroughput should be 100000, got %f", hm.smoothThroughput)
	}

	// Single spike: lossRate=0.96 from 0.0 baseline
	ws2 := wire.WireStats{Transport: "quic", LossRate: 0.96, Throughput: 50000}
	applyEWMA(hm, ws2)

	// EWMA α=0.3: smoothLoss = 0.3 * 0.96 + 0.7 * 0.0 = 0.288
	if hm.smoothLoss < 0.27 || hm.smoothLoss > 0.30 {
		t.Errorf("After spike: smoothLoss should be ~0.288, got %f", hm.smoothLoss)
	}

	// After spike ends: 5 samples of 0.0 loss → smoothLoss decays
	for i := 0; i < 5; i++ {
		applyEWMA(hm, wire.WireStats{Transport: "quic", LossRate: 0.0, Throughput: 100000})
	}
	if hm.smoothLoss > 0.05 {
		t.Errorf("After decay: smoothLoss should be < 0.05, got %f", hm.smoothLoss)
	}
}

func TestHealthMonitor_EWMAPreventsFalseCritical(t *testing.T) {
	// After Layer 1 (ARC Snapshot fix), the health monitor sees moderate
	// residual noise (e.g., 0.15 loss) instead of extreme MI boundary spikes.
	// EWMA should prevent this moderate noise from escalating to CRITICAL.
	//
	// Without EWMA: 5 samples of 0.15 → degraded, then throughputDrop → CRITICAL
	// With EWMA: smoothLoss ramps slowly, never reaches veryHighLoss threshold
	tm := mockTransportManager()
	tunnel := core.NewClientTunnel(core.ClientTunnelConfig{})
	hm := newHealthMonitor(tm, tunnel, nil, nil)

	// Initialize with healthy baseline
	hm.smoothLoss = 0.01
	hm.smoothThroughput = 100000
	hm.ewmaInitialized = true
	hm.peakThroughput = 200000

	for i := 0; i < 10; i++ {
		level := hm.assess(wire.WireStats{Transport: "quic"})
		hm.transition(level)
	}
	if hm.health != HealthHealthy {
		t.Fatalf("Should be healthy after baseline, got %s", hm.health)
	}

	// Inject 5-second spike of moderate loss (0.15) — realistic post-Layer-1 noise
	for i := 0; i < 5; i++ {
		ws := wire.WireStats{Transport: "quic", LossRate: 0.15, Throughput: 50000}
		applyEWMA(hm, ws)
		level := hm.assess(wire.WireStats{Transport: "quic"})
		hm.transition(level)
	}

	// smoothLoss after 5 samples: converges toward 0.15 but from 0.01 baseline:
	// ~0.052, ~0.081, ~0.102, ~0.116, ~0.126
	// All are highLoss (>0.05) but never veryHighLoss (>0.25)
	// So health becomes degraded at most, NEVER critical
	if hm.health == HealthCritical {
		t.Errorf("EWMA should prevent false CRITICAL from moderate 5-sample spike, health=%s, smoothLoss=%f",
			hm.health, hm.smoothLoss)
	}
	if hm.smoothLoss > 0.25 {
		t.Errorf("smoothLoss should be < 0.25 after 5 samples of 0.15 from baseline 0.01, got %f", hm.smoothLoss)
	}
}

func TestHealthMonitor_PeakThroughputDecay(t *testing.T) {
	tm := mockTransportManager()
	tunnel := core.NewClientTunnel(core.ClientTunnelConfig{})
	hm := newHealthMonitor(tm, tunnel, nil, nil)

	hm.peakThroughput = 1e6 // 1 Mbps
	hm.ewmaInitialized = true
	hm.smoothThroughput = 0

	// Simulate 700 samples with 0 throughput (decay only)
	for i := 0; i < 700; i++ {
		hm.peakThroughput *= peakDecayPerSample
	}

	// Half-life of 0.999 decay ≈ 693 samples → peak should be roughly halved
	if hm.peakThroughput > 0.55e6 {
		t.Errorf("After 700 samples of decay, peak should be ~0.5e6, got %f", hm.peakThroughput)
	}
	if hm.peakThroughput < 0.45e6 {
		t.Errorf("After 700 samples of decay, peak should be ~0.5e6, got %f", hm.peakThroughput)
	}
}

func TestHealthMonitor_PeakTracksSmoothedNotRaw(t *testing.T) {
	tm := mockTransportManager()
	tunnel := core.NewClientTunnel(core.ClientTunnelConfig{})
	hm := newHealthMonitor(tm, tunnel, nil, nil)

	// Initialize with moderate throughput
	hm.ewmaInitialized = true
	hm.smoothThroughput = 50000
	hm.peakThroughput = 50000

	// Inject a single raw throughput spike of 1e6 (MI boundary artifact)
	ws := wire.WireStats{Transport: "quic", LossRate: 0, Throughput: 1e6}
	applyEWMA(hm, ws)

	// Update peak from smoothed (in sample)
	if hm.smoothThroughput > hm.peakThroughput {
		hm.peakThroughput = hm.smoothThroughput
	}

	// peakThroughput should NOT jump to 1e6 — should reflect smoothed value
	// smoothThroughput = 0.3 * 1e6 + 0.7 * 50000 = 335000
	if hm.peakThroughput > 400000 {
		t.Errorf("Peak should track smoothed (~335K), not raw (1M), got %f", hm.peakThroughput)
	}
	if hm.peakThroughput < 250000 {
		t.Errorf("Peak should be ~335K, got %f", hm.peakThroughput)
	}
}

// ============================================================================
// Tests for connection-level fallback
// ============================================================================

// ============================================================================
// Deadlock regression tests
// ============================================================================

func TestHealthMonitor_MaybeActReturnsAction(t *testing.T) {
	// Regression: maybeAct must return the action without executing it.
	// Before fix, maybeAct called onAction() inline while hm.mu was held,
	// causing self-deadlock when onAction → OnNetworkChanged → resetCounters → hm.mu.Lock().
	tm := mockTransportManager()
	tunnel := core.NewClientTunnel(core.ClientTunnelConfig{})

	var called bool
	hm := newHealthMonitor(tm, tunnel, nil, func() { called = true })
	hm.health = HealthCritical

	// Exhaust 3 port hops to reach full reconnect.
	for i := 0; i < 3; i++ {
		hm.lastAction = time.Time{}
		action := hm.maybeAct()
		if action != nil {
			action()
		}
	}
	if called {
		t.Fatal("onAction should not be called during port hops")
	}

	// 4th attempt should return onAction without calling it.
	hm.lastAction = time.Time{}
	action := hm.maybeAct()
	if action == nil {
		t.Fatal("4th attempt should return full reconnect action")
	}
	if called {
		t.Fatal("onAction should not be called yet (deferred)")
	}

	// Execute the returned action.
	action()
	if !called {
		t.Fatal("onAction should be called after executing returned action")
	}
}

func TestHealthMonitor_TransitionReturnsDeferredAction(t *testing.T) {
	// Regression: transition must return the action, not execute it inline.
	tm := mockTransportManager()
	tunnel := core.NewClientTunnel(core.ClientTunnelConfig{})

	var called bool
	hm := newHealthMonitor(tm, tunnel, nil, func() { called = true })

	// Force critical state and exhaust port hops.
	hm.health = HealthCritical
	hm.hopAttempts = 3
	hm.lastAction = time.Time{} // bypass cooldown

	action := hm.transition(HealthCritical)
	if action == nil {
		t.Fatal("transition should return action for full reconnect")
	}
	if called {
		t.Fatal("onAction must not be called inside transition")
	}

	action()
	if !called {
		t.Fatal("onAction should fire when returned action is executed")
	}
}

func TestHealthMonitor_NoConcurrentDeadlock(t *testing.T) {
	// Regression: sample() + stop() must not deadlock.
	// Before fix, Stop() held e.mu and called hm.stop() (wg.Wait),
	// while sample()'s onAction callback needed e.mu → cross-deadlock.
	//
	// This test simulates the scenario: onAction acquires a shared mutex
	// (simulating e.mu), while stop() is called from a goroutine that
	// also holds that mutex.
	tm := mockTransportManager()
	tunnel := core.NewClientTunnel(core.ClientTunnelConfig{})

	var sharedMu sync.Mutex
	hm := newHealthMonitor(tm, tunnel, nil, func() {
		// Simulate OnNetworkChanged acquiring e.mu briefly.
		sharedMu.Lock()
		time.Sleep(time.Millisecond)
		sharedMu.Unlock()
	})

	// Force critical state with exhausted hops so onAction will fire.
	hm.mu.Lock()
	hm.health = HealthCritical
	hm.hopAttempts = 3
	hm.lastAction = time.Time{}
	hm.smoothLoss = 0.5
	hm.ewmaInitialized = true
	hm.mu.Unlock()

	hm.start()

	// Deadline: if this test doesn't complete in 5s, it's a deadlock.
	done := make(chan struct{})
	go func() {
		// Wait for health goroutine to potentially enter onAction.
		time.Sleep(1500 * time.Millisecond)

		// Simulate Stop() pattern: acquire shared lock, then stop health.
		// Before fix, this would deadlock if health goroutine was inside onAction.
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

// applyEWMA applies EWMA smoothing to healthMonitor fields.
// This is the logic that sample() applies before calling assess().
func applyEWMA(hm *healthMonitor, ws wire.WireStats) {
	if !hm.ewmaInitialized {
		hm.smoothLoss = ws.LossRate
		hm.smoothThroughput = ws.Throughput
		hm.ewmaInitialized = true
	} else {
		hm.smoothLoss = ewmaAlpha*ws.LossRate + (1-ewmaAlpha)*hm.smoothLoss
		hm.smoothThroughput = ewmaAlpha*ws.Throughput + (1-ewmaAlpha)*hm.smoothThroughput
	}
}

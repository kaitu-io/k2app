package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kaitu-io/k2/core"
	"github.com/kaitu-io/k2/wire"
)

// TestHealthSnapshot_HasChannelScores verifies HealthSnapshot serializes the
// dual-channel quality scoreboard (Task 8 observability) — daemon/gateway's
// broadcastSnapshot wholesale-marshals HealthSnapshot for SSE, so any field
// added here reaches those consumers automatically.
func TestHealthSnapshot_HasChannelScores(t *testing.T) {
	var snap HealthSnapshot
	snap.ScoreQUIC = 0.9
	snap.ScoreTCPWS = 0.5
	snap.ActiveChannel = "quic"
	b, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"score_quic", "score_tcpws", "active_channel"} {
		if !strings.Contains(string(b), key) {
			t.Fatalf("snapshot JSON missing %q: %s", key, b)
		}
	}
}

// mockTransportManager returns a TransportManager with no transports set.
// WireStats() returns zero values; Reset() is a no-op.
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

	// Fallback (TCP-WS) with clean stats: health is quality-driven, not
	// channel-driven — a clean fallback path settles healthy, not degraded.
	for i := 0; i < 10; i++ {
		hm.smoothWireLoss = 0.0
		level := hm.assess(wire.WireStats{Transport: "tcpws", WireFallback: true})
		hm.transition(level, false)
	}
	if hm.health != HealthHealthy {
		t.Fatalf("clean TCP-WS fallback should be healthy, got %s", hm.health)
	}

	// Quality degrades while still on TCP-WS fallback: health tracks the
	// loss/RTT signal, not the fallback flag itself.
	for i := 0; i < 5; i++ {
		hm.smoothWireLoss = 0.10
		level := hm.assess(wire.WireStats{Transport: "tcpws", WireFallback: true})
		hm.transition(level, false)
	}
	if hm.health != HealthDegraded {
		t.Fatalf("high-loss TCP-WS fallback should be degraded, got %s", hm.health)
	}

	// Recovery: QUIC comes back with good stats → healthy after 10 samples.
	for i := 0; i < 10; i++ {
		hm.smoothWireLoss = 0.01
		level := hm.assess(wire.WireStats{Transport: "quic"})
		hm.transition(level, false)
	}
	if hm.health != HealthHealthy {
		t.Fatalf("should be healthy after QUIC recovery, got %s", hm.health)
	}
}

func TestAssess_TCPWSActiveHealthyNotDegraded(t *testing.T) {
	hm := &healthMonitor{health: HealthHealthy}
	// TCP-WS is serving, but quality is good — must read healthy, not degraded.
	ws := wire.WireStats{Transport: "tcpws", WireFallback: true}
	hm.smoothWireLoss = 0.0
	hm.ewmaInitialized = true
	if got := hm.assess(ws); got != HealthHealthy {
		t.Fatalf("healthy TCP-WS should assess healthy, got %v", got)
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

// --- echoStep state-machine tests ----------------------------------------
//
// echoStep is a pure function; these tests cover every transition in §6 of
// the wire-health-exponential-trust plan. No time, no I/O, no goroutines.

func TestEchoStep_OkGrowsTrustOneStep(t *testing.T) {
	res := echoStep(0, 0, nil)
	if res.NewTrust != 1 || res.NewStrikes != 0 || res.ShouldRerace || res.Classify != echoOK {
		t.Fatalf("got %+v", res)
	}
}

func TestEchoStep_OkCapsAtMaxTrust(t *testing.T) {
	maxIdx := int32(len(echoIntervals)) - 1
	res := echoStep(maxIdx, 0, nil)
	if res.NewTrust != maxIdx {
		t.Fatalf("trust should cap at %d, got %d", maxIdx, res.NewTrust)
	}
}

func TestEchoStep_OkResetsStrikes(t *testing.T) {
	// strikes was non-zero (mid-failure-streak); a single ok resets it.
	res := echoStep(0, 2, nil)
	if res.NewStrikes != 0 {
		t.Fatalf("strikes should reset to 0 on ok, got %d", res.NewStrikes)
	}
}

func TestEchoStep_UnsupportedTreatedAsOk(t *testing.T) {
	// errors.New = non-DeadlineExceeded → IsEchoUnsupported = true.
	res := echoStep(0, 0, errors.New("stream reset by old server"))
	if res.NewTrust != 1 || res.NewStrikes != 0 || res.ShouldRerace || res.Classify != echoUnsupported {
		t.Fatalf("unsupported should behave like ok for trust/strikes/rerace, got %+v", res)
	}
}

func TestEchoStep_UnsupportedAlsoCapsAtMaxTrust(t *testing.T) {
	maxIdx := int32(len(echoIntervals)) - 1
	res := echoStep(maxIdx, 0, io.EOF)
	if res.NewTrust != maxIdx || res.Classify != echoUnsupported {
		t.Fatalf("unsupported at max should stay at max, got %+v", res)
	}
}

func TestEchoStep_FailResetsTrust(t *testing.T) {
	// At max trust, single real failure collapses to 0.
	maxIdx := int32(len(echoIntervals)) - 1
	res := echoStep(maxIdx, 0, context.DeadlineExceeded)
	if res.NewTrust != 0 || res.Classify != echoFail {
		t.Fatalf("fail should collapse trust to 0, got %+v", res)
	}
}

// TestEchoStep_QUICStreamDeadlineCountsAsFailure pins the cross-layer contract
// that a black-holed data path accumulates strikes toward a re-race.
//
// This is the error shape EchoProbe actually returns when the response never
// arrives: stream.SetReadDeadline expires and quic-go reports a deadline error
// that unwraps to os.ErrDeadlineExceeded — NOT context.DeadlineExceeded, which
// is what every other case here feeds in. Classifying it as echoUnsupported
// grows trust and holds strikes at zero, so the engine never re-races off a
// dead wire while the UI still reads "connected".
//
// The synthetic error below only guards the engine's half of the contract.
// wire.TestIsEchoUnsupported_RealStreamDeadlineIsNotUnsupported is the
// authority: it drives a real quic-go connection to a real read deadline.
func TestEchoStep_QUICStreamDeadlineCountsAsFailure(t *testing.T) {
	err := fmt.Errorf("echo: read response: %w", os.ErrDeadlineExceeded)

	res := echoStep(0, 0, err)
	if res.Classify != echoFail {
		t.Fatalf("QUIC stream deadline classified as %v, want echoFail: %v", res.Classify, err)
	}
	if res.NewStrikes != 1 {
		t.Fatalf("strikes should advance to 1 on a dead data path, got %d", res.NewStrikes)
	}

	// Three consecutive deadlines must reach the re-race threshold.
	res = echoStep(0, echoReraceThreshold-1, err)
	if !res.ShouldRerace {
		t.Fatalf("%d consecutive stream deadlines did not trigger re-race: %+v",
			echoReraceThreshold, res)
	}
}

func TestEchoStep_FailAccumulatesStrikes(t *testing.T) {
	r1 := echoStep(0, 0, context.DeadlineExceeded)
	r2 := echoStep(r1.NewTrust, r1.NewStrikes, context.DeadlineExceeded)
	if r1.NewStrikes != 1 || r2.NewStrikes != 2 {
		t.Fatalf("strikes should accumulate: r1=%d r2=%d", r1.NewStrikes, r2.NewStrikes)
	}
	if r1.ShouldRerace || r2.ShouldRerace {
		t.Fatal("rerace must not fire below threshold")
	}
}

func TestEchoStep_FailFiresReraceAtThreshold(t *testing.T) {
	// Walk to 3 strikes — third must signal rerace.
	trust, strikes := int32(0), int32(0)
	for i := 1; i <= echoReraceThreshold; i++ {
		r := echoStep(trust, strikes, context.DeadlineExceeded)
		trust, strikes = r.NewTrust, r.NewStrikes
		if i < echoReraceThreshold && r.ShouldRerace {
			t.Fatalf("rerace fired prematurely at strike %d", i)
		}
		if i == echoReraceThreshold && !r.ShouldRerace {
			t.Fatalf("rerace did not fire at strike %d", i)
		}
	}
}

func TestEchoStep_OkAfterFailResetsStrikesNotTrust(t *testing.T) {
	// After 2 fails: trust=0, strikes=2.
	r1 := echoStep(2, 0, context.DeadlineExceeded) // trust 2→0, strikes 0→1
	r2 := echoStep(r1.NewTrust, r1.NewStrikes, context.DeadlineExceeded)
	// One ok must reset strikes and start growing trust again.
	r3 := echoStep(r2.NewTrust, r2.NewStrikes, nil)
	if r3.NewStrikes != 0 {
		t.Fatalf("ok should reset strikes, got %d", r3.NewStrikes)
	}
	if r3.NewTrust != 1 {
		t.Fatalf("ok should grow trust by 1, got %d", r3.NewTrust)
	}
}

// --- echoIntervals invariants --------------------------------------------

func TestEchoIntervals_WorstCaseDetectionBelowQUICIdle(t *testing.T) {
	// Mirror of the init() invariant. Tests the same property explicitly so
	// failure mode is a test fail (clear) instead of a startup panic (opaque).
	ceiling := echoIntervals[len(echoIntervals)-1]
	base := echoIntervals[0]
	worst := ceiling + echoProbeTimeout +
		time.Duration(echoReraceThreshold-1)*(base+echoProbeTimeout)
	quicIdle := wire.DefaultQUICConfig().MaxIdleTimeout
	if worst >= quicIdle {
		t.Fatalf("worst-case detection %v ≥ QUIC MaxIdleTimeout %v — echoLoop "+
			"would lose detection race to passive idle teardown",
			worst, quicIdle)
	}
}

func TestEchoIntervals_ExponentiallyGrowing(t *testing.T) {
	for i := 1; i < len(echoIntervals); i++ {
		if echoIntervals[i] != 2*echoIntervals[i-1] {
			t.Fatalf("echoIntervals[%d]=%v should be 2× echoIntervals[%d]=%v",
				i, echoIntervals[i], i-1, echoIntervals[i-1])
		}
	}
}

func TestEchoIntervals_BaseIsOneSecond(t *testing.T) {
	if echoIntervals[0] != time.Second {
		t.Fatalf("echoIntervals[0] = %v, expected 1s (must be fast to catch t≈0 wire failures)",
			echoIntervals[0])
	}
}

// Scenario walk: wire dies at t≈1s — verify detection at ~5s per plan §5.1.
// Replays the iteration logic against echoStep with synthesized probe outcomes.
func TestEchoStep_ScenarioWireFailsEarly(t *testing.T) {
	// t=1s: handshake just succeeded, first probe finds wire still healthy.
	r := echoStep(0, 0, nil)
	if r.Classify != echoOK || r.NewTrust != 1 {
		t.Fatalf("step 1 (ok at t=1s): %+v", r)
	}

	// Wire breaks. Subsequent probes timeout.
	r = echoStep(r.NewTrust, r.NewStrikes, context.DeadlineExceeded)
	if r.NewStrikes != 1 || r.NewTrust != 0 || r.ShouldRerace {
		t.Fatalf("step 2 (fail #1): %+v", r)
	}
	r = echoStep(r.NewTrust, r.NewStrikes, context.DeadlineExceeded)
	if r.NewStrikes != 2 || r.ShouldRerace {
		t.Fatalf("step 3 (fail #2): %+v", r)
	}
	r = echoStep(r.NewTrust, r.NewStrikes, context.DeadlineExceeded)
	if r.NewStrikes != echoReraceThreshold || !r.ShouldRerace {
		t.Fatalf("step 4 (fail #3, expect rerace): %+v", r)
	}
}

// --- Task 7: echoLoop drives the dual-channel scoreboard ------------------
//
// Uses the echoStubDialer / newEchoTransport / newHMForEcho fakes from
// health_echo_integration_test.go. probeOnceForTest drives exactly one
// echoProbeLoop (primary-channel) iteration synchronously. The backup-probe
// tests below (fix-round-1) call hm.probeBackupOnce directly with an
// explicit *backupLoopState that persists across calls, since — after
// fix-round-1 decoupled the backup probe onto its own timer/goroutine
// (Important 1 + 4) — probeOnceForTest no longer touches backup logic at
// all, and cadence assertions require state that survives multiple
// synthetic-time calls.

func TestEchoLoop_FeedsChannelScore(t *testing.T) {
	// A healthMonitor with one TM whose QUIC echo succeeds should raise that
	// channel's availability score above neutral (0.5*0.5=0.25) within one
	// probe cycle.
	ot, _ := newEchoTransport("t-quic-ok", func(ctx context.Context) error { return nil })
	hm, _ := newHMForEcho(t, []*outboundTransport{ot})

	now := time.Now()
	hm.probeOnceForTest(ot, now)

	if got := ot.tm.ScoreQUIC(now); got <= 0.5*0.5 {
		t.Fatalf("successful echo should push score above neutral, got %v", got)
	}
}

func TestBackupProbe_CadenceUpgradesWhenQUICDegraded(t *testing.T) {
	// fix-round-1 (Important 1 + 3): the backup probe's cadence must be the
	// REAL cadence it runs at — 5s once QUIC degrades, not bounded by
	// whatever the primary's exponential-trust timer happens to be at. This
	// test drives probeBackupOnce directly across synthetic time steps (no
	// real waiting) with one *backupLoopState carried across calls, so it
	// actually exercises pacing — unlike the original (vacuous) version,
	// which built a fresh state each call and so let every call through
	// unconditionally regardless of which interval constant was in play.
	ot, _ := newEchoTransport("t-quic-degraded", func(ctx context.Context) error { return nil })
	backup := &echoStubDialer{}
	backup.setFn(func(ctx context.Context) error { return nil })
	ot.tm.SetTCPWS(backup)

	t0 := time.Now()
	// context.DeadlineExceeded is a real failure (not IsEchoUnsupported),
	// so this actually drives scoreQUIC down.
	ot.tm.RecordEchoResult("quic", 0, context.DeadlineExceeded, t0)
	if got := ot.tm.ScoreQUIC(t0); got >= echoBackupProbeThreshold {
		t.Fatalf("setup: expected degraded QUIC score below %v, got %v", echoBackupProbeThreshold, got)
	}

	hm, _ := newHMForEcho(t, []*outboundTransport{ot})
	st := &backupLoopState{}

	// t0: first check — nothing scheduled yet, so it probes immediately and
	// reports the degraded (5s) cadence.
	if interval := hm.probeBackupOnce(ot, st, t0); interval != backupProbeIntervalDegraded {
		t.Fatalf("expected degraded cadence %v at t0, got %v", backupProbeIntervalDegraded, interval)
	}
	if calls := backup.calls.Load(); calls != 1 {
		t.Fatalf("expected 1 probe at t0, got %d", calls)
	}

	// t0+4s: inside the 5s cadence window — must NOT probe again.
	hm.probeBackupOnce(ot, st, t0.Add(4*time.Second))
	if calls := backup.calls.Load(); calls != 1 {
		t.Fatalf("expected no probe at t0+4s (within the 5s degraded cadence), got %d calls", calls)
	}

	// t0+6s: cadence window elapsed — probes again.
	hm.probeBackupOnce(ot, st, t0.Add(6*time.Second))
	if calls := backup.calls.Load(); calls != 2 {
		t.Fatalf("expected a second probe at t0+6s, got %d calls", calls)
	}
}

func TestBackupProbe_HealthyCadenceStaysLowFrequency(t *testing.T) {
	// Mirror of the degraded test above, but with QUIC healthy: the real
	// PROBE cadence must stay 60s, even though (fix-round-3, N2) the WAKE
	// cadence returned by probeBackupOnce is now always capped at
	// backupProbeIntervalDegraded regardless of branch — so healthy vs
	// degraded is no longer visible in the return value itself. Instead
	// this proves it behaviorally: a re-check at exactly +5s (which WOULD
	// re-probe if the real gate had mistakenly become the 5s degraded
	// cadence) must NOT probe, and neither does a re-check well within the
	// 60s window. This is what actually proves the two PROBE cadences are
	// distinct, not just that the WAKE cadence is capped either way.
	ot, _ := newEchoTransport("t-quic-healthy", func(ctx context.Context) error { return nil })
	backup := &echoStubDialer{}
	backup.setFn(func(ctx context.Context) error { return nil })
	ot.tm.SetTCPWS(backup)

	t0 := time.Now()
	// Availability alone caps Value() at avail*neutral(0.5)=0.5, which is
	// BELOW echoBackupProbeThreshold (0.7) — quality (CC stats) must also be
	// primed for scoreQUIC to clear 0.7 and land in the healthy branch.
	ot.tm.RecordEchoResult("quic", 0, nil, t0)
	ot.tm.RecordQUICStats(wire.WireStats{PacketLossRate: 0, RTT: 50 * time.Millisecond, MinRTT: 50 * time.Millisecond}, t0)
	if got := ot.tm.ScoreQUIC(t0); got < echoBackupProbeThreshold {
		t.Fatalf("setup: expected healthy QUIC score >= %v, got %v", echoBackupProbeThreshold, got)
	}

	hm, _ := newHMForEcho(t, []*outboundTransport{ot})
	st := &backupLoopState{}

	if wake := hm.probeBackupOnce(ot, st, t0); wake > backupProbeIntervalDegraded {
		t.Fatalf("expected the wake cadence to be capped at %v, got %v", backupProbeIntervalDegraded, wake)
	}
	if calls := backup.calls.Load(); calls != 1 {
		t.Fatalf("expected 1 probe at t0, got %d", calls)
	}

	// If the real PROBE cadence were mistakenly the degraded 5s instead of
	// the healthy 60s, a check landing exactly at t0+5s would re-probe.
	hm.probeBackupOnce(ot, st, t0.Add(backupProbeIntervalDegraded))
	if calls := backup.calls.Load(); calls != 1 {
		t.Fatalf("expected no second probe at t0+%v — QUIC is healthy, the real probe cadence must be %v, not %v", backupProbeIntervalDegraded, backupProbeIntervalHealthy, backupProbeIntervalDegraded)
	}

	// And well within the 60s healthy window generally.
	hm.probeBackupOnce(ot, st, t0.Add(30*time.Second))
	if calls := backup.calls.Load(); calls != 1 {
		t.Fatalf("expected no second probe within the 60s healthy cadence (checked at +30s), got %d calls", calls)
	}
}

func TestBackupProbe_SkipsWhenPrimaryIsTCPWS(t *testing.T) {
	// fix-round-1 Important 2: when tcpws wins the transport race outright
	// (no separate QUIC dialer connected), engine.go's setupOutboundTransport
	// installs the SAME *TCPWSClient as both tm.primary and tm.TCPWS() (mirrors
	// engine.go's tm.SetPrimary(raceResult.Dialer, "tcpws") + tm.SetTCPWS(...)
	// when raceResult.QUICBackup is nil and raceResult.TransportType=="tcpws").
	// ActiveChannel() still nominally reports "quic" (fresh TransportManager
	// default) even though PrimaryType is "tcpws". probeBackupOnce must
	// recognize this and skip — probing "the backup" would double-probe the
	// identical connection echoProbeLoop is already probing as the primary.
	stub := &echoStubDialer{}
	stub.setFn(func(ctx context.Context) error { return nil })
	tm := wire.NewTransportManager()
	tm.SetPrimary(stub, "tcpws")
	tm.SetTCPWS(stub)
	ot := &outboundTransport{tm: tm, name: "t-tcpws-only"}

	hm, _ := newHMForEcho(t, []*outboundTransport{ot})
	st := &backupLoopState{}

	now := time.Now()
	if interval := hm.probeBackupOnce(ot, st, now); interval != backupProbeIntervalHealthy {
		t.Fatalf("expected the no-op/healthy pace, got %v", interval)
	}
	if calls := stub.calls.Load(); calls != 0 {
		t.Fatalf("expected zero backup probes when primary IS tcpws (double-probe guard), got %d", calls)
	}
}

func TestBackupProbe_MobilePowerSavingSkipsProbe(t *testing.T) {
	// Degraded-QUIC setup (would normally upgrade to 5s cadence), but with
	// MobilePowerSaving on: backup probing must NOT fire — mobile relies on
	// protocol keepalive and lets the tcpws score decay to neutral by
	// staleness instead.
	ot, _ := newEchoTransport("t-quic-degraded-ps", func(ctx context.Context) error { return nil })
	backup := &echoStubDialer{}
	backup.setFn(func(ctx context.Context) error { return nil })
	ot.tm.SetTCPWS(backup)

	now := time.Now()
	ot.tm.RecordEchoResult("quic", 0, context.DeadlineExceeded, now)

	hm, _ := newHMForEcho(t, []*outboundTransport{ot})
	hm.mobilePowerSaving = true
	st := &backupLoopState{}

	hm.probeBackupOnce(ot, st, now)

	if calls := backup.calls.Load(); calls != 0 {
		t.Fatalf("MobilePowerSaving must disable active backup probing, got %d calls", calls)
	}
}

// --- fix-round-2 --------------------------------------------------------

// statsEchoStub adds wire.StatsProvider (WireStats()) on top of
// echoStubDialer's wire.Dialer + wire.EchoProber, so TransportManager.
// WireStats() takes the "quic" branch and calls RecordQUICStats — the exact
// production path the N1 fix relies on. Test-only.
type statsEchoStub struct {
	echoStubDialer
	stats wire.WireStats
}

func (s *statsEchoStub) WireStats() wire.WireStats {
	return s.stats
}

func TestSample_FeedsQUICQualityForEveryOutboundNotJustPrimary(t *testing.T) {
	// N1 regression: before this fix, healthMonitor.sample() only ever
	// called WireStats() on hm.primaryTM, so a secondary outbound's
	// scoreQUIC.quality never initialized — permanently neutral 0.5 —
	// capping ScoreQUIC at avail×0.5 ≤ 0.5 forever, always below
	// echoBackupProbeThreshold (0.7). Two consequences on any multi-outbound
	// config: (a) probeBackupOnce permanently stuck in the degraded 5s
	// branch, and (b) once a couple of real (healthy) tcpws backup probes
	// land, wire's selectorStep quality path (scoreTCPWS > scoreQUIC×1.5,
	// sustained, backupFresh≥2) trips a false switch-away on a perfectly
	// healthy secondary QUIC outbound.
	//
	// This drives the real hm.sample() (not a hand-rolled WireStats call)
	// so a regression in the sampler loop itself is what this test is
	// sensitive to — see the mutation evidence in the fix-round-2 report
	// section (revert to primaryTM-only sampling → RED).
	healthyStats := wire.WireStats{
		Transport:      "quic",
		PacketLossRate: 0,
		RTT:            50 * time.Millisecond,
		MinRTT:         50 * time.Millisecond,
	}

	primaryStub := &statsEchoStub{stats: healthyStats}
	primaryStub.setFn(func(ctx context.Context) error { return nil })
	primaryTM := wire.NewTransportManager()
	primaryTM.SetPrimary(primaryStub, "quic-443")
	primaryOT := &outboundTransport{tm: primaryTM, name: "t-primary"}

	secondaryStub := &statsEchoStub{stats: healthyStats}
	secondaryStub.setFn(func(ctx context.Context) error { return nil })
	secondaryTM := wire.NewTransportManager()
	secondaryTM.SetPrimary(secondaryStub, "quic-443")
	secondaryOT := &outboundTransport{tm: secondaryTM, name: "t-secondary"}

	backup := &echoStubDialer{}
	backup.setFn(func(ctx context.Context) error { return nil })
	secondaryTM.SetTCPWS(backup)

	// ScoreQUIC = availability × quality. Availability comes from dial/echo
	// outcomes (recordDialOutcome / RecordEchoResult), not from WireStats —
	// every connected outbound necessarily had a successful primary dial, so
	// simulate that here. This test is about the QUALITY component (N1's
	// bug), not availability, so priming it directly keeps the test focused.
	primaryTM.RecordEchoResult("quic", 0, nil, time.Now())
	secondaryTM.RecordEchoResult("quic", 0, nil, time.Now())

	tunnel := core.NewClientTunnel(core.ClientTunnelConfig{})
	hm := newHealthMonitor(primaryOT, []*outboundTransport{primaryOT, secondaryOT}, tunnel, nil, nil, time.Now())

	// Drive a few real sample() ticks directly (no ticker/goroutine needed —
	// sample() uses time.Now() internally, which is fine: we only need
	// scoreQUIC's quality component to prime, not to control exact timing).
	for range 3 {
		hm.sample()
	}

	now := time.Now()
	if got := secondaryOT.tm.ScoreQUIC(now); got < echoBackupProbeThreshold {
		t.Fatalf("N1 regression: secondary outbound's ScoreQUIC never primed by sample(), got %v (want >= %v) — quality stuck neutral because only primaryTM was ever sampled", got, echoBackupProbeThreshold)
	}

	// (a) backup cadence must land in the healthy 60s branch, not stuck
	// degraded forever. Fix-round-3 caps probeBackupOnce's return value at
	// backupProbeIntervalDegraded even on the healthy path (that's the WAKE
	// cadence — see probeBackupOnce's doc), so the healthy/degraded
	// distinction is no longer visible in the return value directly; check
	// the real PROBE gate instead — a call landing exactly where the
	// degraded 5s cadence would have re-probed must NOT re-probe.
	st := &backupLoopState{}
	if wake := hm.probeBackupOnce(secondaryOT, st, now); wake > backupProbeIntervalDegraded {
		t.Fatalf("N1 regression: wake cadence not capped, got %v", wake)
	}
	if calls := backup.calls.Load(); calls != 1 {
		t.Fatalf("N1 regression: expected 1 probe, got %d", calls)
	}
	hm.probeBackupOnce(secondaryOT, st, now.Add(backupProbeIntervalDegraded))
	if calls := backup.calls.Load(); calls != 1 {
		t.Fatalf("N1 regression: secondary re-probed at +%v — real cadence is degraded (5s) not healthy (60s), quality still stuck neutral pre-fix", backupProbeIntervalDegraded)
	}

	// (b) the hysteresis FSM's quality-path switch-away condition must not
	// hold: with scoreQUIC primed near 1.0, scoreTCPWS (even if probed and
	// near its own max of 1.0) can never exceed scoreQUIC×1.5 — the raw
	// invariant that gates channelSwitchToTCPWS, independent of how long we
	// wait. 1.5 mirrors wire's private switchAwayRatio (wire/transport.go).
	const switchAwayRatio = 1.5
	scoreQUIC := secondaryOT.tm.ScoreQUIC(now)
	scoreTCPWS := secondaryOT.tm.ScoreTCPWS(now)
	if scoreTCPWS > scoreQUIC*switchAwayRatio {
		t.Fatalf("N1 regression: switch-away condition holds (scoreTCPWS=%v > scoreQUIC=%v×%v) — a healthy secondary QUIC outbound would eventually be switched away from", scoreTCPWS, scoreQUIC, switchAwayRatio)
	}

	secondaryTM.EvaluateSelector(now)
	if secondaryTM.ActiveChannel() != "quic" {
		t.Fatalf("N1 regression: secondary outbound's healthy QUIC was switched away from (scoreQUIC=%v, scoreTCPWS=%v)", scoreQUIC, scoreTCPWS)
	}
}

func TestBackupProbe_ModeChangeDuringHealthyWaitNoticedQuickly(t *testing.T) {
	// N2, reopened in fix-round-3: the round-2 fix's pull-in logic (in the
	// `now.Before(st.nextProbeAt)` waiting branch) was correct in isolation
	// but UNREACHABLE from production. backupProbeLoop's only wake source is
	// probeBackupOnce's own previous return value (timer.Reset(next)); round
	// 2's PROBE path still returned the full healthy interval (60s)
	// unchanged, so the next wake always landed exactly at (or after) the
	// freshly-set st.nextProbeAt — now.Before(st.nextProbeAt) was therefore
	// never true in production, only reachable by a test driving a
	// synthetic mid-window `now` directly (which is exactly what round 2's
	// version of this test did — it defended the bug, asserting the t0
	// return WAS the full 60s). Fix-round-3 caps the probe path's return the
	// same way the waiting path already was.
	//
	// This version walks the timeline the way backupProbeLoop's own timer
	// actually would: each call's `now` is the PREVIOUS call's own returned
	// wake duration later — never a synthetic mid-window jump — so a
	// regression back to "probe path returns the uncapped interval" is
	// caught by the very first assertion below, not by the timeline walk.
	ot, _ := newEchoTransport("t-quic-mode-change", func(ctx context.Context) error { return nil })
	backup := &echoStubDialer{}
	backup.setFn(func(ctx context.Context) error { return nil })
	ot.tm.SetTCPWS(backup)

	t0 := time.Now()
	ot.tm.RecordEchoResult("quic", 0, nil, t0)
	ot.tm.RecordQUICStats(wire.WireStats{PacketLossRate: 0, RTT: 50 * time.Millisecond, MinRTT: 50 * time.Millisecond}, t0)
	if got := ot.tm.ScoreQUIC(t0); got < echoBackupProbeThreshold {
		t.Fatalf("setup: expected healthy QUIC score >= %v, got %v", echoBackupProbeThreshold, got)
	}

	hm, _ := newHMForEcho(t, []*outboundTransport{ot})
	st := &backupLoopState{}

	// t0: probes (healthy branch). The wake duration returned MUST be
	// capped at backupProbeIntervalDegraded even here — NOT the full 60s
	// healthy interval. Returning the uncapped interval is exactly the
	// round-3 bug: it would make backupProbeLoop's next wake land exactly
	// on top of nextProbeAt, so the waiting branch (and its pull-in) would
	// never actually be entered in production.
	wake := hm.probeBackupOnce(ot, st, t0)
	if wake > backupProbeIntervalDegraded {
		t.Fatalf("expected the wake cadence to be capped at %v even in the healthy branch, got %v", backupProbeIntervalDegraded, wake)
	}
	if calls := backup.calls.Load(); calls != 1 {
		t.Fatalf("expected 1 probe at t0, got %d", calls)
	}

	// QUIC degrades immediately after the t0 probe — the worst case for
	// detection latency.
	degradeAt := t0.Add(time.Millisecond)
	ot.tm.RecordEchoResult("quic", 0, context.DeadlineExceeded, degradeAt)
	if got := ot.tm.ScoreQUIC(degradeAt); got >= echoBackupProbeThreshold {
		t.Fatalf("setup: expected degraded QUIC score below %v, got %v", echoBackupProbeThreshold, got)
	}

	// Walk the timeline exactly the way backupProbeLoop's real timer would:
	// each wake's `now` is the previous wake's `now` plus that SAME call's
	// own returned duration.
	now := t0.Add(wake)
	var probedAt time.Time
	for i := 0; i < 20; i++ {
		w := hm.probeBackupOnce(ot, st, now)
		if w > backupProbeIntervalDegraded {
			t.Fatalf("wake #%d (now=t0+%v) returned %v — exceeds the %v mode-check cadence", i, now.Sub(t0), w, backupProbeIntervalDegraded)
		}
		if backup.calls.Load() == 2 {
			probedAt = now
			break
		}
		now = now.Add(w)
		if now.Sub(t0) >= backupProbeIntervalHealthy {
			break
		}
	}

	if probedAt.IsZero() {
		t.Fatalf("degraded probe never fired while walking the timeline (stopped at t0+%v)", now.Sub(t0))
	}
	// (b) fires within ~5s of degradation — bounded by (wake to notice) +
	// (wake to actually probe once the gate is pulled in), i.e.
	// ≤2×backupProbeIntervalDegraded, never at the old 60s boundary.
	if elapsed := probedAt.Sub(degradeAt); elapsed > 2*backupProbeIntervalDegraded {
		t.Fatalf("degraded probe fired %v after degradation — worse than the ~%v the mode-check cadence should allow", elapsed, 2*backupProbeIntervalDegraded)
	}
	if elapsed := probedAt.Sub(t0); elapsed >= backupProbeIntervalHealthy {
		t.Fatalf("degraded probe fired at t0+%v — no better than the stale 60s boundary this fix targets", elapsed)
	}
}

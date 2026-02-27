package engine

import (
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
	tm := mockTransportManager()
	tunnel := core.NewClientTunnel(core.ClientTunnelConfig{})
	hm := newHealthMonitor(tm, tunnel, nil, nil)

	// Start healthy.
	if hm.health != HealthHealthy {
		t.Fatalf("initial health: got %s, want healthy", hm.health)
	}

	// Simulate 5 consecutive high-loss samples → degraded.
	for i := 0; i < 5; i++ {
		level := hm.assess(wire.WireStats{Transport: "quic", LossRate: 0.10})
		hm.transition(level)
	}
	if hm.health != HealthDegraded {
		t.Fatalf("after 5 high-loss: got %s, want degraded", hm.health)
	}

	// Simulate 3 consecutive very-high-loss + throughput drop → critical.
	hm.peakThroughput = 100e6
	for i := 0; i < 3; i++ {
		level := hm.assess(wire.WireStats{Transport: "quic", LossRate: 0.30, Throughput: 10e6})
		hm.transition(level)
	}
	if hm.health != HealthCritical {
		t.Fatalf("after 3 very-high-loss: got %s, want critical", hm.health)
	}

	// Simulate 10 consecutive healthy samples → healthy.
	for i := 0; i < 10; i++ {
		level := hm.assess(wire.WireStats{Transport: "quic", LossRate: 0.01, Throughput: 80e6})
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

	// First action should fire.
	hm.maybeAct()
	if hm.hopAttempts != 1 {
		t.Fatalf("expected 1 hop attempt, got %d", hm.hopAttempts)
	}

	// Immediate retry should be blocked by cooldown.
	hm.maybeAct()
	if hm.hopAttempts != 1 {
		t.Fatalf("cooldown should block: expected 1, got %d", hm.hopAttempts)
	}

	// After cooldown expires, should fire again.
	hm.lastAction = time.Now().Add(-actionCooldown - time.Second)
	hm.maybeAct()
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
		level := hm.assess(wire.WireStats{Transport: "quic", LossRate: 0.01, Throughput: 80e6})
		hm.transition(level)
	}
	if hm.health != HealthHealthy {
		t.Fatalf("should be healthy after QUIC recovery, got %s", hm.health)
	}
}

func TestHealthEscalation(t *testing.T) {
	tm := mockTransportManager()
	tunnel := core.NewClientTunnel(core.ClientTunnelConfig{})

	var reconnectCalled bool
	hm := newHealthMonitor(tm, tunnel, nil, func() { reconnectCalled = true })

	// Simulate 4 hop attempts (>3 triggers escalation).
	hm.health = HealthCritical
	for i := 0; i < 4; i++ {
		hm.lastAction = time.Time{} // bypass cooldown
		hm.maybeAct()
	}

	if !reconnectCalled {
		t.Fatal("expected escalation to full reconnect after >3 hops")
	}
}

package engine

import (
	"sync/atomic"
	"testing"
	"time"
)

// mockCallbacks provides atomic counters for netCoordinator callback assertions.
type mockCallbacks struct {
	reconnects atomic.Int32
	stopProbes atomic.Int32
}

func newMockCallbacks() *mockCallbacks {
	return &mockCallbacks{}
}

func (m *mockCallbacks) onReconnect() {
	m.reconnects.Add(1)
}

func (m *mockCallbacks) onStopProbe() {
	m.stopProbes.Add(1)
}

// newTestCoord builds a netCoordinator and registers cleanup to stop any
// pending trailing timer so the test never leaks an AfterFunc goroutine.
func newTestCoord(t *testing.T, cb *mockCallbacks) *netCoordinator {
	t.Helper()
	nc := newNetCoordinator(cb.onReconnect, cb.onStopProbe)
	t.Cleanup(nc.close)
	return nc
}

func TestNetCoordinator_Unavailable_StopsReconnects(t *testing.T) {
	cb := newMockCallbacks()
	nc := newTestCoord(t, cb)

	// Network goes unavailable — should call onStopProbe.
	nc.handleEvent(&NetEvent{Signal: SignalUnavailable, Source: "test"})

	if cb.stopProbes.Load() != 1 {
		t.Fatalf("expected 1 stop probe call, got %d", cb.stopProbes.Load())
	}
	if nc.isNetworkUp() {
		t.Fatal("expected networkUp=false after unavailable")
	}

	// "changed" while network is down — should be ignored.
	nc.handleEvent(&NetEvent{Signal: SignalChanged, Source: "test"})

	if cb.reconnects.Load() != 0 {
		t.Fatalf("expected 0 reconnects when network down, got %d", cb.reconnects.Load())
	}
}

func TestNetCoordinator_Available_TriggersReconnect(t *testing.T) {
	cb := newMockCallbacks()
	nc := newTestCoord(t, cb)

	// First make network unavailable.
	nc.handleEvent(&NetEvent{Signal: SignalUnavailable, Source: "test"})

	// Then network becomes available — should call onReconnect.
	nc.handleEvent(&NetEvent{Signal: SignalAvailable, Source: "test"})

	if cb.reconnects.Load() != 1 {
		t.Fatalf("expected 1 reconnect, got %d", cb.reconnects.Load())
	}
	if !nc.isNetworkUp() {
		t.Fatal("expected networkUp=true after available")
	}
}

func TestNetCoordinator_DuplicateAvailable_Ignored(t *testing.T) {
	cb := newMockCallbacks()
	nc := newTestCoord(t, cb)

	// Network starts up. Sending "available" should be a no-op (duplicate).
	nc.handleEvent(&NetEvent{Signal: SignalAvailable, Source: "test"})

	if cb.reconnects.Load() != 0 {
		t.Fatalf("expected 0 reconnects for duplicate available, got %d", cb.reconnects.Load())
	}
}

func TestNetCoordinator_Changed_ReconnectsWhenUp(t *testing.T) {
	cb := newMockCallbacks()
	nc := newTestCoord(t, cb)

	// Network is up (default). "changed" should trigger reconnect.
	nc.handleEvent(&NetEvent{Signal: SignalChanged, Source: "test", InterfaceName: "en0"})

	if cb.reconnects.Load() != 1 {
		t.Fatalf("expected 1 reconnect, got %d", cb.reconnects.Load())
	}
}

func TestNetCoordinator_Changed_LeadingPlusTrailing(t *testing.T) {
	cb := newMockCallbacks()
	nc := newTestCoord(t, cb)

	// First "changed" fires immediately (leading edge).
	nc.handleEvent(&NetEvent{Signal: SignalChanged, Source: "test"})
	if cb.reconnects.Load() != 1 {
		t.Fatalf("expected 1 reconnect after leading, got %d", cb.reconnects.Load())
	}

	// Second "changed" within the window is throttled and scheduled
	// as a single trailing fire.
	nc.handleEvent(&NetEvent{Signal: SignalChanged, Source: "test"})
	if cb.reconnects.Load() != 1 {
		t.Fatalf("expected reconnect to stay at 1 while trailing pending, got %d", cb.reconnects.Load())
	}

	// Wait past the debounce window — trailing should have fired by now.
	time.Sleep(700 * time.Millisecond)
	if got := cb.reconnects.Load(); got != 2 {
		t.Fatalf("expected 2 reconnects after trailing flush, got %d", got)
	}
}

func TestNetCoordinator_DuplicateUnavailable_Ignored(t *testing.T) {
	cb := newMockCallbacks()
	nc := newTestCoord(t, cb)

	nc.handleEvent(&NetEvent{Signal: SignalUnavailable, Source: "test"})
	nc.handleEvent(&NetEvent{Signal: SignalUnavailable, Source: "test"})

	if cb.stopProbes.Load() != 1 {
		t.Fatalf("expected 1 stop probe call for duplicate unavailable, got %d", cb.stopProbes.Load())
	}
}

// TestNetCoordinator_RapidFlapping_BoundedTo2Reconnects exercises the worst
// real-world scenario: 10 alternating Unavailable/Available signals at 50ms
// intervals (subway / weak Wi-Fi / cellular handoff). The old netCoordinator
// fired onReconnect on every transition; with leading+trailing debounce it
// must collapse to at most 2 reconnects per window.
func TestNetCoordinator_RapidFlapping_BoundedTo2Reconnects(t *testing.T) {
	cb := newMockCallbacks()
	nc := newTestCoord(t, cb)

	// 10 events alternating starting with Unavailable, 50ms apart.
	// Span = 450ms, all within one debounce window.
	for i := 0; i < 10; i++ {
		sig := SignalUnavailable
		if i%2 == 1 {
			sig = SignalAvailable
		}
		nc.handleEvent(&NetEvent{Signal: sig, Source: "test"})
		time.Sleep(50 * time.Millisecond)
	}

	// Wait for trailing window to expire.
	time.Sleep(700 * time.Millisecond)

	if got := cb.reconnects.Load(); got > 2 {
		t.Fatalf("expected ≤2 reconnects under 10× flapping, got %d", got)
	}
	// Lower bound: at least the leading fire from the very first Available.
	if got := cb.reconnects.Load(); got < 1 {
		t.Fatalf("expected ≥1 reconnect (leading fire on first Available), got %d", got)
	}
}

// TestNetCoordinator_PendingCancelledOnUnavailable verifies that a trailing
// reconnect armed during the throttle window is cancelled when the network
// goes down — pointless to reconnect onto an interface we just learned is
// gone.
func TestNetCoordinator_PendingCancelledOnUnavailable(t *testing.T) {
	cb := newMockCallbacks()
	nc := newTestCoord(t, cb)

	// Leading fire establishes lastFire.
	nc.handleEvent(&NetEvent{Signal: SignalChanged, Source: "test"})
	if cb.reconnects.Load() != 1 {
		t.Fatalf("expected 1 reconnect after leading, got %d", cb.reconnects.Load())
	}

	// Schedule trailing.
	nc.handleEvent(&NetEvent{Signal: SignalChanged, Source: "test"})

	// Network drops before trailing fires — pending must be cancelled.
	nc.handleEvent(&NetEvent{Signal: SignalUnavailable, Source: "test"})

	// Wait past where the trailing would have fired.
	time.Sleep(700 * time.Millisecond)

	if got := cb.reconnects.Load(); got != 1 {
		t.Fatalf("expected reconnect count unchanged after Unavailable cancellation, got %d", got)
	}
	if got := cb.stopProbes.Load(); got != 1 {
		t.Fatalf("expected 1 stop-probe call, got %d", got)
	}
}

// TestNetCoordinator_NetworkUpAlwaysCurrent guards the invariant that
// isNetworkUp() reflects the most-recent signal even while throttled —
// the wake reconnect guard in engine.go depends on this.
func TestNetCoordinator_NetworkUpAlwaysCurrent(t *testing.T) {
	cb := newMockCallbacks()
	nc := newTestCoord(t, cb)

	nc.handleEvent(&NetEvent{Signal: SignalUnavailable, Source: "test"})
	if nc.isNetworkUp() {
		t.Fatal("isNetworkUp must be false right after Unavailable")
	}

	nc.handleEvent(&NetEvent{Signal: SignalAvailable, Source: "test"})
	if !nc.isNetworkUp() {
		t.Fatal("isNetworkUp must be true right after Available, throttle or not")
	}

	// Force a throttled Available (no state change so really a no-op via dedupe).
	// Use Changed to actually exercise the throttle path while up.
	nc.handleEvent(&NetEvent{Signal: SignalChanged, Source: "test"})
	if !nc.isNetworkUp() {
		t.Fatal("isNetworkUp must remain true through Changed")
	}

	nc.handleEvent(&NetEvent{Signal: SignalUnavailable, Source: "test"})
	if nc.isNetworkUp() {
		t.Fatal("isNetworkUp must be false right after second Unavailable, even though trailing was pending")
	}
}

// TestNetCoordinator_CloseStopsPendingTrailing verifies that close() prevents
// a scheduled trailing fire from running — required so engine.Stop() can fully
// quiesce the coordinator.
func TestNetCoordinator_CloseStopsPendingTrailing(t *testing.T) {
	cb := newMockCallbacks()
	nc := newNetCoordinator(cb.onReconnect, cb.onStopProbe)
	// Don't t.Cleanup here — we close explicitly below and assert no late fire.

	// Leading fire + schedule trailing.
	nc.handleEvent(&NetEvent{Signal: SignalChanged, Source: "test"})
	nc.handleEvent(&NetEvent{Signal: SignalChanged, Source: "test"})
	if cb.reconnects.Load() != 1 {
		t.Fatalf("expected 1 reconnect after leading+schedule, got %d", cb.reconnects.Load())
	}

	// Close before trailing fires.
	nc.close()

	// Wait past where trailing would have fired.
	time.Sleep(700 * time.Millisecond)
	if got := cb.reconnects.Load(); got != 1 {
		t.Fatalf("expected reconnect count unchanged after close(), got %d", got)
	}

	// Post-close events are no-ops.
	nc.handleEvent(&NetEvent{Signal: SignalChanged, Source: "test"})
	if got := cb.reconnects.Load(); got != 1 {
		t.Fatalf("expected reconnect count unchanged after post-close event, got %d", got)
	}
}

// TestNetCoordinator_DownThenUp_WithinWindow verifies the real-fast-recovery
// edge case: network drops then comes back inside a debounce window. Even if
// the leading fire was recent, the Unavailable cancels pending and the
// subsequent Available scheduling still happens — but lastFire was already
// recent, so it goes into the trailing slot. End state: exactly one further
// reconnect after the window.
func TestNetCoordinator_DownThenUp_WithinWindow(t *testing.T) {
	cb := newMockCallbacks()
	nc := newTestCoord(t, cb)

	// Leading fire to set lastFire.
	nc.handleEvent(&NetEvent{Signal: SignalChanged, Source: "test"})
	if cb.reconnects.Load() != 1 {
		t.Fatalf("expected 1 reconnect after leading, got %d", cb.reconnects.Load())
	}

	// 100ms later: real network drop.
	time.Sleep(100 * time.Millisecond)
	nc.handleEvent(&NetEvent{Signal: SignalUnavailable, Source: "test"})

	// 100ms later: real network recovery (<500ms since lastFire).
	time.Sleep(100 * time.Millisecond)
	nc.handleEvent(&NetEvent{Signal: SignalAvailable, Source: "test"})

	// Inside the window, no extra reconnect yet.
	if got := cb.reconnects.Load(); got != 1 {
		t.Fatalf("expected reconnect count still 1 inside throttle window, got %d", got)
	}

	// Wait for trailing.
	time.Sleep(500 * time.Millisecond)
	if got := cb.reconnects.Load(); got != 2 {
		t.Fatalf("expected 2 reconnects after trailing flush, got %d", got)
	}
}

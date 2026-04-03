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

func TestNetCoordinator_Unavailable_StopsReconnects(t *testing.T) {
	cb := newMockCallbacks()
	nc := newNetCoordinator(cb.onReconnect, cb.onStopProbe)

	// Network goes unavailable — should call onStopProbe.
	nc.handleEvent(&NetEvent{Signal: SignalUnavailable, Source: "test"})

	if cb.stopProbes.Load() != 1 {
		t.Fatalf("expected 1 stop probe call, got %d", cb.stopProbes.Load())
	}
	if !nc.isNetworkUp() == true {
		// networkUp should be false
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
	nc := newNetCoordinator(cb.onReconnect, cb.onStopProbe)

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
	nc := newNetCoordinator(cb.onReconnect, cb.onStopProbe)

	// Network starts up. Sending "available" should be a no-op (duplicate).
	nc.handleEvent(&NetEvent{Signal: SignalAvailable, Source: "test"})

	if cb.reconnects.Load() != 0 {
		t.Fatalf("expected 0 reconnects for duplicate available, got %d", cb.reconnects.Load())
	}
}

func TestNetCoordinator_Changed_ReconnectsWhenUp(t *testing.T) {
	cb := newMockCallbacks()
	nc := newNetCoordinator(cb.onReconnect, cb.onStopProbe)

	// Network is up (default). "changed" should trigger reconnect.
	nc.handleEvent(&NetEvent{Signal: SignalChanged, Source: "test", InterfaceName: "en0"})

	if cb.reconnects.Load() != 1 {
		t.Fatalf("expected 1 reconnect, got %d", cb.reconnects.Load())
	}
}

func TestNetCoordinator_Changed_DebounceRapidFire(t *testing.T) {
	cb := newMockCallbacks()
	nc := newNetCoordinator(cb.onReconnect, cb.onStopProbe)

	// First "changed" should go through.
	nc.handleEvent(&NetEvent{Signal: SignalChanged, Source: "test"})

	// Second "changed" immediately after — should be debounced.
	nc.handleEvent(&NetEvent{Signal: SignalChanged, Source: "test"})

	if cb.reconnects.Load() != 1 {
		t.Fatalf("expected 1 reconnect after debounce, got %d", cb.reconnects.Load())
	}

	// Wait for debounce window to pass, then "changed" should go through again.
	time.Sleep(600 * time.Millisecond)
	nc.handleEvent(&NetEvent{Signal: SignalChanged, Source: "test"})

	if cb.reconnects.Load() != 2 {
		t.Fatalf("expected 2 reconnects after debounce window, got %d", cb.reconnects.Load())
	}
}

func TestNetCoordinator_DuplicateUnavailable_Ignored(t *testing.T) {
	cb := newMockCallbacks()
	nc := newNetCoordinator(cb.onReconnect, cb.onStopProbe)

	nc.handleEvent(&NetEvent{Signal: SignalUnavailable, Source: "test"})
	nc.handleEvent(&NetEvent{Signal: SignalUnavailable, Source: "test"})

	if cb.stopProbes.Load() != 1 {
		t.Fatalf("expected 1 stop probe call for duplicate unavailable, got %d", cb.stopProbes.Load())
	}
}

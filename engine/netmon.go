package engine

import (
	"log/slog"
	"time"

	"github.com/sasha-s/go-deadlock"
)

// Signal constants for NetEvent.Signal.
const (
	SignalAvailable   = "available"
	SignalUnavailable = "unavailable"
	SignalChanged     = "changed"
)

// NetEvent is a network state signal from platform or interface monitors.
// All fields are primitive types for gomobile compatibility.
type NetEvent struct {
	Signal        string // "available", "unavailable", "changed"
	InterfaceName string // e.g. "en0", "pdp_ip0"
	InterfaceIndex int
	IsWifi        bool
	IsCellular    bool
	HasIPv4       bool
	HasIPv6       bool
	Source        string // e.g. "sing-tun", "NWPathMonitor", "ConnectivityManager"
}

// netCoordinator fuses network signals from multiple sources (sing-tun
// interface monitor, iOS NWPathMonitor, Android ConnectivityManager) into
// engine reconnect/stop decisions. It debounces rapid "changed" signals
// and deduplicates state transitions.
type netCoordinator struct {
	mu         deadlock.Mutex
	networkUp  bool
	lastSignal time.Time

	onReconnect func()
	onStopProbe func()
}

const debounceInterval = 500 * time.Millisecond

// newNetCoordinator creates a netCoordinator with the given callbacks.
// networkUp defaults to true (assume network is available at start).
func newNetCoordinator(onReconnect, onStopProbe func()) *netCoordinator {
	return &netCoordinator{
		networkUp:   true,
		onReconnect: onReconnect,
		onStopProbe: onStopProbe,
	}
}

// handleEvent processes a network state signal. Callbacks are called OUTSIDE the lock.
func (nc *netCoordinator) handleEvent(event *NetEvent) {
	nc.mu.Lock()

	switch event.Signal {
	case SignalUnavailable:
		if !nc.networkUp {
			// Already down — duplicate, ignore.
			slog.Debug("netmon: duplicate unavailable, ignoring", "source", event.Source)
			nc.mu.Unlock()
			return
		}
		nc.networkUp = false
		nc.mu.Unlock()

		slog.Warn("netmon: network unavailable", "source", event.Source)
		if nc.onStopProbe != nil {
			nc.onStopProbe()
		}

	case SignalAvailable:
		if nc.networkUp {
			// Already up — duplicate, ignore.
			slog.Debug("netmon: duplicate available, ignoring", "source", event.Source)
			nc.mu.Unlock()
			return
		}
		nc.networkUp = true
		nc.mu.Unlock()

		slog.Info("netmon: network available", "source", event.Source)
		if nc.onReconnect != nil {
			nc.onReconnect()
		}

	case SignalChanged:
		if !nc.networkUp {
			// Network is down — changed signal is meaningless, ignore.
			slog.Debug("netmon: changed while network down, ignoring", "source", event.Source)
			nc.mu.Unlock()
			return
		}
		now := time.Now()
		if now.Sub(nc.lastSignal) < debounceInterval {
			// Too soon after last signal — debounce.
			slog.Debug("netmon: changed debounced", "source", event.Source)
			nc.mu.Unlock()
			return
		}
		nc.lastSignal = now
		nc.mu.Unlock()

		slog.Info("netmon: network changed", "source", event.Source, "interface", event.InterfaceName)
		if nc.onReconnect != nil {
			nc.onReconnect()
		}

	default:
		nc.mu.Unlock()
		slog.Warn("netmon: unknown signal", "signal", event.Signal, "source", event.Source)
	}
}

// isNetworkUp returns the current network state. Thread-safe.
func (nc *netCoordinator) isNetworkUp() bool {
	nc.mu.Lock()
	defer nc.mu.Unlock()
	return nc.networkUp
}

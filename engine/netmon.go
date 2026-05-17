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
	Signal         string // "available", "unavailable", "changed"
	InterfaceName  string // e.g. "en0", "pdp_ip0"
	InterfaceIndex int
	IsWifi         bool
	IsCellular     bool
	HasIPv4        bool
	HasIPv6        bool
	Source         string // e.g. "sing-tun", "NWPathMonitor", "ConnectivityManager"
}

// netCoordinator fuses network signals from multiple sources (sing-tun
// interface monitor, iOS NWPathMonitor, Android ConnectivityManager) into
// engine reconnect/stop decisions.
//
// State commit vs callback firing:
//   - networkUp is updated synchronously on every event so isNetworkUp()
//     always reflects the latest signal (consumed by wake reconnect guard).
//   - onReconnect firing is throttled via leading+trailing debounce: the first
//     reconnect-triggering event fires immediately, subsequent events within
//     the debounce window are coalesced into one trailing fire at window end.
//   - onStopProbe fires immediately on every real Unavailable transition
//     (rare in practice — once per session if at all). Any pending trailing
//     reconnect is cancelled when the network goes down.
//
// Worst-case callback rate under sustained flapping: 2 onReconnect per window
// (1 leading + 1 trailing). Adversarial 10× alternation in 500ms collapses to
// 2 reconnects, not 10.
type netCoordinator struct {
	mu        deadlock.Mutex
	networkUp bool
	closed    bool

	// lastFire is the most-recent onReconnect fire time (leading or trailing).
	// Zero value means "never fired" — treated as outside the window.
	lastFire time.Time

	// Trailing-fire bookkeeping. pendingFire is set when at least one
	// reconnect-triggering event was suppressed inside the debounce window.
	// pendingTimer is the single armed AfterFunc; nil when no trailing scheduled.
	// pendingDesc captures the most recent suppressed event for logging only
	// (last-write-wins).
	pendingFire  bool
	pendingDesc  string
	pendingTimer *time.Timer

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

// handleEvent processes a network state signal. Callbacks are invoked
// OUTSIDE the lock to satisfy the engine's no-external-calls-under-lock rule.
func (nc *netCoordinator) handleEvent(event *NetEvent) {
	nc.mu.Lock()
	if nc.closed {
		nc.mu.Unlock()
		return
	}

	switch event.Signal {
	case SignalUnavailable:
		if !nc.networkUp {
			nc.mu.Unlock()
			slog.Debug("netmon: duplicate unavailable, ignoring", "source", event.Source)
			return
		}
		nc.networkUp = false
		// Cancel any pending trailing reconnect — pointless when network is down.
		nc.cancelPendingLocked()
		nc.mu.Unlock()
		slog.Warn("netmon: network unavailable", "source", event.Source)
		if nc.onStopProbe != nil {
			nc.onStopProbe()
		}

	case SignalAvailable:
		if nc.networkUp {
			nc.mu.Unlock()
			slog.Debug("netmon: duplicate available, ignoring", "source", event.Source)
			return
		}
		nc.networkUp = true
		nc.scheduleReconnectAndUnlock("available source=" + event.Source)

	case SignalChanged:
		if !nc.networkUp {
			nc.mu.Unlock()
			slog.Debug("netmon: changed while network down, ignoring", "source", event.Source)
			return
		}
		nc.scheduleReconnectAndUnlock("changed source=" + event.Source + " iface=" + event.InterfaceName)

	default:
		nc.mu.Unlock()
		slog.Warn("netmon: unknown signal", "signal", event.Signal, "source", event.Source)
	}
}

// scheduleReconnectAndUnlock decides leading vs trailing fire.
// Caller MUST hold nc.mu; this function releases it before firing any callback.
//
// Leading: outside throttle window, fire immediately + record timestamp.
// Trailing: inside window, mark pendingFire and arm a one-shot timer that fires
// at the original window deadline. If a timer is already armed it is reused
// — subsequent events keep the same firing time but update pendingDesc.
func (nc *netCoordinator) scheduleReconnectAndUnlock(desc string) {
	now := time.Now()
	if nc.lastFire.IsZero() || now.Sub(nc.lastFire) >= debounceInterval {
		nc.lastFire = now
		nc.cancelPendingLocked()
		nc.mu.Unlock()
		slog.Info("netmon: reconnect", "trigger", desc)
		if nc.onReconnect != nil {
			nc.onReconnect()
		}
		return
	}

	nc.pendingFire = true
	nc.pendingDesc = desc
	if nc.pendingTimer == nil {
		delay := debounceInterval - now.Sub(nc.lastFire)
		nc.pendingTimer = time.AfterFunc(delay, nc.flushTrailing)
	}
	nc.mu.Unlock()
	slog.Debug("netmon: reconnect throttled, trailing scheduled", "trigger", desc)
}

// cancelPendingLocked clears any pending trailing fire. Caller holds nc.mu.
// Timer.Stop is best-effort: if the AfterFunc goroutine has already started,
// flushTrailing will observe pendingFire=false and bail out.
func (nc *netCoordinator) cancelPendingLocked() {
	if nc.pendingTimer != nil {
		nc.pendingTimer.Stop()
		nc.pendingTimer = nil
	}
	nc.pendingFire = false
	nc.pendingDesc = ""
}

// flushTrailing is invoked by AfterFunc on its own goroutine when the debounce
// window expires with a pending reconnect. It re-checks state under lock —
// the event may have been cancelled (network down, close()) or pre-empted by
// a fresh leading fire (race between Stop and AfterFunc dispatch).
func (nc *netCoordinator) flushTrailing() {
	nc.mu.Lock()
	nc.pendingTimer = nil
	if nc.closed || !nc.pendingFire || !nc.networkUp {
		nc.pendingFire = false
		nc.pendingDesc = ""
		nc.mu.Unlock()
		return
	}
	nc.pendingFire = false
	desc := nc.pendingDesc
	nc.pendingDesc = ""
	nc.lastFire = time.Now()
	nc.mu.Unlock()
	slog.Info("netmon: reconnect (trailing)", "trigger", desc)
	if nc.onReconnect != nil {
		nc.onReconnect()
	}
}

// close stops any pending trailing fire and prevents future callbacks. Called
// from engine.Stop / failure-cleanup paths after the netCoord reference has
// been extracted and the engine lock released.
func (nc *netCoordinator) close() {
	nc.mu.Lock()
	nc.closed = true
	nc.cancelPendingLocked()
	nc.mu.Unlock()
}

// isNetworkUp returns the current network state. Thread-safe.
func (nc *netCoordinator) isNetworkUp() bool {
	nc.mu.Lock()
	defer nc.mu.Unlock()
	return nc.networkUp
}

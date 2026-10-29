package engine

// EventHandler receives lifecycle events from the Engine.
// OnStatus delivers a typed Status snapshot on every state change.
// OnStats delivers traffic counters (high-frequency, separate concern).
// OnOutboundFatal fires when an outbound has exhausted its replacement options
// (i.e. the OutboundProvider returned an error). The engine will have already
// called e.fail() before firing; this callback is informational for handlers
// that want to surface which outbound finally died.
type EventHandler interface {
	OnStatus(status Status)
	OnStats(txBytes, rxBytes int64)
	OnOutboundFatal(outbound string, err error)
}

// Engine states.
const (
	StateDisconnected = "disconnected"
	StateConnecting   = "connecting"
	StateConnected    = "connected"
	StatePaused       = "paused"       // handler-only transient; engine.state stays "connected"
	StateReconnecting = "reconnecting" // handler-only transient; engine.state stays "connected"
)

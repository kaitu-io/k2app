package engine

// EventHandler receives lifecycle events from the Engine.
// OnStatus delivers a typed Status snapshot on every state change.
// OnStats delivers traffic counters (high-frequency, separate concern).
type EventHandler interface {
	OnStatus(status Status)
	OnStats(txBytes, rxBytes int64)
}

// Engine states.
const (
	StateDisconnected = "disconnected"
	StateConnecting   = "connecting"
	StateConnected    = "connected"
	StatePaused       = "paused"       // handler-only transient; engine.state stays "connected"
	StateReconnecting = "reconnecting" // handler-only transient; engine.state stays "connected"
)

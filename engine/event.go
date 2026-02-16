package engine

// EventHandler receives lifecycle events from the Engine.
// All methods use simple parameter types for gomobile compatibility.
type EventHandler interface {
	OnStateChange(state string)
	OnError(message string)
	OnStats(txBytes, rxBytes int64)
}

// Engine states.
const (
	StateDisconnected = "disconnected"
	StateConnecting   = "connecting"
	StateConnected    = "connected"
)

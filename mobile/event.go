package mobile

// EventHandler receives lifecycle events from the Engine.
// All methods use gomobile-compatible parameter types (string, int64).
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

package appext

import (
	"github.com/kaitu-io/k2/engine"
)

// EventHandler receives lifecycle events from the Engine.
// All methods use gomobile-compatible parameter types (string, int64).
type EventHandler interface {
	OnStateChange(state string)
	OnError(message string)
	OnStats(txBytes, rxBytes int64)
}

// Engine states — re-exported from engine package for gomobile binding.
const (
	StateDisconnected = engine.StateDisconnected
	StateConnecting   = engine.StateConnecting
	StateConnected    = engine.StateConnected
)

// eventBridge adapts appext.EventHandler to engine.EventHandler.
type eventBridge struct {
	handler EventHandler
}

func (b *eventBridge) OnStateChange(state string) { b.handler.OnStateChange(state) }
func (b *eventBridge) OnError(message string)     { b.handler.OnError(message) }
func (b *eventBridge) OnStats(tx, rx int64)       { b.handler.OnStats(tx, rx) }

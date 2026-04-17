package appext

import (
	"encoding/json"

	"github.com/kaitu-io/k2/engine"
)

// EventHandler receives lifecycle events from the Engine.
// All methods use gomobile-compatible parameter types (string, int64).
type EventHandler interface {
	OnStatus(statusJSON string)
	OnStats(txBytes, rxBytes int64)
}

// Engine states — re-exported from engine package for gomobile binding.
const (
	StateDisconnected = engine.StateDisconnected
	StateConnecting   = engine.StateConnecting
	StateConnected    = engine.StateConnected
	StatePaused       = engine.StatePaused
	StateReconnecting = engine.StateReconnecting
)

// eventBridge adapts appext.EventHandler to engine.EventHandler.
// Converts typed engine.Status to JSON string for gomobile compatibility.
type eventBridge struct {
	handler EventHandler
}

func (b *eventBridge) OnStatus(status engine.Status) {
	data, _ := json.Marshal(status)
	b.handler.OnStatus(string(data))
}

func (b *eventBridge) OnStats(tx, rx int64) { b.handler.OnStats(tx, rx) }

// OnOutboundFatal implements engine.EventHandler. Mobile/sysext shells do
// not install OutboundProvider (k2subs:// Subscription machinery lives in
// the daemon, not in appext), so this callback never fires in practice.
// Left as a no-op to keep the gomobile-exported EventHandler surface
// stable; if/when mobile grows its own subscription system, add a
// matching method to the gomobile EventHandler and forward here.
func (b *eventBridge) OnOutboundFatal(outbound string, err error) {}

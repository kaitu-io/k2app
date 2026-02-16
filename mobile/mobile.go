package mobile

import (
	"github.com/kaitu-io/k2/engine"
)

// Engine is the mobile VPN engine. It delegates all tunnel assembly and
// lifecycle management to engine.Engine, exposing only the gomobile-compatible
// API surface (string, int, int64 params only).
type Engine struct {
	inner *engine.Engine
}

// NewEngine creates a new Engine in the disconnected state.
func NewEngine() *Engine {
	return &Engine{
		inner: engine.New(),
	}
}

// SetEventHandler registers a callback receiver for state changes,
// errors, and traffic stats. Pass nil to clear.
func (e *Engine) SetEventHandler(h EventHandler) {
	if h == nil {
		e.inner.SetEventHandler(nil)
		return
	}
	e.inner.SetEventHandler(&eventBridge{handler: h})
}

// Status returns the current engine state.
func (e *Engine) Status() string {
	return e.inner.Status()
}

// Start assembles the tunnel stack and begins forwarding traffic.
// url is a k2v5:// connection URL; fd is the TUN file descriptor
// provided by the platform (iOS NEPacketTunnelProvider / Android VpnService);
// dataDir is the k2rule cache directory.
func (e *Engine) Start(url string, fd int, dataDir string) error {
	return e.inner.Start(engine.Config{
		WireURL:        url,
		FileDescriptor: fd,
		DataDir:        dataDir,
	})
}

// Stop tears down the tunnel and returns the engine to disconnected.
func (e *Engine) Stop() error {
	return e.inner.Stop()
}

// StatusJSON returns the engine status as a JSON string.
// The format matches daemon.statusInfo() for consistency.
func (e *Engine) StatusJSON() string {
	return e.inner.StatusJSON()
}

package appext

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/engine"
)

func init() {
	// Enable debug-level logging for mobile builds.
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})))
}

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
// configJSON is a JSON-encoded config.ClientConfig; fd is the TUN file
// descriptor provided by the platform (iOS NEPacketTunnelProvider /
// Android VpnService); cfg provides platform-specific paths (CacheDir, LogDir).
func (e *Engine) Start(configJSON string, fd int, cfg *EngineConfig) error {
	if cfg == nil {
		return fmt.Errorf("engine: EngineConfig is required")
	}

	var clientCfg config.ClientConfig
	if err := json.Unmarshal([]byte(configJSON), &clientCfg); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}
	config.SetDefaults(&clientCfg)

	// Inject platform cache dir for k2rule.
	if cfg.CacheDir != "" {
		clientCfg.Rule.CacheDir = filepath.Join(cfg.CacheDir, "k2rule")
	}

	ecfg := engine.Config{
		Client:         &clientCfg,
		FileDescriptor: fd,
	}
	if cfg.SocketProtector != nil {
		p := cfg.SocketProtector
		ecfg.ProtectFunc = func(fd int) bool {
			if fd < 0 || fd > math.MaxInt32 {
				slog.Warn("socket protect: fd out of int32 range", "fd", fd)
				return false
			}
			ok := p.Protect(int32(fd))
			slog.Debug("appext: SocketProtector.Protect()", "fd", fd, "ok", ok)
			return ok
		}
	}
	return e.inner.Start(ecfg)
}

// Stop tears down the tunnel and returns the engine to disconnected.
func (e *Engine) Stop() error {
	return e.inner.Stop()
}

// OnNetworkChanged notifies the engine of a network change.
// Call from platform layer (Android NetworkCallback, iOS NWPathMonitor).
func (e *Engine) OnNetworkChanged() {
	e.inner.OnNetworkChanged()
}

// StatusJSON returns the engine status as a JSON string.
// The format matches daemon.statusInfo() for consistency.
func (e *Engine) StatusJSON() string {
	return e.inner.StatusJSON()
}

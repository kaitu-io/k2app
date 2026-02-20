package mobile

import (
	"encoding/json"
	"fmt"

	"github.com/kaitu-io/k2/config"
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
// configJSON is a JSON-encoded config.ClientConfig; fd is the TUN file
// descriptor provided by the platform (iOS NEPacketTunnelProvider /
// Android VpnService); dataDir is the k2rule cache directory.
func (e *Engine) Start(configJSON string, fd int, dataDir string) error {
	var cfg config.ClientConfig
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}
	config.SetDefaults(&cfg)

	// Map rule config to engine RuleMode.
	ruleMode := "global"
	if !cfg.Rule.Global {
		ruleMode = "smart"
	}

	return e.inner.Start(engine.Config{
		WireURL:          cfg.Server,
		FileDescriptor:   fd,
		DataDir:          dataDir,
		RuleMode:         ruleMode,
		TunIPv4:          cfg.Tun.IPv4,
		TunIPv6:          cfg.Tun.IPv6,
		DNSProxyServers:  cfg.DNS.Proxy,
		DNSDirectServers: cfg.DNS.Direct,
	})
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

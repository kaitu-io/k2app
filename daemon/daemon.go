package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	tun "github.com/sagernet/sing-tun"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/directdial"
	"github.com/kaitu-io/k2/engine"
)

// DefaultAddr is the daemon HTTP listen address.
const DefaultAddr = "127.0.0.1:1777"

// Connection states.
const (
	StateStopped    = "stopped"
	StateConnecting = "connecting"
	StateConnected  = "connected"
)

// Daemon is the k2 background service.
// It manages the VPN tunnel lifecycle and exposes an HTTP API.
//
// Concurrency model:
//   - opMu serializes doUp/doDown (coarse lifecycle lock)
//   - mu protects field reads/writes (fine-grained data lock)
type Daemon struct {
	opMu sync.Mutex   // serializes doUp/doDown operations
	mu   sync.RWMutex // protects field access

	state string

	// Last config used for auto-reconnect.
	lastConfig *config.ClientConfig

	// Error from last connection attempt (orthogonal to state).
	lastError *engine.EngineError

	// Connected at timestamp.
	connectedAt time.Time

	// Process monitor PID.
	monitorPID int

	// Active engine and its cancel function for PID monitoring.
	engine    *engine.Engine
	engineCtx context.Context
	engineCnl context.CancelFunc

	// ConfigPath is the path to the YAML config file loaded at startup.
	// When set, Reload() re-reads this file and applies dynamic field changes.
	ConfigPath string

	// EngineStarter allows overriding engine creation for testing.
	// It should create an engine, start it with the given config, and return it.
	// When nil, the default production path (engine.New + Start) is used.
	EngineStarter func(cfg engine.Config) (*engine.Engine, error)

	// MonitorFactory creates the network change monitor.
	// Returns (notifier, interfaceMonitor, error).
	// When nil, the default NewNetworkMonitor is used.
	// Overridable for testing.
	MonitorFactory func() (engine.NetworkChangeNotifier, any, error)
}

// New creates a new daemon.
func New() *Daemon {
	return &Daemon{
		state: StateStopped,
	}
}

// Run starts the daemon HTTP server and blocks until ctx is cancelled.
// If listenAddr is empty, DefaultAddr is used.
func (d *Daemon) Run(ctx context.Context, listenAddr string) error {
	if listenAddr == "" {
		listenAddr = DefaultAddr
	}
	slog.Info("k2 daemon starting", "addr", listenAddr)

	mux := http.NewServeMux()
	mux.HandleFunc("/ping", d.handlePing)
	mux.HandleFunc("/metrics", d.handleMetrics)
	mux.HandleFunc("/api/core", d.handleCore)

	srv := &http.Server{
		Addr:    listenAddr,
		Handler: corsMiddleware(mux),
	}

	// Load persisted state and auto-reconnect if applicable.
	d.tryAutoReconnect(ctx)

	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	slog.Info("k2 daemon ready")

	select {
	case <-ctx.Done():
		slog.Info("daemon shutting down")
		d.doDown()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
		return ctx.Err()
	case err := <-errCh:
		return fmt.Errorf("http server: %w", err)
	}
}

// State returns the current connection state.
func (d *Daemon) State() string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.state
}

// setStateLocked sets state. Caller must hold mu.
func (d *Daemon) setStateLocked(s string) {
	slog.Info("state transition", "from", d.state, "to", s)
	d.state = s
}

// closeTunnel tears down the active engine. Caller must hold opMu.
// Idempotent: no-op if engine is nil.
func (d *Daemon) closeTunnel() {
	d.mu.Lock()
	eng := d.engine
	cancel := d.engineCnl
	d.engine = nil
	d.engineCnl = nil
	d.engineCtx = nil
	d.monitorPID = 0
	d.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if eng != nil {
		eng.Stop()
	}

	d.mu.Lock()
	d.setStateLocked(StateStopped)
	d.mu.Unlock()

	saveState(persistedState{State: StateStopped, Timestamp: time.Now()})
}

// doUp connects the tunnel with the given ClientConfig.
func (d *Daemon) doUp(cfg *config.ClientConfig, pid int) error {
	d.opMu.Lock()
	defer d.opMu.Unlock()

	// Unconditionally tear down any existing tunnel (idempotent).
	d.closeTunnel()

	d.mu.Lock()
	d.setStateLocked(StateConnecting)
	d.lastConfig = cfg
	d.monitorPID = pid
	d.lastError = nil
	d.mu.Unlock()

	// Persist state for auto-reconnect across restarts.
	saveState(persistedState{
		Config:    cfg,
		State:     StateConnecting,
		Timestamp: time.Now(),
	})

	// Create network monitor first — directdial uses it for dynamic interface tracking.
	factory := d.MonitorFactory
	if factory == nil {
		factory = defaultMonitorFactory
	}
	monitor, ifaceMon, monErr := factory()

	// Create directdial with monitor for dynamic interface tracking.
	// When monitor is unavailable, dd stays nil — engine skips interface binding.
	var dd *directdial.Dialer
	if dim, ok := ifaceMon.(tun.DefaultInterfaceMonitor); ok && monErr == nil {
		dd = directdial.New(dim)
		if dd.InterfaceName() != "" {
			slog.Info("tunnel: direct dialer bound", "interface", dd.InterfaceName())
		}
	} else if monErr != nil {
		slog.Warn("network monitor unavailable, no interface binding", "err", monErr)
	}

	engineCfg := d.engineConfigFromClientConfig(cfg, dd, monitor, ifaceMon, monErr)

	// Create and start engine.
	starter := d.EngineStarter
	if starter == nil {
		starter = defaultEngineStarter
	}
	eng, err := starter(engineCfg)
	if err != nil {
		d.mu.Lock()
		d.lastError = engine.ClassifyError(err)
		d.setStateLocked(StateStopped)
		d.mu.Unlock()
		slog.Error("engine start failed", "err", err)
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())

	d.mu.Lock()
	d.engine = eng
	d.engineCtx = ctx
	d.engineCnl = cancel
	d.connectedAt = time.Now()
	d.setStateLocked(StateConnected)
	d.mu.Unlock()

	// Start PID monitoring if requested.
	// Uses engineCtx so the goroutine stops when closeTunnel() cancels the context.
	if pid > 0 {
		go d.monitorProcess(ctx, pid)
	}

	// Update persisted state.
	saveState(persistedState{
		Config:    cfg,
		State:     StateConnected,
		Timestamp: time.Now(),
	})

	return nil
}

// defaultEngineStarter is the production engine creation path.
func defaultEngineStarter(cfg engine.Config) (*engine.Engine, error) {
	eng := engine.New()
	if err := eng.Start(cfg); err != nil {
		return nil, err
	}
	return eng, nil
}

// defaultMonitorFactory wraps NewNetworkMonitor with the any return type
// expected by MonitorFactory.
func defaultMonitorFactory() (engine.NetworkChangeNotifier, any, error) {
	return NewNetworkMonitor()
}

// engineConfigFromClientConfig maps a config.ClientConfig to an engine.Config.
// monitor/ifaceMon/monErr are the pre-created network monitor results (created in doUp
// before directdial so the same monitor can be shared).
func (d *Daemon) engineConfigFromClientConfig(
	cfg *config.ClientConfig,
	dd *directdial.Dialer,
	monitor engine.NetworkChangeNotifier,
	ifaceMon any,
	monErr error,
) engine.Config {
	// Ensure CacheDir has a value for k2rule.
	if cfg.Rule.CacheDir == "" {
		cfg.Rule.CacheDir = filepath.Join(cacheDir(), "k2rule")
	}

	ecfg := engine.Config{
		Client:         cfg,
		FileDescriptor: -1, // Desktop: engine creates its own TUN.
		DirectDialer:   dd,
	}

	// Apply network monitor if available.
	if monErr == nil {
		ecfg.NetworkMonitor = monitor
		ecfg.InterfaceMonitor = ifaceMon
	}

	return ecfg
}

// doDown disconnects the tunnel.
func (d *Daemon) doDown() {
	d.opMu.Lock()
	defer d.opMu.Unlock()
	d.closeTunnel()
}

// tryAutoReconnect checks persisted state and reconnects if applicable.
// ctx is the daemon's lifecycle context — reconnect is aborted if ctx is cancelled.
func (d *Daemon) tryAutoReconnect(ctx context.Context) {
	st, err := loadState()
	if err != nil || st.State != StateConnected {
		return
	}

	// Only auto-reconnect if disconnected less than 1 hour ago.
	if time.Since(st.Timestamp) > 1*time.Hour {
		slog.Info("auto-reconnect: stale state, skipping", "age", ">1h")
		return
	}

	if st.Config == nil {
		slog.Info("auto-reconnect: no config in saved state, skipping")
		return
	}

	slog.Info("auto-reconnect: reconnecting with saved config")
	go func() {
		// Delay 5s to let daemon fully start; abort if daemon shuts down.
		select {
		case <-time.After(5 * time.Second):
		case <-ctx.Done():
			slog.Info("auto-reconnect: cancelled during startup delay")
			return
		}
		// Skip if already connected (e.g. user called up during delay).
		if d.State() == StateConnected {
			return
		}
		if err := d.doUp(st.Config, 0); err != nil {
			slog.Error("auto-reconnect failed", "err", err)
		}
	}()
}

// statusInfo returns current status as a map.
func (d *Daemon) statusInfo() map[string]any {
	d.mu.RLock()
	defer d.mu.RUnlock()

	info := map[string]any{
		"state": d.state,
	}
	if d.state == StateConnected {
		info["connected_at"] = d.connectedAt.Format(time.RFC3339)
		info["uptime_seconds"] = int(time.Since(d.connectedAt).Seconds())
	}
	if d.lastError != nil {
		info["error"] = d.lastError
	}
	if d.lastConfig != nil {
		info["config"] = d.lastConfig
	}
	return info
}

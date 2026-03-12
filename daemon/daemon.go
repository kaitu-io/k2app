package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/sasha-s/go-deadlock"

	tun "github.com/sagernet/sing-tun"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/engine"
	"github.com/kaitu-io/k2/provider"
	"github.com/kaitu-io/k2/safego"
)

// DefaultAddr is the daemon HTTP listen address.
const DefaultAddr = "127.0.0.1:1777"

// Daemon is the k2 background service.
// It manages the VPN tunnel lifecycle and exposes an HTTP API.
//
// State is event-driven: engine fires OnStatus → daemon updates lastStatus.
// Engine is the single source of truth for connection state.
//
// Concurrency model:
//   - opMu serializes doUp/doDown (coarse lifecycle lock)
//   - abortUp signals doUp to abort after engine.Start returns (set by doDown before opMu)
//   - mu protects field reads/writes (fine-grained data lock)
//   - sseMu protects SSE client map
type Daemon struct {
	opMu    deadlock.Mutex   // serializes doUp/doDown operations
	abortUp atomic.Bool      // doDown sets before opMu.Lock; doUp checks after Start
	mu      deadlock.RWMutex // protects field access

	// lastStatus is updated by engine's OnStatus callback — single source of truth.
	lastStatus engine.Status

	// Last config used for auto-reconnect.
	lastConfig *config.ClientConfig

	// Process monitor PID.
	monitorPID int

	// Active engine and its cancel function for PID monitoring.
	engine    *engine.Engine
	engineCtx context.Context
	engineCnl context.CancelFunc

	// Version and Commit are set by main via ldflags.
	// Used by handleVersion to report the running binary's identity.
	Version string
	Commit  string

	// ConfigPath is the path to the YAML config file loaded at startup.
	// When set, Reload() re-reads this file and applies dynamic field changes.
	ConfigPath string

	// StartFunc allows overriding engine start for testing.
	// It receives a pre-created engine (with EventHandler already set)
	// and should start it with the given config.
	// When nil, the default production path (eng.Start) is used.
	StartFunc func(eng *engine.Engine, cfg engine.Config) error

	// MonitorFactory creates the network change monitor.
	// Returns (notifier, interfaceMonitor, error).
	// When nil, the default NewNetworkMonitor is used.
	// Overridable for testing.
	MonitorFactory func() (engine.NetworkChangeNotifier, any, error)

	// SSE client channels for real-time status push.
	sseClients map[chan string]struct{}
	sseMu      deadlock.RWMutex
}

// New creates a new daemon.
func New() *Daemon {
	return &Daemon{
		lastStatus: engine.Status{State: engine.StateDisconnected},
		sseClients: make(map[chan string]struct{}),
	}
}

// Run starts the daemon HTTP server and blocks until ctx is cancelled.
// If listenAddr is empty, DefaultAddr is used.
func (d *Daemon) Run(ctx context.Context, listenAddr string) error {
	if listenAddr == "" {
		listenAddr = DefaultAddr
	}
	slog.Info("k2 daemon starting", "addr", listenAddr, "version", d.Version, "commit", d.Commit)
	slog.Info("daemon: runtime",
		"pid", os.Getpid(),
		"go", runtime.Version(),
		"os", runtime.GOOS,
		"arch", runtime.GOARCH,
	)

	// Clean stale DNS override from previous crash (kill -9, power loss).
	provider.CleanupStaleDNSOverride()

	mux := http.NewServeMux()
	mux.HandleFunc("/ping", d.handlePing)
	mux.HandleFunc("/metrics", d.handleMetrics)
	mux.HandleFunc("/api/core", d.handleCore)
	mux.HandleFunc("/api/log-level", d.handleLogLevel)
	mux.HandleFunc("/api/events", d.handleEvents)
	mux.HandleFunc("/api/stats", d.handleStats)
	mux.HandleFunc("/stats", d.handleStatsPage)

	srv := &http.Server{
		Addr:    listenAddr,
		Handler: corsMiddleware(mux),
	}

	// Load persisted state and auto-reconnect if applicable.
	d.tryAutoReconnect(ctx)

	errCh := make(chan error, 1)
	safego.Go(func() {
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			errCh <- err
		}
	})

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
	return d.lastStatus.State
}

// closeTunnel tears down the active engine. Caller must hold opMu.
// Idempotent: no-op if engine is nil.
// Engine.Stop() fires OnStatus(disconnected) → updates d.lastStatus automatically.
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
		eng.Stop() // fires OnStatus(disconnected) → d.lastStatus updated via EventHandler
	}

	saveState(persistedState{State: engine.StateDisconnected, Timestamp: time.Now()})
}

// doUp connects the tunnel with the given ClientConfig.
// Engine is created with EventHandler(d) set BEFORE Start, so the daemon
// receives all state transitions (connecting, connected, disconnected+error).
func (d *Daemon) doUp(cfg *config.ClientConfig, pid int) error {
	d.opMu.Lock()
	defer d.opMu.Unlock()
	d.abortUp.Store(false) // reset on new up
	slog.Info("daemon: doUp", "mode", cfg.Mode, "pid", pid)

	// Unconditionally tear down any existing tunnel (idempotent).
	d.closeTunnel()

	d.mu.Lock()
	d.lastConfig = cfg
	d.monitorPID = pid
	d.mu.Unlock()

	// Persist state for auto-reconnect across restarts.
	saveState(persistedState{
		Config:    cfg,
		State:     engine.StateConnecting,
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
	var dd *directDialer
	if dim, ok := ifaceMon.(tun.DefaultInterfaceMonitor); ok && monErr == nil {
		dd = newDirectDialer(dim)
		if dd.interfaceName() != "" {
			slog.Info("tunnel: direct dialer bound", "interface", dd.interfaceName())
		}
	} else if monErr != nil {
		slog.Warn("network monitor unavailable, no interface binding", "err", monErr)
	}

	engineCfg := d.engineConfigFromClientConfig(cfg, dd, monitor, ifaceMon, monErr)

	// Create engine and set EventHandler BEFORE start — daemon receives all events.
	eng := engine.New()
	eng.SetEventHandler(d)

	// Early abort: doDown arrived while we were setting up (before expensive Start).
	if d.abortUp.Load() {
		slog.Info("daemon: doUp aborted before start")
		return fmt.Errorf("aborted before start")
	}

	startFn := d.StartFunc
	if startFn == nil {
		startFn = defaultStartFunc
	}
	if err := startFn(eng, engineCfg); err != nil {
		// Ensure error is captured even if StartFunc didn't go through engine.fail().
		d.mu.Lock()
		if d.lastStatus.Error == nil {
			d.lastStatus = engine.Status{
				State: engine.StateDisconnected,
				Error: engine.ClassifyError(err),
			}
		}
		s := d.lastStatus
		d.mu.Unlock()
		d.broadcastStatus(s)
		slog.Error("engine start failed", "err", err)
		return err
	}

	// Post-start abort: doDown arrived while engine.Start was running.
	// Engine started successfully but user wants down — tear it down immediately
	// within the same opMu critical section (route atomicity guaranteed).
	if d.abortUp.Load() {
		slog.Info("daemon: doUp aborted after start")
		eng.Stop()
		saveState(persistedState{State: engine.StateDisconnected, Timestamp: time.Now()})
		return fmt.Errorf("aborted by pending down")
	}

	ctx, cancel := context.WithCancel(context.Background())

	d.mu.Lock()
	d.engine = eng
	d.engineCtx = ctx
	d.engineCnl = cancel
	d.mu.Unlock()

	// Start PID monitoring if requested and supported on this platform.
	// Uses engineCtx so the goroutine stops when closeTunnel() cancels the context.
	if pid > 0 && shouldMonitorPID() {
		go d.monitorProcess(ctx, pid)
	}

	// Update persisted state.
	saveState(persistedState{
		Config:    cfg,
		State:     engine.StateConnected,
		Timestamp: time.Now(),
	})

	return nil
}

// defaultStartFunc is the production engine start path.
func defaultStartFunc(eng *engine.Engine, cfg engine.Config) error {
	return eng.Start(cfg)
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
	dd *directDialer,
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
		HealthCallback: d.broadcastSnapshot,
	}

	// Apply network monitor if available.
	if monErr == nil {
		ecfg.NetworkMonitor = monitor
		ecfg.InterfaceMonitor = ifaceMon
	}

	return ecfg
}

// doDown disconnects the tunnel.
// Sets abortUp before acquiring opMu so an in-progress doUp can detect
// the pending down and abort after engine.Start returns, avoiding a
// wasteful connect→immediate-disconnect cycle with transient route changes.
func (d *Daemon) doDown() {
	d.abortUp.Store(true)
	d.opMu.Lock()
	defer d.opMu.Unlock()
	d.abortUp.Store(false)
	slog.Info("daemon: doDown")
	d.closeTunnel()
}

// tryAutoReconnect checks persisted state and reconnects if applicable.
// ctx is the daemon's lifecycle context — reconnect is aborted if ctx is cancelled.
func (d *Daemon) tryAutoReconnect(ctx context.Context) {
	st, err := loadState()
	if err != nil || st.State != engine.StateConnected {
		return
	}

	// Only auto-reconnect if disconnected less than 7 days ago.
	// State=="connected" means previous daemon didn't shut down gracefully
	// (crash or kill), so reconnect is appropriate. 7-day window covers
	// overnight sleep, weekend, and vacation scenarios.
	if time.Since(st.Timestamp) > 7*24*time.Hour {
		slog.Info("auto-reconnect: stale state, skipping", "age", ">7d")
		return
	}

	if st.Config == nil {
		slog.Info("auto-reconnect: no config in saved state, skipping")
		return
	}

	slog.Info("auto-reconnect: reconnecting with saved config")
	safego.Go(func() {
		// Delay 5s to let daemon fully start; abort if daemon shuts down.
		select {
		case <-time.After(5 * time.Second):
		case <-ctx.Done():
			slog.Info("auto-reconnect: cancelled during startup delay")
			return
		}
		// Skip if already connected (e.g. user called up during delay).
		if d.State() == engine.StateConnected {
			return
		}
		if err := d.doUp(st.Config, 0); err != nil {
			slog.Error("auto-reconnect failed", "err", err)
		}
	})
}

// statusInfo returns current status as a map for the HTTP poll endpoint.
func (d *Daemon) statusInfo() map[string]any {
	d.mu.RLock()
	s := d.lastStatus
	cfg := d.lastConfig
	d.mu.RUnlock()

	info := map[string]any{
		"state": s.State,
	}
	if s.Error != nil {
		info["error"] = s.Error
	}
	if s.State == engine.StateConnected && !s.ConnectedAt.IsZero() {
		info["connected_at"] = s.ConnectedAt.Format(time.RFC3339)
		info["uptime_seconds"] = int(time.Since(s.ConnectedAt).Seconds())
	}
	if cfg != nil {
		info["config"] = cfg
	}
	return info
}

// OnStatus implements engine.EventHandler — updates lastStatus and broadcasts to SSE clients.
func (d *Daemon) OnStatus(status engine.Status) {
	d.mu.Lock()
	d.lastStatus = status
	d.mu.Unlock()
	d.broadcastStatus(status)
}

// OnStats implements engine.EventHandler — currently a no-op for daemon.
func (d *Daemon) OnStats(txBytes, rxBytes int64) {}

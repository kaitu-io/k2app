package daemon

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"time"

	"github.com/sasha-s/go-deadlock"

	tun "github.com/sagernet/sing-tun"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/engine"
	"github.com/kaitu-io/k2/provider"
	"github.com/kaitu-io/k2/rule"
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

	// Active subscription sessions. Each wraps a *config.Subscription with
	// session-local state for server rotation (the "excluded" URL set the
	// engine has asked us to skip via OutboundProvider.NextURL).
	subs []*subSession

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

	// Helper for non-VPN device management (ADB install, etc.).
	// Initialized by registerHelperRoutes() in Run().
	helper *AndroidHelper
}

// New creates a new daemon.
func New() *Daemon {
	return &Daemon{
		lastStatus: engine.Status{State: engine.StateDisconnected},
		sseClients: make(map[chan string]struct{}),
		helper:     &AndroidHelper{state: InstallState{Phase: "idle"}},
	}
}

// recoveryMiddleware catches panics in HTTP handler goroutines to prevent
// a single handler panic from crashing the entire daemon process.
func recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("HTTP handler panic",
					"panic", rec,
					"method", r.Method,
					"path", r.URL.Path,
					"stack", string(debug.Stack()),
				)
				http.Error(w, "internal error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
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
	d.registerHelperRoutes(mux)

	// Opt-in pprof for leak / goroutine diagnostics. Gated because it
	// exposes profile, cmdline, symbol, and goroutine dumps that can leak
	// heap addresses and internal state.
	if os.Getenv("K2_PPROF") == "1" {
		registerPProf(mux)
		slog.Warn("daemon: pprof handlers mounted at /debug/pprof/ (K2_PPROF=1)")
	}

	// Linux-only webapp + /api/storage + /api/platform + /api/upgrade.
	// No-op on macOS/Windows (Tauri provides the UI on those platforms).
	d.registerWebUI(mux)

	srv := &http.Server{
		Addr:    listenAddr,
		Handler: recoveryMiddleware(corsMiddleware(mux)),
	}

	// Pre-warm rule bundle cache in the background so engine.Start()
	// can load rules from disk instantly.
	d.startRulePrefetch(ctx)

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
		d.helper.stopAdbServer()
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

	// Stop subscription refresh goroutines.
	d.mu.Lock()
	subs := d.subs
	d.subs = nil
	d.mu.Unlock()
	for _, s := range subs {
		s.Close()
	}

	if cancel != nil {
		cancel()
	}
	if eng != nil {
		// Engine.Stop() calls tm.Close() → wire Close which may block on
		// quic-go/smux cleanup. Wire layer has 5s timeout, but add an outer
		// 10s bound as defense-in-depth against other blocking paths
		// (CGo dnsOverride cleanup, sing-tun provider.Close).
		done := make(chan struct{})
		go func() {
			eng.Stop()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			slog.Warn("daemon: engine.Stop timed out after 10s, orphaning")
		}
	}

	// Persist disconnected state, but keep the config so rule prefetch can
	// use it on next daemon start to pre-warm the bundle cache.
	d.mu.RLock()
	cfg := d.lastConfig
	d.mu.RUnlock()
	saveState(persistedState{Config: cfg, State: engine.StateDisconnected, Timestamp: time.Now()})
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
	// Note: cfg still has original k2subs:// URLs — never persist resolved k2v5://.
	saveState(persistedState{
		Config:    cfg,
		State:     engine.StateConnecting,
		Timestamp: time.Now(),
	})

	// Resolve k2subs:// subscriptions to k2v5:// URLs.
	subs, resolved, subsErr := d.resolveSubscriptions(cfg)
	if subsErr != nil {
		slog.Error("daemon: doUp aborted — subscription resolve failed", "err", subsErr)
		d.mu.Lock()
		d.lastStatus = engine.Status{
			State: engine.StateDisconnected,
			Error: engine.ClassifyError(subsErr),
		}
		s := d.lastStatus
		d.mu.Unlock()
		d.broadcastStatus(s)
		return subsErr
	}
	// Ensure subs are cleaned up if doUp fails after this point.
	subsCleanup := true
	defer func() {
		if subsCleanup {
			for _, s := range subs {
				s.Close()
			}
		}
	}()

	// Build resolved config for engine (k2subs:// → k2v5://).
	// Original cfg (with k2subs://) is kept for saveState/lastConfig.
	engineClientCfg := applyResolvedRoutes(cfg, resolved)

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

	engineCfg := d.engineConfigFromClientConfig(engineClientCfg, dd, monitor, ifaceMon, monErr)

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
		saveState(persistedState{Config: cfg, State: engine.StateDisconnected, Timestamp: time.Now()})
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

	// Update persisted state (original cfg with k2subs:// URLs).
	saveState(persistedState{
		Config:    cfg,
		State:     engine.StateConnected,
		Timestamp: time.Now(),
	})

	// Disarm cleanup — subs are now owned by d.subs (closed in closeTunnel).
	subsCleanup = false

	// Start background subscription refresh — reuse the per-session HTTP
	// client built in resolveSubscriptions so refresh fetches go through
	// the same Transport (connection pool) that subSession.Close will
	// later release via CloseIdleConnections.
	for _, s := range subs {
		client := s.httpClient
		s.sub.StartRefresh(ctx, func() *http.Client { return client })
	}

	d.mu.Lock()
	d.subs = subs
	d.mu.Unlock()

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
	// Ensure CacheDir has a value for rule bundles.
	if cfg.CacheDir == "" {
		cfg.CacheDir = filepath.Join(cacheDir(), "rules")
	}

	ecfg := engine.Config{
		Client:           cfg,
		FileDescriptor:   -1, // Desktop: engine creates its own TUN.
		DirectDialer:     dd,
		HealthCallback:   d.broadcastSnapshot,
		OutboundProvider: d, // Daemon implements engine.OutboundProvider (k2subs rotation).
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

// startRulePrefetch kicks off a background rule bundle download at daemon startup.
// Uses the persisted config to determine which bundles are needed. Skips the
// download entirely if cached bundles are less than 24h old (no HTTP at all).
// Does not block — runs in a safego goroutine.
func (d *Daemon) startRulePrefetch(ctx context.Context) {
	st, err := loadState()
	if err != nil || st.Config == nil {
		return
	}

	cfg := st.Config
	config.SetDefaults(cfg)

	dir := cfg.CacheDir
	if dir == "" {
		dir = filepath.Join(cacheDir(), "rules")
	}

	matches := make([]rule.MatchConfig, len(cfg.Routes))
	for i, r := range cfg.Routes {
		matches[i] = r.Match
	}
	bundleNames := rule.BundlesForConfig(matches)
	if len(bundleNames) == 0 {
		return
	}

	sources := cfg.RuleSources
	if len(sources) == 0 {
		sources = rule.DefaultSources
	}

	safego.Go(func() {
		dlCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if err := rule.DownloadAndReload(dlCtx, nil, dir, bundleNames, sources, nil, 24*time.Hour); err != nil {
			slog.Warn("rule prefetch failed", "err", err)
		} else {
			slog.Info("rule prefetch complete", "bundles", len(bundleNames))
		}
	})
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

	// Derive Server from routes (Server is json:"-", not persisted).
	config.SetDefaults(st.Config)

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
		info["startAt"] = s.ConnectedAt.Unix()
		info["uptimeSeconds"] = int(time.Since(s.ConnectedAt).Seconds())
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

// OnOutboundFatal implements engine.EventHandler. The engine has already
// fired e.fail() with the underlying error before this callback fires, so
// d.lastStatus is already updated via OnStatus. This hook is informational:
// we log which outbound finally exhausted replacements so operators can
// distinguish "all subscribed tunnels failed" from other transport errors.
func (d *Daemon) OnOutboundFatal(outbound string, err error) {
	slog.Warn("daemon: outbound exhausted replacements", "outbound", outbound, "err", err)
}

// resolveSubscriptions scans routes for k2subs:// URLs, fetches each subscription,
// picks a server, and returns the Subscription objects + a map of route index → resolved k2v5:// URL.
// On error, all already-created Subscription objects are closed before returning.
func (d *Daemon) resolveSubscriptions(cfg *config.ClientConfig) (
	subs []*subSession,
	resolved map[int]string,
	err error,
) {
	resolved = make(map[int]string)
	subsCache := filepath.Join(cacheDir(), "subs")

	closeAll := func(ss []*subSession) {
		for _, s := range ss {
			s.Close()
		}
	}

	for i, route := range cfg.Routes {
		if !strings.HasPrefix(route.Via, "k2subs://") {
			continue
		}

		sub, parseErr := config.NewSubscription(route.Via, subsCache)
		if parseErr != nil {
			slog.Error("subscription parse failed",
				"via", config.TruncateVia(route.Via),
				"err", parseErr)
			closeAll(subs)
			return nil, nil, fmt.Errorf("subscription parse: %w", parseErr)
		}

		// Per-session HTTP client. Same Transport is reused for the
		// initial Fetch and for StartRefresh's fetches; subSession.Close
		// calls Transport.CloseIdleConnections to release pooled
		// persistConns (otherwise the net/http readLoop goroutines leak
		// one per up/down cycle).
		sess := &subSession{
			sub: sub,
			httpTransport: &http.Transport{
				TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
			},
		}
		sess.httpClient = &http.Client{
			Timeout:   15 * time.Second,
			Transport: sess.httpTransport,
		}

		fetchCtx, fetchCancel := context.WithTimeout(context.Background(), 15*time.Second)
		fetchErr := sub.Fetch(fetchCtx, sess.httpClient)
		fetchCancel()

		if fetchErr != nil {
			if len(sub.Tunnels()) == 0 {
				slog.Error("subscription fetch failed with no cached data",
					"via", config.TruncateVia(route.Via),
					"err", fetchErr)
				sess.Close()
				closeAll(subs)
				return nil, nil, fmt.Errorf("subscription fetch failed (no cache): %w", fetchErr)
			}
			slog.Warn("subscription fetch failed, using cached data", "err", fetchErr)
		}

		tunnel, pickErr := sub.Pick(nil)
		if pickErr != nil {
			slog.Error("subscription pick failed",
				"via", config.TruncateVia(route.Via),
				"servers", len(sub.Tunnels()),
				"err", pickErr)
			sess.Close()
			closeAll(subs)
			return nil, nil, fmt.Errorf("subscription pick: %w", pickErr)
		}

		slog.Info("subscription resolved",
			"to", config.TruncateVia(tunnel.URL),
			"servers", len(sub.Tunnels()),
		)

		resolved[i] = tunnel.URL
		subs = append(subs, sess)
	}

	return subs, resolved, nil
}

// applyResolvedRoutes returns a shallow copy of cfg with k2subs:// routes replaced
// by their resolved k2v5:// URLs. The original cfg is not modified.
func applyResolvedRoutes(cfg *config.ClientConfig, resolved map[int]string) *config.ClientConfig {
	if len(resolved) == 0 {
		return cfg
	}
	engineCfg := *cfg
	engineCfg.Routes = make([]config.RouteConfig, len(cfg.Routes))
	copy(engineCfg.Routes, cfg.Routes)
	for i, url := range resolved {
		engineCfg.Routes[i].Via = url
	}
	engineCfg.Server = ""
	config.SetDefaults(&engineCfg)
	return &engineCfg
}

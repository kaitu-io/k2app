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
	"sync"
	"sync/atomic"
	"time"

	"github.com/sasha-s/go-deadlock"

	tun "github.com/sagernet/sing-tun"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/engine"
	"github.com/kaitu-io/k2/probe"
	"github.com/kaitu-io/k2/provider"
	"github.com/kaitu-io/k2/rule"
	"github.com/kaitu-io/k2/safego"
	"github.com/kaitu-io/k2/wire"
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

	// Active subscription sessions (k2subs:// routes, background refresh).
	// Each wraps a *config.Subscription with session-local state for server
	// rotation (the "excluded" URL set the engine has asked us to skip via
	// OutboundProvider.NextURL).
	subs []*subSession

	// dd is the current session's physical-interface-bound dialer.
	// Set in doUp alongside d.subs, cleared in closeTunnel. Probe reads it
	// via daemonURLSource.DirectDialer() so background probes bypass the
	// active TUN (otherwise probe UDP sockets loop through our own tunnel).
	dd *directDialer

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

	// probeRegistry caches node measurements. Shared between:
	//   - Subscription.Pick (via SetScoreSource — see resolveSubscriptions)
	//   - Background probe.Service loop (populates registry)
	//   - /api/core probe action (returns snapshot + populates registry)
	probeRegistry *probe.Registry

	// probeFn is the function used to execute a single probe. Defaults to
	// wire.ProbeURL. Overridable for tests.
	probeFn func(ctx context.Context, rawURL string, dd wire.DirectDialer, rounds int, interval time.Duration) (wire.ProbeStats, error)

	// probeService runs the periodic background probe loop. May be nil in
	// tests that don't need it.
	probeService *probe.Service

	// subsHTTPClient is the HTTP client used to fetch subscription endpoints
	// from resolveSubscriptions. When nil, a default TLS-1.2-minimum client
	// is constructed per call. Overridable for tests.
	subsHTTPClient *http.Client

	// subsCacheDirOverride pins the disk cache directory used by
	// resolveSubscriptions. Empty string = use platform default. Tests
	// point this at t.TempDir() so cached tunnels can be preloaded.
	subsCacheDirOverride string

	// appBypassOverrides is the in-memory cache of daemon's persisted app-bypass
	// state. Loaded at Run() startup from state.json via tryAutoReconnect.
	// Mutated via /api/core app-bypass-* actions.
	appBypassOverrides AppBypassOverrides
}

// New creates a new daemon.
func New() *Daemon {
	return &Daemon{
		lastStatus:    engine.Status{State: engine.StateDisconnected},
		sseClients:    make(map[chan string]struct{}),
		helper:        &AndroidHelper{state: InstallState{Phase: "idle"}},
		probeRegistry: probe.NewRegistry(15 * time.Minute),
		probeFn:       wire.ProbeURL,
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

	// Start periodic probe loop. Source is empty when disconnected; once
	// doUp populates d.subs, the service begins probing active tunnels.
	d.probeService = probe.NewService(probe.Config{
		Registry: d.probeRegistry,
		Source:   daemonURLSource{d: d},
		// DirectDialer: nil — background probe uses default routing, same
		// as ad-hoc probe action. A more advanced variant could reuse the
		// active tunnel's directDialer, but that's out of scope for v1.
	})
	d.probeService.Start(ctx)

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
		if d.probeService != nil {
			d.probeService.Close()
		}
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

	// Stop subscription refresh goroutines and release the direct dialer
	// snapshot (probe must fall back to OS routing when we're not connected).
	d.mu.Lock()
	subs := d.subs
	d.subs = nil
	d.dd = nil
	d.mu.Unlock()
	for _, sess := range subs {
		sess.Close()
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
	d.saveLifecycleState(cfg, engine.StateDisconnected)
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

	// Pull ClientConfig.AppBypass into in-memory overrides BEFORE the first
	// saveState so the persisted snapshot is fresh and engineConfigFromClientConfig
	// (called below) picks up the updated region + custom adds.
	d.ingestClientAppBypass(cfg)

	// Persist state for auto-reconnect across restarts.
	// Note: cfg still has original k2subs:// URLs — never persist resolved k2v5://.
	d.saveLifecycleState(cfg, engine.StateConnecting)

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
			for _, sess := range subs {
				sess.Close()
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
		d.saveLifecycleState(cfg, engine.StateDisconnected)
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
	d.saveLifecycleState(cfg, engine.StateConnected)

	// Disarm cleanup — subs are now owned by d.subs (closed in closeTunnel).
	subsCleanup = false

	// Start background subscription refresh — reuse the per-session HTTP
	// client built in resolveSubscriptions so refresh fetches go through
	// the same Transport (connection pool) that subSession.Close will
	// later release via CloseIdleConnections.
	for _, sess := range subs {
		client := sess.httpClient
		sess.sub.StartRefresh(ctx, func() *http.Client { return client })
	}

	d.mu.Lock()
	d.subs = subs
	d.dd = dd
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

	d.mu.RLock()
	overrides := d.appBypassOverrides
	d.mu.RUnlock()

	ecfg := engine.Config{
		Client:           cfg,
		FileDescriptor:   -1, // Desktop: engine creates its own TUN.
		DirectDialer:     dd,
		HealthCallback:   d.broadcastSnapshot,
		OutboundProvider: d, // Daemon implements engine.OutboundProvider (k2subs rotation).
		AppBypassRegion:  overrides.Region,
		AppBypassCustom: engine.AppBypassCustom{
			ProcessAdds: overrides.ProcessAdds,
			PackageAdds: overrides.PackageAdds,
		},
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
// Always fetches the full country-set tarball — no persisted config required.
// Skips entirely if bundles.version is <24h old (handled inside EnsureBundles).
func (d *Daemon) startRulePrefetch(ctx context.Context) {
	dir := filepath.Join(cacheDir(), "rules")

	safego.Go(func() {
		dlCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if err := rule.EnsureBundles(dlCtx, dir, rule.DefaultSources); err != nil {
			slog.Warn("rule prefetch failed", "err", err)
			return
		}
		slog.Info("rule prefetch complete", "dir", dir)
	})
}

// tryAutoReconnect checks persisted state and reconnects if applicable.
// ctx is the daemon's lifecycle context — reconnect is aborted if ctx is cancelled.
func (d *Daemon) tryAutoReconnect(ctx context.Context) {
	st, err := loadState()

	// Seed app-bypass overrides from persisted state. Independent of
	// reconnect decision — even if Config is nil / State != connected,
	// the user's app-bypass preferences should survive daemon restarts.
	if err == nil && st.AppBypass != nil {
		d.mu.Lock()
		d.appBypassOverrides = *st.AppBypass
		d.mu.Unlock()
		slog.Info("daemon: loaded app-bypass overrides",
			"region", st.AppBypass.Region,
			"process_count", len(st.AppBypass.ProcessAdds),
			"package_count", len(st.AppBypass.PackageAdds),
		)
	}

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

// currentAppBypass returns a snapshot of the current app-bypass overrides.
func (d *Daemon) currentAppBypass() AppBypassOverrides {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.appBypassOverrides
}

// persistAppBypass writes the daemon's full persistedState (including the
// current Config + AppBypass) atomically via saveState. Returns silently —
// saveState already logs on failure (matches existing daemon convention).
func (d *Daemon) persistAppBypass() {
	d.mu.RLock()
	o := d.appBypassOverrides
	cfg := d.lastConfig
	state := d.lastStatus.State
	d.mu.RUnlock()

	saveState(persistedState{
		Config:    cfg,
		State:     state,
		Timestamp: time.Now().UTC(),
		AppBypass: &o,
	})
}

// saveLifecycleState writes persistedState while preserving the current
// AppBypass overrides. Used by lifecycle transitions (doUp / closeTunnel)
// so smart-bypass preferences round-trip across daemon restarts without
// having to call persistAppBypass separately.
func (d *Daemon) saveLifecycleState(cfg *config.ClientConfig, state string) {
	d.mu.RLock()
	o := d.appBypassOverrides
	d.mu.RUnlock()
	saveState(persistedState{
		Config:    cfg,
		State:     state,
		Timestamp: time.Now().UTC(),
		AppBypass: &o,
	})
}

// ingestClientAppBypass mirrors ClientConfig.AppBypass into the daemon's
// in-memory overrides so the engine.Config built later picks them up.
// Empty Region falls back to deriving from the routes' "<cc>-access"
// preset, letting a global-mode connect still pick up a region preset
// when the user has one set.
func (d *Daemon) ingestClientAppBypass(cfg *config.ClientConfig) {
	if cfg == nil {
		return
	}
	region := ""
	var processAdds, packageAdds []string
	if cfg.AppBypass != nil {
		region = strings.ToLower(strings.TrimSpace(cfg.AppBypass.Region))
		if len(cfg.AppBypass.ProcessAdds) > 0 {
			processAdds = append([]string(nil), cfg.AppBypass.ProcessAdds...)
		}
		if len(cfg.AppBypass.PackageAdds) > 0 {
			packageAdds = append([]string(nil), cfg.AppBypass.PackageAdds...)
		}
	}
	if region == "" {
		region = deriveRegionFromRoutes(cfg.Routes)
	}
	d.mu.Lock()
	d.appBypassOverrides = AppBypassOverrides{
		Region:      region,
		ProcessAdds: processAdds,
		PackageAdds: packageAdds,
	}
	d.mu.Unlock()
}

// deriveRegionFromRoutes returns the country code embedded in the first
// "<cc>-access" preset found in routes (e.g. "cn-access" → "cn"). Returns
// "" when no such preset exists. Lets the daemon pick up region-aware
// smart bypass even when ClientConfig.AppBypass.Region is empty.
func deriveRegionFromRoutes(routes []config.RouteConfig) string {
	for _, r := range routes {
		preset := r.Match.Preset
		if preset == "" {
			continue
		}
		if cc, ok := strings.CutSuffix(preset, "-access"); ok && len(cc) == 2 {
			return strings.ToLower(cc)
		}
	}
	return ""
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
// picks a server, and returns the subscription sessions + a map of route index → resolved k2v5:// URL.
// On error, all already-created sessions are closed before returning.
func (d *Daemon) resolveSubscriptions(cfg *config.ClientConfig) (
	subs []*subSession,
	resolved map[int]string,
	err error,
) {
	resolved = make(map[int]string)
	subsCache := d.subsCacheDirOverride
	if subsCache == "" {
		subsCache = filepath.Join(cacheDir(), "subs")
	}

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

		// Wire probe-derived ScoreSource so the first Pick and every NextURL
		// replacement consult probe.Registry. Nil-safe — preserves legacy
		// behavior when registry is unpopulated.
		if d.probeRegistry != nil {
			sub.SetScoreSource(d.probeRegistry)
		}

		// Per-session HTTP client. Same Transport is reused for the
		// initial Fetch and for StartRefresh's fetches; subSession.Close
		// calls Transport.CloseIdleConnections to release pooled
		// persistConns (otherwise the net/http readLoop goroutines leak
		// one per up/down cycle). Tests can override via d.subsHTTPClient
		// to supply a client with a custom TLS trust root.
		sess := &subSession{sub: sub}
		if d.subsHTTPClient != nil {
			sess.httpClient = d.subsHTTPClient
		} else {
			sess.httpTransport = &http.Transport{
				TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
			}
			sess.httpClient = &http.Client{
				Timeout:   15 * time.Second,
				Transport: sess.httpTransport,
			}
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

// handleProbe runs concurrent wire.ProbeURL calls for the given URLs and
// returns aggregated stats. Results are also written to d.probeRegistry so
// ad-hoc probes benefit Subscription.Pick too.
//
// Concurrency cap: 4. Total budget: params.timeoutMs (default 8000).
func (d *Daemon) handleProbe(w http.ResponseWriter, params map[string]any) {
	rawURLs, ok := params["urls"].([]any)
	if !ok || len(rawURLs) == 0 {
		writeJSON(w, Response{Code: 400, Message: "urls: required, array of strings"})
		return
	}
	urls := make([]string, 0, len(rawURLs))
	for _, u := range rawURLs {
		s, ok := u.(string)
		if !ok || s == "" {
			writeJSON(w, Response{Code: 400, Message: "urls: must contain non-empty strings"})
			return
		}
		urls = append(urls, s)
	}

	timeoutMs := 8000
	if v, ok := params["timeoutMs"].(float64); ok && v > 0 {
		timeoutMs = int(v)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()

	// Reuse the active session's direct dialer so probe UDP bypasses the
	// TUN when the VPN is up; nil is correct when disconnected.
	d.mu.RLock()
	dd := d.dd
	d.mu.RUnlock()
	var probeDD wire.DirectDialer
	if dd != nil {
		probeDD = dd
	}

	results := make([]map[string]any, len(urls))
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for i, u := range urls {
		sem <- struct{}{}
		wg.Add(1)
		idx, url := i, u
		safego.Go(func() {
			defer wg.Done()
			defer func() { <-sem }()
			stats, err := d.probeFn(ctx, url, probeDD, 8, 50*time.Millisecond)
			if err != nil {
				stats.MeasuredAt = time.Now()
			}
			d.probeRegistry.Record(url, stats)
			results[idx] = map[string]any{
				"url":           url,
				"avgRttMs":      stats.AvgRttMs,
				"minRttMs":      stats.MinRttMs,
				"maxRttMs":      stats.MaxRttMs,
				"jitterMs":      stats.JitterMs,
				"lossRate":      stats.LossRate,
				"reachable":     stats.Reachable,
				"echoSupported": stats.EchoSupported,
				"probeScore":    stats.Score(),
				"measuredAt":    stats.MeasuredAt.UTC().Format(time.RFC3339Nano),
			}
		})
	}
	wg.Wait()

	writeJSON(w, Response{Code: 0, Data: map[string]any{"results": results}})
}

// daemonURLSource returns the set of URLs currently subscribed by active
// subSessions. Returns empty when disconnected — probe.Service's cycle()
// treats that as a no-op.
//
// Concurrency: snapshots d.subs under d.mu.RLock (state lock), then releases
// the lock before calling sub.Tunnels() on each session. Subscription.mu is
// a standalone lock in the Lock Ordering Graph — it must never be acquired
// while holding daemon.mu.
type daemonURLSource struct{ d *Daemon }

func (s daemonURLSource) CandidateURLs() []string {
	s.d.mu.RLock()
	sessions := make([]*subSession, len(s.d.subs))
	copy(sessions, s.d.subs)
	s.d.mu.RUnlock()

	seen := make(map[string]struct{})
	var urls []string
	for _, sess := range sessions {
		for _, t := range sess.sub.Tunnels() {
			if _, ok := seen[t.URL]; ok {
				continue
			}
			seen[t.URL] = struct{}{}
			urls = append(urls, t.URL)
		}
	}
	return urls
}

// DirectDialer returns the active session's physical-interface-bound dialer,
// or nil when no session is active (OS routing is correct while disconnected).
// Explicit nil-interface return when the concrete pointer is nil — otherwise
// a typed-nil slips through any `if dd != nil` check in wire.ProbeURL.
func (s daemonURLSource) DirectDialer() wire.DirectDialer {
	s.d.mu.RLock()
	dd := s.d.dd
	s.d.mu.RUnlock()
	if dd == nil {
		return nil
	}
	return dd
}

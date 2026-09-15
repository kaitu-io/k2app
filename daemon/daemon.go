package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
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
	"github.com/kaitu-io/k2/subscription"
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

	// inDoUpTeardown counts doUp invocations currently in their teardown
	// window. While > 0, OnStatus(disconnected) with Error==nil is dropped —
	// it's the engine.Stop teardown noise from swapping engines, not a real
	// disconnect. Real failures (Error != nil) and all non-disconnected states
	// pass through unconditionally.
	//
	// A counter (not a bool) is required because concurrent doUps would
	// otherwise stomp each other's flag on defer — doUp #1's deferred clear
	// would zero the flag while doUp #2 still needs it set across its own
	// closeTunnel. With Add(+1)/Add(-1) pairing, each doUp manages its own
	// contribution and the filter stays active iff any doUp is mid-teardown.
	inDoUpTeardown atomic.Int32

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

	// Subscription manager. Created once at construction; Resolve populates
	// sessions on each doUp; CloseAll drains them in closeTunnel. NextURL is
	// wired into engine.Config.OutboundProvider.
	subsMgr *subscription.Manager

	// dd is the current session's physical-interface-bound dialer.
	// Set in doUp, cleared in closeTunnel. Probe reads it via
	// daemonURLSource.DirectDialer() so background probes bypass the
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
	//   - Subscription.Pick (via subsMgr's ScoreSource)
	//   - Background probe.Service loop (populates registry)
	//   - /api/core probe action (returns snapshot + populates registry)
	probeRegistry *probe.Registry

	// probeFn is the function used to execute a single probe. Defaults to
	// wire.ProbeURL. Overridable for tests.
	probeFn func(ctx context.Context, rawURL string, dd wire.DirectDialer, rounds int, interval time.Duration) (wire.ProbeStats, error)

	// probeService runs the periodic background probe loop. May be nil in
	// tests that don't need it.
	probeService *probe.Service

	// ruleCacheDir is the bundles directory derived from cacheDir() once at
	// daemon construction. Cached so the refresh loop doesn't recompute the
	// platform path on every tick.
	ruleCacheDir string

	// refreshAction is the body executed by the rule refresh ticker.
	// Real Daemon instances use doRefreshRules; tests inject a counter.
	refreshAction func(ctx context.Context, dir string)

	// subsCacheDirOverride pins the disk cache directory used by
	// resolveSubscriptions. Empty string = use platform default. Tests
	// point this at t.TempDir() so cached tunnels can be preloaded.
	subsCacheDirOverride string
}

// New creates a new daemon.
func New() *Daemon {
	d := &Daemon{
		lastStatus:    engine.Status{State: engine.StateDisconnected},
		sseClients:    make(map[chan string]struct{}),
		helper:        &AndroidHelper{state: InstallState{Phase: "idle"}},
		probeRegistry: probe.NewRegistry(15 * time.Minute),
		probeFn:       wire.ProbeURL,
		ruleCacheDir:  filepath.Join(cacheDir(), "rules"),
	}
	d.refreshAction = d.doRefreshRules
	d.subsMgr = subscription.NewManager(
		filepath.Join(cacheDir(), "subs"),
		nil,
		d.probeRegistry,
	)
	return d
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
	d.registerWebUI(ctx, mux)

	srv := &http.Server{
		Addr:    listenAddr,
		Handler: recoveryMiddleware(corsMiddleware(mux)),
	}

	// Pre-warm rule bundle cache in the background so engine.Start()
	// can load rules from disk instantly.
	d.startRuleRefreshLoop(ctx)

	// Load persisted state and auto-reconnect if applicable.
	d.tryAutoReconnect(ctx)

	// Start periodic probe loop. Source is empty when disconnected; once
	// doUp populates subsMgr, the service begins probing active tunnels.
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
	d.dd = nil
	d.mu.Unlock()
	d.subsMgr.CloseAll()

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
	// Pre-fire "connecting" + arm the teardown filter BEFORE opMu.Lock so the
	// opMu wait window — which can be 5–9s on Windows while a previous
	// doDown's engine.Stop blocks on wintun teardown — also looks like
	// monotonic "connecting" to external consumers. Without this, consumers
	// polling during the wait would see the previous doDown's lingering
	// "disconnected" status and the webapp vpn-machine would flicker through
	// idle/reconnecting.
	//
	// The filter (see OnStatus) drops clean OnStatus(disconnected) while
	// inDoUpTeardown is true. Real failures (Error != nil) always propagate.
	// We clear the flag IMMEDIATELY after closeTunnel returns so the rest of
	// doUp (including the post-start abort path that calls eng.Stop) is
	// unaffected — those engine.Stop events are legitimate user-visible
	// signals, not teardown noise. `defer Store(false)` is the safety net
	// for any error path that returns before reaching closeTunnel.
	pre := engine.Status{State: engine.StateConnecting}
	d.mu.Lock()
	d.lastStatus = pre
	d.mu.Unlock()
	d.broadcastStatus(pre)

	// Counter semantics, not a bool: under concurrent doUp, a deferred
	// Store(false) from one call would otherwise zero the flag while a
	// peer's closeTunnel still needs it set. Add/armed guards against that.
	d.inDoUpTeardown.Add(1)
	armed := true
	defer func() {
		if armed {
			d.inDoUpTeardown.Add(-1)
		}
	}()

	d.opMu.Lock()
	defer d.opMu.Unlock()
	d.abortUp.Store(false) // reset on new up
	slog.Info("daemon: doUp", "mode", cfg.Mode, "pid", pid)

	// Unconditionally tear down any existing tunnel (idempotent).
	// OnStatus(disconnected) from this engine.Stop is the teardown noise we
	// suppress. After closeTunnel returns, decrement the counter so the rest
	// of doUp behaves normally — including the post-start abort eng.Stop below.
	d.closeTunnel()
	d.inDoUpTeardown.Add(-1)
	armed = false

	d.mu.Lock()
	d.lastConfig = cfg
	d.monitorPID = pid
	d.mu.Unlock()

	// Persist state for auto-reconnect across restarts.
	// Note: cfg still has original k2subs:// URLs — never persist resolved k2v5://.
	d.saveLifecycleState(cfg, engine.StateConnecting)

	// Resolve k2subs:// subscriptions to k2v5:// URLs.
	subs, resolved, subsErr := d.subsMgr.Resolve(cfg)
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
	// Delegates to subsMgr.CloseAll so the manager's internal sessions slice
	// is also cleared (Session.Close is idempotent — double-close on
	// closeTunnel is safe).
	subsCleanup := true
	defer func() {
		if subsCleanup {
			d.subsMgr.CloseAll()
		}
	}()

	// Build resolved config for engine (k2subs:// → k2v5://).
	// Original cfg (with k2subs://) is kept for saveState/lastConfig.
	engineClientCfg := subscription.ApplyResolved(cfg, resolved)

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

	// Disarm cleanup — subs are now owned by subsMgr (closed in closeTunnel).
	subsCleanup = false

	// Start background subscription refresh — reuse the per-session HTTP
	// client built in subsMgr.Resolve so refresh fetches go through the
	// same Transport (connection pool) that Session.Close will later
	// release via CloseIdleConnections.
	for _, sess := range subs {
		client := sess.HTTPClient()
		sub := sess.Subscription()
		// Phase 0（spec §4.3 P2）：凭据轮换后回写 persistedState.Config，
		// 使 7 天自动重连重放的是新 token 而非冻结在上次连接时刻的旧值。
		sub.SetOnCredsRotate(d.persistRotatedCreds)
		sub.StartRefresh(ctx, func() *http.Client { return client })
	}

	d.mu.Lock()
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

	ecfg := engine.Config{
		Client:           cfg,
		FileDescriptor:   -1, // Desktop: engine creates its own TUN.
		DirectDialer:     dd,
		HealthCallback:   d.broadcastSnapshot,
		OutboundProvider: d.subsMgr, // subscription.Manager implements engine.OutboundProvider (k2subs rotation).
		ProcessSearcher:  newProcessSearcher(),
		// Desktop has no battery-radio cost concern — echoProbeLoop actively
		// probes the inactive backup transport (Task 7 dual-transport
		// scoreboard). MobilePowerSaving stays false (default); set
		// explicitly here for symmetry with appext's true.
		//
		// K2_MOBILE_POWER_SAVING=1 is a test-only override so the Linux
		// daemon under test/netem can exercise the mobile probe gate
		// (appext's path) on an emulated lossy link. Never set in production.
		MobilePowerSaving: os.Getenv("K2_MOBILE_POWER_SAVING") == "1",
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

// startRuleRefreshLoop spawns a background goroutine that performs an
// immediate rule bundle refresh and then re-checks every 1h for the
// lifetime of the daemon process. Unlike the previous one-shot prefetch,
// this loop runs independently of tunnel connect state, so a daemon that
// stays up for days without a connect still picks up new bundles.
//
// Cost per tick is dominated by one os.Stat (rule.EnsureBundles short-
// circuits via the 24h bundlesFreshTTL gate when bundles.version is
// present and recent); a real CDN fetch happens at most once per day.
// When bundles.version is absent (cold start or failed prior fetch),
// every tick attempts a real download until one succeeds.
func (d *Daemon) startRuleRefreshLoop(ctx context.Context) {
	safego.Go(func() {
		d.runRuleRefreshLoop(ctx, 1*time.Hour)
	})
}

// runRuleRefreshLoop is the loop body extracted for testing — accepts an
// explicit interval so unit tests can run with millisecond-scale ticks
// without sleeping for hours.
func (d *Daemon) runRuleRefreshLoop(ctx context.Context, interval time.Duration) {
	// Fire once immediately so cold-start behavior matches the previous
	// one-shot prefetch.
	d.refreshAction(ctx, d.ruleCacheDir)

	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			d.refreshAction(ctx, d.ruleCacheDir)
		}
	}
}

// doRefreshRules is the default refreshAction — performs a bounded
// EnsureBundles call with a 30s overall timeout.
func (d *Daemon) doRefreshRules(ctx context.Context, dir string) {
	dlCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := rule.EnsureBundles(dlCtx, dir, rule.DefaultSources); err != nil {
		// Warn so cold-start failures (no cached bundle, GFW block, etc.) surface
		// in support logs. Steady-state hourly ticks share this level — rule
		// fetches rarely fail in production, so noise is bounded.
		slog.Warn("rule refresh failed", "err", err, "dir", dir)
		return
	}
	slog.Debug("rule refresh complete", "dir", dir)
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

// saveLifecycleState writes persistedState (Config + State) so the daemon can
// auto-reconnect across restarts. Called by doUp / closeTunnel lifecycle
// transitions.
func (d *Daemon) saveLifecycleState(cfg *config.ClientConfig, state string) {
	saveState(persistedState{
		Config:    cfg,
		State:     state,
		Timestamp: time.Now().UTC(),
	})
}

// OnStatus implements engine.EventHandler — updates lastStatus and broadcasts to SSE clients.
//
// Teardown noise filter: during a doUp closeTunnel window the previous engine
// fires OnStatus(disconnected) as part of being swapped out. That intermediate
// disconnected state is an implementation artifact, not a real disconnect, and
// must not leak to external consumers (webapp state machine, SSE clients).
// The filter is narrow: only clean disconnected (Error == nil) while any doUp
// is mid-teardown (counter > 0) is dropped — real failures (Error != nil)
// always propagate.
func (d *Daemon) OnStatus(status engine.Status) {
	if d.inDoUpTeardown.Load() > 0 && status.State == engine.StateDisconnected && status.Error == nil {
		return
	}
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
// Sessions. Returns empty when disconnected — probe.Service's cycle()
// treats that as a no-op. Delegates to subsMgr.CandidateURLs which has
// its own internal locking; daemon.mu is not held across the call.
type daemonURLSource struct{ d *Daemon }

func (s daemonURLSource) CandidateURLs() []string {
	return s.d.subsMgr.CandidateURLs()
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

// rewriteRouteVia replaces every route whose Via equals oldVia with newVia.
// Returns true when at least one route was rewritten. Pure function.
func rewriteRouteVia(cfg *config.ClientConfig, oldVia, newVia string) bool {
	if cfg == nil || oldVia == "" || newVia == "" || oldVia == newVia {
		return false
	}
	changed := false
	for i := range cfg.Routes {
		if cfg.Routes[i].Via == oldVia {
			cfg.Routes[i].Via = newVia
			changed = true
		}
	}
	return changed
}

// persistRotatedCreds is the Subscription creds-rotation callback: rewrite
// the matching k2subs:// route in lastConfig and re-persist auto-reconnect
// state. Concurrency constitution: field rewrite under d.mu; the disk write
// (saveLifecycleState) happens OUTSIDE the lock.
//
// Every subscription registered in doUp's loop shares this same callback and
// the same *config.ClientConfig (d.lastConfig) — so two subscriptions can
// rotate concurrently. d.mu only serializes access to the *pointer*; once a
// pointer to the shared struct escapes the lock, nothing protects the fields
// it points to. saveLifecycleState's json.Marshal walks every route, not
// just the one this call rewrote, so it must never read the live, still-
// mutable d.lastConfig after unlocking. Instead we snapshot (deep-copy) the
// Routes slice into a fresh backing array while still holding the lock, and
// marshal/persist that private copy outside the lock — no other goroutine
// can ever observe or mutate memory reachable only from this copy.
func (d *Daemon) persistRotatedCreds(oldRaw, newRaw string) {
	d.mu.Lock()
	cfg := d.lastConfig
	state := d.lastStatus.State
	updated := rewriteRouteVia(cfg, oldRaw, newRaw)
	var snapshot *config.ClientConfig
	if updated {
		cp := *cfg
		cp.Routes = append([]config.RouteConfig(nil), cfg.Routes...)
		snapshot = &cp
	}
	d.mu.Unlock()
	if !updated {
		slog.Warn("DIAG: subs-creds-persist-miss", "component", "daemon")
		return
	}
	slog.Info("DIAG: subs-creds-persist", "component", "daemon")
	d.saveLifecycleState(snapshot, state)
}

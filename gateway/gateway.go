//go:build linux

package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/engine"
	"github.com/kaitu-io/k2/safego"
	"github.com/kaitu-io/k2/subscription"
	"github.com/kaitu-io/k2/webui"
	"github.com/sasha-s/go-deadlock"
)

// Gateway is the k2r daemon lifecycle manager. It owns the engine, interceptor,
// provider, and sysctl state. HTTP API on DefaultGatewayAddr exposes control
// (up/down/status) and SSE event streaming.
//
// Lock ordering: opMu (serialization) -> mu (state) -> sseMu (state).
// opMu serializes doUp/doDown (may block on engine start/stop).
// mu protects field reads/writes only.
type Gateway struct {
	opMu deadlock.Mutex // serialization: doUp/doDown (never nested inside mu)
	mu   deadlock.Mutex // state: protects fields below

	eng         *engine.Engine
	lastStatus  engine.Status
	interceptor Interceptor
	provider    *gatewayProvider
	sysctl      sysctlState

	httpServer *http.Server
	sseClients map[chan []byte]struct{}
	sseMu      deadlock.RWMutex

	ctx    context.Context
	cancel context.CancelFunc

	// engineCtx is a per-session context derived from ctx in doUp and
	// cancelled in closeTunnel. Subscription refresh loops use it so they
	// stop on engine teardown without relying solely on sub.Close()
	// (defense-in-depth: ctx cancel + sub.Close both terminate refresh).
	engineCtx context.Context
	engineCnl context.CancelFunc

	storage         *webui.Storage
	version         string
	commit          string
	arch            string
	beaconBase      string // Center base URL for public-IP discovery beacon ("" disables)
	routerDeviceMgr *RouterDeviceManager
	updater         *webui.Upgrader

	// Subscription manager — handles k2subs:// routes, OutboundProvider
	// hot-swap, exclude state. Cleared on doDown via CloseAll.
	subsMgr *subscription.Manager

	// controlKeyHash gates the panel HTTP API ("" = unbound). Authority is
	// Center via the subs channel; see reconcileControlKey. mu-protected.
	controlKeyHash   string
	controlKeyLoaded bool
}

// k2rReleaseEndpoints lists the CDN base URLs used by k2r's OTA upgrader.
// Tried in order; first successful LATEST + checksums.txt fetch wins.
var k2rReleaseEndpoints = []string{
	"https://d13jc1jqzlg4yt.cloudfront.net/kaitu/k2r",
	"https://dl.kaitu.io/kaitu/k2r",
}

// newK2rUpgrader wires a webui.Upgrader with k2r-specific CDN, state, and
// restart configuration. RestartFunc is detected at call time so OpenWRT
// hosts use procd while systemd hosts use systemctl.
func newK2rUpgrader(version string) *webui.Upgrader {
	return webui.NewUpgrader(version, webui.UpgradeConfig{
		BinaryName:       "k2r",
		StateDir:         stateDir(),
		ReleaseEndpoints: k2rReleaseEndpoints,
		RestartFunc: func() {
			if webui.IsOpenWRT() {
				exec.Command("/etc/init.d/k2r", "restart").Run()
			} else {
				exec.Command("systemctl", "restart", "k2r").Run()
			}
		},
	})
}

// New creates a Gateway ready to Run. beaconBase is the Center base URL the
// public-IP discovery beacon POSTs to ("" disables the beacon).
func New(version, commit, arch, beaconBase string) *Gateway {
	ctx, cancel := context.WithCancel(context.Background())
	g := &Gateway{
		ctx:        ctx,
		cancel:     cancel,
		lastStatus: engine.Status{State: engine.StateDisconnected},
		sseClients: make(map[chan []byte]struct{}),
		storage:    webui.NewStorage(filepath.Join(stateDir(), "storage.json")),
		version:    version,
		commit:     commit,
		arch:       arch,
		beaconBase: beaconBase,
		updater:    newK2rUpgrader(version),
	}
	g.subsMgr = subscription.NewManager(
		filepath.Join(stateDir(), "subs"),
		nil,
		nil, // probe.Registry is daemon-only for now; gateway can wire later
	)
	return g
}

// Run starts the HTTP API server and blocks until Stop is called or the
// listener fails. It attempts auto-reconnect from saved state.
//
// Note: stale firewall rules from a previous crashed daemon are NOT cleaned
// at startup — that would tear down the previous chain immediately, opening
// a multi-second LAN-escape window before auto-reconnect's first Install can
// restore protection. Install's pre-flight Remove handles stale state at the
// correct moment (back-to-back replacement). See startupRecovery + Bug 6 fix.
func (g *Gateway) Run(addr string) error {
	defer g.handlePanic()

	g.startupRecovery()

	if g.updater != nil {
		g.updater.RecoverOnStartup()
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/ping", g.handlePing)
	mux.HandleFunc("/api/core", g.handleCore)
	mux.HandleFunc("/api/events", g.handleEvents)
	mux.HandleFunc("/api/log-level", g.handleLogLevel)

	// Shared webui endpoints (platform, upgrade) — same handlers as cmd/k2
	// (Linux desktop) uses via the daemon package.
	mux.HandleFunc("/api/platform", webui.NewPlatformHandler(webui.PlatformInfo{
		PlatformType: "gateway",
		Version:      g.version,
		Commit:       g.commit,
		Arch:         g.arch,
	}))
	mux.HandleFunc("/api/upgrade", webui.NewUpgradeHandler(g.updater))

	// Router device management (state resolved per-request — nil until doUp).
	mux.HandleFunc("/api/router-devices", g.handleRouterDevices)
	mux.HandleFunc("/api/router-devices/allow", g.handleRouterDeviceAllow)
	mux.HandleFunc("/api/router-devices/remove", g.handleRouterDeviceRemove)
	mux.HandleFunc("/api/router-devices/mode", g.handleRouterDeviceMode)

	if webapp := webui.NewWebappHandler(g.version, g.commit, g.arch); webapp != nil {
		// Non-nowebapp dev builds only: k2r production always ships with
		// -tags nowebapp, so this branch never runs there. requireAuth
		// exempts only the exact "/" path, so the embedded SPA's /assets/*
		// requests get 401 from a LAN origin (and browsers can't attach a
		// Bearer token to asset fetches anyway) — the panel only fully
		// works when loaded from loopback.
		mux.Handle("/", webapp)
	} else {
		mux.HandleFunc("/", handleRootNotice)
	}

	g.httpServer = &http.Server{Handler: g.requireAuth(mux)}

	safego.Go(g.tryAutoReconnect)

	// Bind loopback + every private/LAN interface IP so LAN devices can reach
	// the panel — WAN/public IPs are excluded (control plane never exposed to
	// the internet). Port is parsed from addr (default cfg.Listen → :1779).
	_, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)
	if port == 0 {
		port = 1779
	}
	binds := resolveLANBindAddrs(enumerateIfaceAddrs(), port)
	slog.Info("gateway: HTTP API binding", "addrs", binds)

	// One shared http.Server serves every listener (http.Server.Serve is safe
	// to call concurrently for multiple listeners). A bind failure on a single
	// interface is logged and skipped — loopback always remains.
	// Buffer one slot per bind: on Shutdown, net/http closes every listener this
	// server is serving, so all Serve goroutines return at once and must each be
	// able to send without blocking (else bound-1 goroutines leak at shutdown).
	errCh := make(chan error, len(binds))
	bound := 0
	for _, b := range binds {
		ln, err := net.Listen("tcp", b)
		if err != nil {
			slog.Warn("gateway: bind failed", "addr", b, "err", err)
			continue
		}
		bound++
		safego.Go(func() { errCh <- g.httpServer.Serve(ln) })
	}
	if bound == 0 {
		return fmt.Errorf("gateway: no panel listener could bind (tried %v)", binds)
	}

	// mDNS responder: answer k2.local A queries with the LAN IP so users can
	// reach the panel at http://k2.local:<port> without knowing the IP.
	g.startMDNS()

	// Public-IP discovery beacon: periodically report our LAN IP to Center so
	// webapp can find the router under the same public egress. Unconditional —
	// fires even without configured credentials (discovery is the whole point).
	g.startBeacon(g.beaconBase, port)

	select {
	case <-g.ctx.Done():
		// Daemon-internal shutdown (procd SIGTERM, systemctl restart, etc.).
		// userInitiated=false so state.json keeps {State:"connected", Config}
		// — next start's tryAutoReconnect resumes the tunnel automatically.
		g.doDown(false)
		shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutCancel()
		g.httpServer.Shutdown(shutCtx)
		return nil
	case err := <-errCh:
		return err
	}
}

// Stop cancels the gateway context, triggering graceful shutdown of the
// HTTP server and tunnel.
func (g *Gateway) Stop() {
	g.cancel()
}

// doUp connects with the given config. Acquires opMu to serialize.
func (g *Gateway) doUp(cfg *GatewayConfig) error {
	g.opMu.Lock()
	defer g.opMu.Unlock()

	g.closeTunnel()

	var subnets []string
	var err error
	if len(cfg.LANSubnets) > 0 {
		subnets = cfg.LANSubnets
	} else {
		subnets, err = DiscoverLANSubnets()
		if err != nil {
			return err
		}
	}
	slog.Info("DIAG: gw-lan-discover", "subnets", subnets, "ifaceCount", len(subnets))

	// Resolve k2subs:// routes BEFORE computing TPROXY exclude IPs so the
	// exclude list contains the actual k2v5 wire server IPs (not empty or the
	// k2subs API host). Sessions are owned by g.subsMgr; cleanup is deferred
	// until either an early-return path or (on success) doDown via
	// closeTunnel/CloseAll.
	subs, resolved, subsErr := g.subsMgr.Resolve(&cfg.ClientConfig)
	if subsErr != nil {
		return fmt.Errorf("gateway: subscription resolve: %w", subsErr)
	}
	subsCleanup := true
	defer func() {
		if subsCleanup {
			g.subsMgr.CloseAll()
		}
	}()

	// Engine runs against the resolved client (k2subs:// → k2v5://). The
	// original cfg.ClientConfig keeps the k2subs:// route for persistence so
	// a restart re-fetches the subscription.
	engineClient := subscription.ApplyResolved(&cfg.ClientConfig, resolved)

	dialTargets, err := extractDialTargets(engineClient.Routes)
	if err != nil {
		return err
	}
	excludeIPs, err := resolveExcludeIPs(dialTargets, engineClient.DNS.Direct)
	if err != nil {
		return err
	}

	var sc sysctlState
	if err := sc.Enable(); err != nil {
		return err
	}

	interceptor, err := NewInterceptor()
	if err != nil {
		sc.Restore()
		return err
	}

	// Initialize RouterDeviceManager with quota from user Tier
	rdm := &RouterDeviceManager{
		quota:       cfg.MaxLanClient,
		lanSubnets:  subnets,
		interceptor: interceptor,
		storage:     g.storage,
	}
	rdm.loadFromStorage()

	// Build extended InterceptConfig with all router features
	icfg := InterceptConfig{
		ListenPort:    cfg.ListenPort,
		LANSubnets:    subnets,
		ExcludeIPs:    excludeIPs,
		DNSRedirect:   cfg.DNSRedirect,
		DNSPort:       cfg.DNSPort,
		AllowlistMode: rdm.mode == "allowlist",
		AllowedMACs:   rdm.getAllowedMACs(),
	}

	// Set mode change callback — triggers interceptor reinstall
	rdm.onModeChange = func() {
		rdm.mu.RLock()
		newICfg := icfg
		newICfg.AllowlistMode = rdm.mode == "allowlist"
		newICfg.AllowedMACs = make([]string, len(rdm.entries))
		for i, e := range rdm.entries {
			newICfg.AllowedMACs[i] = e.MAC
		}
		rdm.mu.RUnlock()

		g.mu.Lock()
		interceptor := g.interceptor
		g.mu.Unlock()

		if interceptor != nil {
			interceptor.Remove()
			if err := interceptor.Install(newICfg); err != nil {
				slog.Error("gateway: failed to reinstall interceptor after mode change", "err", err)
			}
		}
	}

	prov := newGatewayProvider(interceptor, icfg)

	eng := engine.New()
	eng.SetEventHandler(g)
	ecfg := engineConfig(engineClient, prov, g.broadcastSnapshot, g.subsMgr)
	if err := eng.Start(ecfg); err != nil {
		prov.Close()
		interceptor.Remove()
		sc.Restore()
		return err
	}
	subsCleanup = false // engine owns sessions until doDown/closeTunnel

	// Per-session context for refresh goroutines — cancelled in closeTunnel
	// so background fetches stop even if sub.Close() is delayed. Mirrors
	// daemon.go's engineCtx pattern.
	engineCtx, engineCnl := context.WithCancel(g.ctx)

	// Start background subscription refresh — reuse the per-session HTTP
	// client built in subsMgr.Resolve so refresh fetches go through the
	// same Transport (connection pool) that Session.Close will later
	// release via CloseIdleConnections. Mirrors daemon.go.
	clientHeader := fmt.Sprintf("kaitu-router/%s (linux; %s)", g.version, g.arch)
	for _, sess := range subs {
		client := sess.HTTPClient()
		sub := sess.Subscription()
		// Identify as a k2r router so Center's /api/subs routes to the gateway
		// branch on every refresh fetch.
		sub.SetClientHeader(clientHeader)
		// Center is the control-key authority: adopt the hash it delivered in
		// the Resolve-time fetch, and keep adopting on every background refresh
		// (rotation convergence).
		sub.SetOnControlKeyHash(g.reconcileControlKey)
		g.reconcileControlKey(sub.ControlKeyHash())
		sub.StartRefresh(engineCtx, func() *http.Client { return client })
	}

	g.mu.Lock()
	g.eng = eng
	g.interceptor = interceptor
	g.provider = prov
	g.sysctl = sc
	g.routerDeviceMgr = rdm
	g.engineCtx = engineCtx
	g.engineCnl = engineCnl
	g.mu.Unlock()

	saveState(&persistedState{Config: cfg, State: "connected", Timestamp: time.Now()})
	return nil
}

// doDown disconnects and cleans up. Acquires opMu to serialize.
//
// userInitiated distinguishes two shutdown semantics:
//   - true: user explicitly ran `k2r down`. State.json gets {State:
//     "disconnected", Config:nil} so the next start does NOT auto-reconnect —
//     the user wanted to stay disconnected.
//   - false: daemon-internal shutdown (ctx.Done() from SIGTERM / procd
//     restart / Linux reboot). State.json is left untouched so the previous
//     doUp's {State:"connected", Config} survives — next start's
//     tryAutoReconnect resumes the tunnel.
//
// Both paths close the tunnel + drain subscription sessions. They differ
// only in whether they persist a disconnect intent.
func (g *Gateway) doDown(userInitiated bool) {
	g.opMu.Lock()
	defer g.opMu.Unlock()
	g.closeTunnel()
	// Drain any active subscription sessions. Engine.Stop() (called via
	// closeTunnel) does not own subscription state — Manager does — so the
	// shell must close them explicitly. Idempotent: safe when no sessions
	// were ever resolved (e.g. doDown without prior doUp). subsMgr is only
	// nil for a zero-value Gateway (never New()-constructed) — handleReset
	// calls doDown best-effort on whatever Gateway it's given, so guard it.
	if g.subsMgr != nil {
		g.subsMgr.CloseAll()
	}
	if userInitiated {
		saveState(&persistedState{State: "disconnected", Timestamp: time.Now()})
	}
	// else: preserve whatever state.json the last doUp wrote — auto-reconnect
	// on next start is the intent.
}

// wipeBinding removes the on-disk binding then clears the in-memory key.
func (g *Gateway) wipeBinding() error {
	if err := WipeBindingFiles(); err != nil {
		return err
	}
	g.mu.Lock()
	g.controlKeyHash = ""
	g.controlKeyLoaded = true
	g.mu.Unlock()
	return nil
}

// WipeBindingFiles removes credential + state + control key from stateDir.
// Exported for the k2r CLI so `k2r reset` also works with the daemon stopped.
func WipeBindingFiles() error {
	for _, p := range []string{defaultConfigPath(), statePath()} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return clearControlKeyHash()
}

// closeTunnel extracts resources under mu, then closes outside lock.
// Caller must hold opMu.
func (g *Gateway) closeTunnel() {
	g.mu.Lock()
	eng := g.eng
	g.eng = nil
	interceptor := g.interceptor
	g.interceptor = nil
	g.provider = nil
	sc := g.sysctl
	g.sysctl = sysctlState{}
	g.routerDeviceMgr = nil
	engineCnl := g.engineCnl
	g.engineCnl = nil
	g.engineCtx = nil
	g.mu.Unlock()

	// Cancel per-session ctx first so subscription refresh goroutines stop
	// independently of sub.Close (called via CloseAll in doDown). No-op on
	// idempotent re-entry — engineCnl is nilled under mu above.
	if engineCnl != nil {
		engineCnl()
	}
	// Tear down iptables + sysctl BEFORE engine.Stop fires
	// OnStatus(disconnected). The previous order let observers see
	// state=disconnected while PREROUTING still had K2R refs (~100 ms race
	// on MIPS), during which LAN traffic kept being TPROXY'd to the
	// listener being torn down. Bug 9 fix (2026-05-21).
	if interceptor != nil {
		interceptor.Remove()
	}
	sc.Restore()
	if eng != nil {
		eng.Stop()
	}
}

// --- EventHandler impl (engine.EventHandler) ---

// OnStatus implements engine.EventHandler.
func (g *Gateway) OnStatus(status engine.Status) {
	g.mu.Lock()
	g.lastStatus = status
	g.mu.Unlock()
	g.broadcastStatus(status)
}

// OnStats implements engine.EventHandler.
func (g *Gateway) OnStats(tx, rx int64) {}

// OnOutboundFatal implements engine.EventHandler. Fired when the
// OutboundProvider (subsMgr) returns no further candidates and the engine
// has terminated the tunnel. Gateway logs and updates lastStatus; the SSE
// broadcast happens via OnStatus's normal path.
func (g *Gateway) OnOutboundFatal(outbound string, err error) {
	slog.Warn("gateway: outbound fatal", "url", config.TruncateVia(outbound), "err", err)
}

func (g *Gateway) broadcastSnapshot(snap engine.HealthSnapshot) {
	data, _ := json.Marshal(snap)
	g.broadcastSSE("stats", data)
}

func (g *Gateway) broadcastStatus(status engine.Status) {
	data, _ := json.Marshal(status)
	g.broadcastSSE("status", data)
}

// --- Recovery ---

// startupRecovery is the startup-phase recovery hook invoked from Run before
// the HTTP API binds and auto-reconnect kicks in.
//
// Intentionally empty re: interceptor state. Previously this eagerly removed
// any pre-existing K2R chain on startup (under the theory that a crashed
// daemon left bad state). In practice that caused an ~8 s LAN-escape window
// — measured 2026-05-21 on GL-XE300 MIPS after `kill -9`: startup-Remove
// fired at T+0, but Install could not re-add until auto-reconnect's 3 s
// settle + transport race (3-5 s on real RTT) completed.
//
// The pre-existing chain — if any — points TPROXY at a port whose listener
// died with the previous daemon. Packets arriving at that port are dropped
// by the kernel = fail-CLOSED, the correct security posture for a VPN
// router. Install's pre-flight Remove + atomic re-add then takes over the
// moment auto-reconnect succeeds, with only a microsecond window.
//
// If a future startup-time recovery task is needed (cleaning some other
// resource, validating a non-iptables artifact), add it here.
func (g *Gateway) startupRecovery() {
	// intentionally empty: stale iptables state is cleaned by Install's
	// pre-flight Remove, not here. See Bug 6 fix.
}

// handlePanic is a deferred recovery handler that cleans up firewall rules
// and sysctl on panic.
func (g *Gateway) handlePanic() {
	if r := recover(); r != nil {
		slog.Error("gateway: panic", "err", r, "stack", string(debug.Stack()))
		g.mu.Lock()
		interceptor := g.interceptor
		g.interceptor = nil
		sc := g.sysctl
		g.sysctl = sysctlState{}
		g.mu.Unlock()
		if interceptor != nil {
			interceptor.Remove()
		}
		sc.Restore()
	}
}

// autoReconnectBackoff is the schedule of delays between auto-reconnect
// attempts. After this list is exhausted, the loop keeps retrying at the
// final cap interval indefinitely (until g.ctx is cancelled). 3 s first
// to let the HTTP server + kernel network stack settle after boot.
//
// Variable rather than const so unit tests can shrink it without changing
// the production schedule.
var autoReconnectBackoff = []time.Duration{
	3 * time.Second,
	15 * time.Second,
	30 * time.Second,
	60 * time.Second,
	120 * time.Second,
	300 * time.Second,
}

// tryAutoReconnect loads persisted state and reconnects if the gateway was
// previously connected within the last 7 days. Retries with bounded backoff
// (autoReconnectBackoff) then steady-state at the cap interval until either
// reconnect succeeds or g.ctx is cancelled.
func (g *Gateway) tryAutoReconnect() {
	g.runAutoReconnectLoop(g.ctx, g.doUp, autoReconnectBackoff)
}

// runAutoReconnectLoop is the testable core of tryAutoReconnect. attempt is
// invoked between sleeps from the backoff slice. After backoff is exhausted,
// the final delay is reused indefinitely. Returns on first attempt success
// or on ctx cancel.
//
// Invariants:
//   - attempt is never called if state.json fails the connected+config gate
//     (mirrors the original tryAutoReconnect bail-out).
//   - ctx.Done() between sleeps interrupts immediately; in-flight attempts
//     are NOT cancelled (doUp owns its own context lifecycle).
//   - Empty backoff slice → no-op (returns immediately).
func (g *Gateway) runAutoReconnectLoop(
	ctx context.Context,
	attempt func(*GatewayConfig) error,
	backoff []time.Duration,
) {
	st := loadState()
	if st == nil || st.State != "connected" || st.Config == nil {
		return
	}
	if time.Since(st.Timestamp) > 7*24*time.Hour {
		return
	}
	if len(backoff) == 0 {
		return
	}
	// Derive Server from routes (Server is json:"-", not persisted).
	SetGatewayDefaults(st.Config)

	sleepThen := func(d time.Duration) bool {
		select {
		case <-time.After(d):
			return true
		case <-ctx.Done():
			return false
		}
	}

	tryOnce := func(n int) bool {
		if n == 1 {
			slog.Info("gateway: auto-reconnecting from saved state")
		} else {
			slog.Info("gateway: auto-reconnect retrying", "attempt", n)
		}
		if err := attempt(st.Config); err != nil {
			slog.Warn("gateway: auto-reconnect failed", "attempt", n, "err", err)
			return false
		}
		return true
	}

	var n int
	for _, delay := range backoff {
		if !sleepThen(delay) {
			return
		}
		n++
		if tryOnce(n) {
			return
		}
	}

	// Steady-state retries at the cap interval.
	cap := backoff[len(backoff)-1]
	for {
		if !sleepThen(cap) {
			return
		}
		n++
		if tryOnce(n) {
			return
		}
	}
}

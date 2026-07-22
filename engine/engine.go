package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/sasha-s/go-deadlock"
	"golang.org/x/sync/errgroup"

	"github.com/kaitu-io/k2-rules/krs"
	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/core"
	"github.com/kaitu-io/k2/core/dns"
	"github.com/kaitu-io/k2/provider"
	"github.com/kaitu-io/k2/rule"
	"github.com/kaitu-io/k2/safego"
	"github.com/kaitu-io/k2/wire"
)

// Well-known outbound indices (convention for outbound table layout).
const (
	outboundDirect = rule.Target(0)
	outboundReject = rule.Target(1)
	// 2+ = wire outbounds (k2v5, k2p, etc.), assigned dynamically.
)

// Engine is the unified tunnel lifecycle manager. It assembles the tunnel
// stack (wire transports, router, DNS middleware, provider) and manages the
// connection lifecycle for both desktop and mobile platforms.
//
// Lock ordering: engine.mu must be acquired before any EventHandler lock
// (e.g. daemon.mu). See CLAUDE.md Lock Ordering Graph.
// handler.OnStatus is called OUTSIDE engine.mu (read-copy-act pattern).
type Engine struct {
	mu          deadlock.Mutex
	state       string
	paused      bool
	client      *config.ClientConfig
	connectedAt time.Time
	lastError   *EngineError
	handler     EventHandler

	cancel         context.CancelFunc
	tunnel         *core.ClientTunnel
	prov           provider.Provider
	networkMonitor NetworkChangeNotifier
	health         *healthMonitor
	netCoord       *netCoordinator
	dnsH           *dnsHandler // for counter reset on network recovery
	proxyDNS       *dns.ProxyDNSClient // proxy DNS connection pool (nil in proxy mode)
	ruleEngine     *rule.Engine
	outbounds      []core.Outbound // owned by engine, closed in Stop()

	outboundTMs  []*outboundTransport // all wire TMs, for health/reconnect
	primaryTM    *outboundTransport   // first k2v5 route's TM (DNS transport)
	directDialer wire.DirectDialer
	reRaceChan   chan *outboundTransport
	reRaceCancel context.CancelFunc

	// wireMode and outboundProvider are snapshotted from Config at Start so
	// the background outboundReplaceLoop can build replacement TMs without
	// re-reading user-supplied Config (which may be mutated by the caller).
	wireMode         string
	outboundProvider OutboundProvider

	// missReporter is non-nil only when client.Telemetry.RuleMiss.Enabled
	// is true. When nil, the rule engine's MissReporter hook is also nil
	// and the match hot path has zero rule-miss overhead. Phase 1.
	missReporter *missReporter
}

// outboundTransport holds a TransportManager and its wire config for one k2v5 outbound.
// Engine-internal — not exported.
type outboundTransport struct {
	tm       *wire.TransportManager
	wireCfg  wire.Config
	dialAddr string
	name     string // log label, e.g. "k2v5-jp.example.com"

	// url is the original k2v5:// URL that produced this outbound. Used by
	// outboundReplaceLoop to ask the OutboundProvider for a replacement
	// when this tunnel server persistently fails. Updated atomically via
	// e.mu on hot-swap.
	url string

	// swappable wraps tm for indirection at the wire.Dialer boundary. All
	// outward-facing Dial traffic (core.WireOutbound, ProxyDNSClient) goes
	// through swappable; engine-internal TransportManager methods
	// (Reset, ReplacePrimary, WireStats, Primary, Close, etc.)
	// continue to use tm directly.
	//
	// Ownership: tm is engine-owned (Closed in engine.Stop OR in
	// outboundReplaceLoop when a hot-swap retires it). swappable holds a
	// reference to tm for Dial forwarding only — it must never be Close()d
	// separately.
	swappable *wire.SwappableDialer

	// reRaceFailStreak counts consecutive wire-level RaceTransport
	// failures against this outbound's current URL. When it reaches
	// reRaceFailThresholdForReplace, outboundReplaceLoop escalates from
	// "re-race wire types against same server" to "ask OutboundProvider
	// for a different server". Reset to 0 on any RaceTransport success
	// or successful hot-swap. Guarded by e.mu.
	reRaceFailStreak int
}

// reRaceFailThresholdForReplace is the RaceTransport-failure count after
// which outboundReplaceLoop gives up on the current tunnel server and asks
// OutboundProvider for a replacement. 2 means the engine tolerates a brief
// transient (e.g. single sleep/wake window) before rotating servers.
const reRaceFailThresholdForReplace = 2

// New creates a new Engine in the disconnected state.
func New() *Engine {
	return &Engine{
		state: StateDisconnected,
	}
}

// SetEventHandler registers a callback receiver for state changes,
// errors, and traffic stats. Pass nil to clear.
func (e *Engine) SetEventHandler(h EventHandler) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.handler = h
}

// Status returns the current engine state.
func (e *Engine) Status() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.state
}

// Start assembles the tunnel stack and begins forwarding traffic.
// The Config determines whether this is a desktop or mobile tunnel:
//   - FileDescriptor >= 0: mobile path (platform-provided TUN FD, DNS interception)
//   - FileDescriptor < 0:  desktop path (self-created TUN or proxy provider)
func (e *Engine) Start(cfg Config) error {
	// Failure guard: from this call on, Start owns cfg.FileDescriptor (mobile:
	// a dup'd TUN fd whose cleanup appext has already relinquished) and, once
	// built, the provider wrapping it. Every failure return must release them —
	// a leaked TUN fd keeps the platform VPN up routing into a dead tunnel
	// (Android: the bypassable=false default route blackholes the whole device
	// until the app process dies). Mirrors the missRep guard below: both
	// locals are cleared at commit, making the defer a no-op on success.
	// Runs lock-free: every return path unlocks e.mu first.
	cleanupFD := cfg.FileDescriptor
	var cleanupProv provider.Provider
	defer func() {
		if cleanupProv != nil {
			cleanupProv.Close() // releases the FD, consumed by sing-tun or not
		} else if cleanupFD > 0 {
			// Provider not built yet — release the raw fd. os.NewFile keeps
			// this portable (the path is unreachable on Windows).
			_ = os.NewFile(uintptr(cleanupFD), "k2-tun-fd").Close()
		}
	}()

	e.mu.Lock()
	if e.state != StateDisconnected {
		e.mu.Unlock()
		return fmt.Errorf("engine: already started")
	}
	client := cfg.Client
	if client == nil {
		e.mu.Unlock()
		return fmt.Errorf("engine: Client config is required")
	}
	e.client = client
	e.lastError = nil

	// Validate route CIDR conflicts (e.g. k2p route overlaps local LAN).
	if err := config.ValidateRouteCIDRConflicts(client.Routes); err != nil {
		e.mu.Unlock()
		return e.fail(fmt.Errorf("route validation: %w", err))
	}

	// Create context early so Stop() can cancel an in-progress Start().
	ctx, cancel := context.WithCancel(context.Background())
	e.cancel = cancel

	e.setStateLocked(StateConnecting)
	handler := e.handler
	status := e.buildStatusLocked()
	e.mu.Unlock()
	if handler != nil {
		handler.OnStatus(status)
	}

	startTime := time.Now()

	// 1. Rule bundles: ensure on disk before building the rule engine.
	// Cold start: downloads the tarball from DefaultSources (up to 10s
	// hard timeout). Warm cache: ~microsecond mtime check, no HTTP.
	//
	// If fresh download fails but stale .krs bundles are already on disk (from
	// the embedded seed above or a prior successful connect), fall through to
	// the stale cache — the background StartUpdater (started below) hot-reloads
	// via rule.Engine.Reload() once a mirror becomes reachable. Only true
	// cold-start with no bundles at all is fatal, preserving the routes=0
	// all-direct guard.
	sources := client.RuleSources
	if len(sources) == 0 {
		sources = rule.DefaultSources
	}
	if ee := e.prefetchRulesForStart(ctx, client, sources, cfg.NetworkExtension); ee != nil {
		return e.fail(ee)
	}

	// 1a. Fail loud if a configured match.region route has no usable bundle.
	// Without this guard the route is silently dropped (engine.go buildRouteEntries)
	// and every connection falls through to the proxy fallback — sending all
	// in-region traffic abroad and overloading the exit (the all-proxy incident).
	// 504 is retryable: isVersionFresh now re-downloads a poisoned cache, the
	// background updater hot-reloads, and the next connect succeeds.
	if client.CacheDir != "" {
		if miss := missingConfiguredRegions(client.Routes, client.CacheDir); len(miss) > 0 {
			slog.Warn("DIAG: rule-bundle-missing-region", "regions", miss, "cacheDir", client.CacheDir)
			return e.fail(&EngineError{
				Code:     ErrCodeRuleBundlesUnavailable,
				Category: CategoryServer,
				Message:  fmt.Sprintf("required region bundle(s) not loaded: %v", miss),
			})
		}
	}

	// 1b. Build outbound map (via URL → Target index) and rule engine.
	outboundMap := buildOutboundMap(client.Routes)
	ruleEngine := buildRuleEngine(client, outboundMap, cfg.NetworkExtension)

	// 1c. (Phase 1 dark instrumentation) Wire rule-miss telemetry reporter
	// ONLY when explicitly opted in via config. Zero-value config = off,
	// so by default the rule engine's MissReporter stays nil and the match
	// hot path is unaffected. See k2/engine/miss_reporter.go for design.
	var missRep *missReporter
	if client.Telemetry.RuleMiss.Enabled {
		missRep = newMissReporter(client.Telemetry.RuleMiss)
		missRep.start()
		ruleEngine.SetMissReporter(missRep)
		slog.Info("engine: rule-miss telemetry enabled",
			"endpoint", client.Telemetry.RuleMiss.Endpoint,
			"country", client.Telemetry.RuleMiss.Country,
		)
	}
	// If Start() fails anywhere after this point, stop the reporter so
	// its goroutine doesn't leak. On success (commit below) this local
	// is set to nil so the defer becomes a no-op.
	defer func() {
		if missRep != nil {
			missRep.Stop()
		}
	}()

	slog.Info("engine: start",
		"server", extractServerHost(client.Server),
		"mode", client.Mode,
		"disableSniff", ruleEngine.DisableSniff(),
		"dnsDirect", client.DNS.Direct,
		"dnsProxy", client.DNS.Proxy,
		"fd", cfg.FileDescriptor,
		"hasDirectDialer", cfg.DirectDialer != nil,
		"hasNetworkMonitor", cfg.NetworkMonitor != nil,
	)
	slog.Info("engine: step 1 - rule engine init done", "elapsed", time.Since(startTime))

	// 2.5. Start network monitor — DirectDialer needs DefaultInterface()
	// for interface-bound dialing. Must start before Happy Eyeballs (step 3).
	// Safe: OnNetworkChanged() early-returns when state != connected.
	if cfg.NetworkMonitor != nil {
		if err := cfg.NetworkMonitor.Start(func() {
			e.mu.Lock()
			nc := e.netCoord
			e.mu.Unlock()
			if nc != nil {
				nc.handleEvent(&NetEvent{Signal: SignalChanged, Source: "singtun"})
			}
		}); err != nil {
			slog.Warn("engine: network monitor start failed (non-fatal)", "err", err)
		}
	}

	// 5. Router.
	router := core.NewRouter(ruleEngine)

	// 6. Build provider.
	var prov provider.Provider
	if cfg.Provider != nil {
		// External provider (gateway/).
		prov = cfg.Provider
	} else if cfg.FileDescriptor >= 0 {
		// Mobile / macOS sysext: platform-provided TUN FD.
		prov = provider.NewTUNProvider(provider.ProviderConfig{
			FileDescriptor:   cfg.FileDescriptor,
			NetworkExtension: cfg.NetworkExtension,
			InterfaceMonitor: cfg.InterfaceMonitor,
			IPv4Address:      client.Tun.IPv4,
			IPv6Address:      client.Tun.IPv6,
		})
	} else if client.Mode == "proxy" {
		// Desktop proxy mode.
		prov = provider.NewProxyProvider(provider.ProviderConfig{
			Mode:        "proxy",
			ProxyListen: client.Proxy.Listen,
		})
	} else {
		// Desktop TUN mode (default).
		prov = provider.NewTUNProvider(provider.ProviderConfig{
			Mode:              "tun",
			RouteExcludeAddrs: computeDNSExclude(client),
			InterfaceMonitor:  cfg.InterfaceMonitor,
			IPv4Address:       client.Tun.IPv4,
			IPv6Address:       client.Tun.IPv6,
		})
	}

	if cfg.Provider == nil {
		// Engine-created provider: the failure guard closes it (and the
		// platform FD it wraps). External providers (gateway) keep their own
		// pre-commit lifecycle — creator destroys.
		cleanupProv = prov
	}
	cleanupFD = -1 // fd ownership (if any) now lives inside prov

	slog.Debug("engine: step 6 - provider created", "provider", prov.Name(), "mode", client.Mode, "elapsed", time.Since(startTime))

	// Check cancellation before starting the tunnel (the expensive part).
	if ctx.Err() != nil {
		if cfg.NetworkMonitor != nil {
			cfg.NetworkMonitor.Close()
		}
		return e.fail(ctx.Err())
	}

	// 6.5. Build per-outbound transports and outbound table.
	//
	// Each unique k2v5:// URL gets its own TransportManager via setupOutboundTransport.
	// Multiple URLs race in parallel (errgroup). buildOutboundMap already deduplicates.
	outbounds := make([]core.Outbound, 2+len(outboundMap))
	outbounds[outboundDirect] = core.NewDirectOutbound(cfg.DirectDialer)
	outbounds[outboundReject] = nil

	// Collect unique k2v5 URLs in config route order (deterministic).
	// IMPORTANT: iterate client.Routes (ordered slice), NOT outboundMap (unordered map).
	// The first k2v5 route becomes primaryTM (DNS transport), so order matters.
	seen := make(map[string]bool)
	var k2v5URLs []string
	for _, route := range client.Routes {
		via := route.Via
		if !strings.HasPrefix(via, "k2v5://") || seen[via] {
			continue
		}
		if _, inMap := outboundMap[via]; !inMap {
			continue
		}
		seen[via] = true
		k2v5URLs = append(k2v5URLs, via)
	}

	// Setup transports in parallel.
	type otResult struct {
		ot          *outboundTransport
		tcpwsDialer wire.Dialer
		err         error
	}
	otResults := make([]otResult, len(k2v5URLs))

	if len(k2v5URLs) > 0 {
		var g errgroup.Group
		for i, via := range k2v5URLs {
			i, via := i, via
			g.Go(func() error {
				ot, tcpws, err := setupOutboundTransport(ctx, via, cfg.DirectDialer, cfg.WireMode)
				otResults[i] = otResult{ot: ot, tcpwsDialer: tcpws, err: err}
				return nil // errors collected in results, not errgroup
			})
		}
		g.Wait()
	}

	// Check results and clean up on any failure.
	tmMap := make(map[string]*outboundTransport, len(k2v5URLs))
	var allOTs []*outboundTransport
	var primaryOT *outboundTransport
	var tcpwsDialer wire.Dialer

	for i, r := range otResults {
		if r.err != nil {
			// Close ALL successfully-created TMs (errgroup runs concurrently,
			// so indices after i may also have succeeded).
			for _, other := range otResults {
				if other.ot != nil {
					other.ot.tm.Close()
				}
			}
			if cfg.NetworkMonitor != nil {
				cfg.NetworkMonitor.Close()
			}
			return e.fail(fmt.Errorf("outbound setup: %w", r.err))
		}
		tmMap[k2v5URLs[i]] = r.ot
		allOTs = append(allOTs, r.ot)
		if primaryOT == nil {
			primaryOT = r.ot
			tcpwsDialer = r.tcpwsDialer
		}
	}

	// TUN mode requires at least one k2v5 outbound for DNS proxy transport.
	// Before the per-outbound refactor, wire.ParseURL(client.Server) in step 2
	// implicitly enforced this. Now that step 2 is merged into step 6.5, we need
	// an explicit check.
	if primaryOT == nil && client.Mode != "proxy" {
		if cfg.NetworkMonitor != nil {
			cfg.NetworkMonitor.Close()
		}
		return e.fail(fmt.Errorf("no k2v5 outbound configured — TUN mode requires at least one"))
	}

	slog.Info("engine: step 6.5 — outbound transports ready",
		"k2v5Outbounds", len(allOTs),
		"elapsed", time.Since(startTime),
	)

	// Build outbound table.
	for via, target := range outboundMap {
		switch {
		case strings.HasPrefix(via, "k2v5://"):
			ot := tmMap[via]
			outbounds[target] = core.NewWireOutbound(ot.name, ot.swappable, e)
		case strings.HasPrefix(via, "k2p://"):
			k2pOut, k2pErr := core.NewK2POutbound(via)
			if k2pErr != nil {
				slog.Warn("engine: k2p outbound failed", "err", k2pErr)
				continue
			}
			outbounds[target] = k2pOut
		}
	}

	// 7. Build client tunnel.
	// Gate ProcessSearcher: skip per-connection process attribution when no
	// loaded rule references it. This zeroes out the lsof exec cost for the
	// 99% of users who haven't configured an App Bypass list.
	processSearcher := cfg.ProcessSearcher
	if processSearcher != nil && !ruleEngine.HasMetaCriteria() {
		processSearcher = nil
	}
	tunnelCfg := core.ClientTunnelConfig{
		Provider:        prov,
		Outbounds:       outbounds,
		DirectTarget:    outboundDirect,
		DefaultTarget:   outboundDirect, // router is always set; fallback for tests
		Router:          router,
		ProcessSearcher: processSearcher,
		SniffBT:   !ruleEngine.DisableSniff(), // sniff BT in smart mode only
		SniffSNI:  !ruleEngine.DisableSniff(), // sniff TLS/QUIC SNI in smart mode only
		BlockQUIC: client.BlockQUIC,
	}
	tunnel := core.NewClientTunnel(tunnelCfg)

	// 8. Start.
	var proxyDNS *dns.ProxyDNSClient // non-nil in TUN mode; stored in Engine for reconnect reset
	var dh *dnsHandler // non-nil in TUN mode; stored in Engine for DNS counter reset
	if client.Mode != "proxy" {
		// TUN mode (mobile or desktop): intercept DNS at port 53.
		// If DirectDialer implements DialerController, use its control func
		// for direct DNS queries (e.g. Android socket protection).
		var dnsOpts []dns.DirectDNSOption
		if dc, ok := cfg.DirectDialer.(wire.DialerController); ok {
			dnsOpts = append(dnsOpts, dns.WithDialer(&net.Dialer{
				Control: dc.ControlFunc(),
			}))
		}
		proxyDNS = dns.NewProxyDNSClient(dns.ProxyDNSClientConfig{
			Dialer:         primaryOT.swappable, // Primary: SwappableDialer wrapping TM (QUIC stream preferred)
			FallbackDialer: tcpwsDialer,         // Fallback: TCP-WS direct (raced with 300ms stagger)
			Servers:        client.DNS.Proxy,
		})
		dnsMW := dns.NewMiddleware(dns.MiddlewareConfig{
			ProxyDNS:     proxyDNS,
			DirectDNS:    dns.NewDirectDNSClient(client.DNS.Direct, dnsOpts...),
			DNSCache:     dns.NewDNSCache(dns.DNSCacheConfig{}),
			Filter:       dns.NewFilter(dns.FilterConfig{}),
			MatchRule:    ruleEngine.Match,
			SetTmpRule:   ruleEngine.SetTmpRule,
			DNSRouter:    buildDNSRouter(outboundMap), // k2v5 outbounds use proxy DNS
			RejectTarget: outboundReject,
			EngineCtx:    ctx, // engine context — cancelled on Stop()
		})
		dh = &dnsHandler{inner: tunnel, dns: dnsMW, ctx: ctx, engine: e}
		if err := tunnel.StartWithHandler(ctx, dh); err != nil {
			if cfg.NetworkMonitor != nil {
				cfg.NetworkMonitor.Close()
			}
			for _, ot := range allOTs {
				ot.tm.Close()
			}
			return e.fail(err)
		}
	} else {
		// Proxy mode: no TUN, no DNS interception.
		if err := tunnel.Start(ctx); err != nil {
			if cfg.NetworkMonitor != nil {
				cfg.NetworkMonitor.Close()
			}
			for _, ot := range allOTs {
				ot.tm.Close()
			}
			return e.fail(err)
		}
	}

	// 9. Commit — verify not cancelled before saving resources.
	e.mu.Lock()
	if ctx.Err() != nil {
		// Stop() was called during Start() — clean up ALL started resources.
		// Order follows resource ownership: prov → tm → tunnel (same as Stop()).
		// The explicit closes below preserve that order; neutralize the
		// failure guard so it doesn't close prov a second time after them.
		cleanupProv = nil
		cleanupFD = -1
		e.mu.Unlock()
		if cfg.NetworkMonitor != nil {
			cfg.NetworkMonitor.Close()
		}
		prov.Close()
		for _, ot := range allOTs {
			ot.tm.Close()
		}
		tunnel.Close()
		return ctx.Err()
	}
	e.tunnel = tunnel
	e.prov = prov
	e.outbounds = outbounds
	e.proxyDNS = proxyDNS
	e.ruleEngine = ruleEngine
	e.missReporter = missRep
	missRep = nil // ownership transferred; cancel the deferred stop
	cleanupProv = nil // ownership transferred to e.prov (Stop/fail close it)
	cleanupFD = -1
	e.networkMonitor = cfg.NetworkMonitor

	e.outboundTMs = allOTs
	e.primaryTM = primaryOT

	// Start health monitor (1Hz wire stats sampling + health FSM).
	hm := newHealthMonitor(primaryOT, allOTs, tunnel, cfg.HealthCallback, func() { e.onHealthCritical() }, time.Now())
	hm.start()
	e.health = hm

	// Store directDialer for re-race (avoids re-parsing URL).
	e.directDialer = cfg.DirectDialer

	// Snapshot Config fields that outboundReplaceLoop needs long after Start
	// returns. WireMode is immutable after Start; OutboundProvider is
	// expected to be long-lived (daemon implements it). Nil provider is
	// fine — the loop then skips the Phase-B escalation branch and behaves
	// like today (Phase-A only).
	e.wireMode = cfg.WireMode
	e.outboundProvider = cfg.OutboundProvider

	// Re-race loop: replaces recoveryProbe.
	e.reRaceChan = make(chan *outboundTransport, 1)
	reRaceCtx, reRaceCancel := context.WithCancel(ctx)
	e.reRaceCancel = reRaceCancel
	safego.Go(func() { e.outboundReplaceLoop(reRaceCtx) })

	hm.reRaceChan = e.reRaceChan
	hm.wake.onWakeReconnect = func() {
		// Guard: skip if network is known down (e.g., airplane mode wake on mobile).
		// Mirrors onHealthCritical()'s network-up gate.
		// Note: during Stop(), netCoord may be nil — reconnect() guards via state check.
		e.mu.Lock()
		nc := e.netCoord
		e.mu.Unlock()
		if nc != nil && !nc.isNetworkUp() {
			slog.Debug("engine: wake reconnect skipped, network down")
			return
		}
		slog.Info("engine: wake reconnect")
		e.doNetworkReconnect()
	}

	e.dnsH = dh
	e.netCoord = newNetCoordinator(
		func() { e.doNetworkReconnect() },
		func() {},
	)

	// Start rule bundle updater (24h background check + reload).
	//
	// Skipped on the NetworkExtension path: the rebuild closure heap-loads the
	// full corpus (LoadNamed, ~19 MB) and ReloadNamed would swap the mmap engine
	// back to a heap one — reintroducing the jetsam pressure this whole path
	// exists to remove. NE cold-start downloads still happen above via
	// EnsureBundles; NE sessions are short, so periodic mid-session refresh is
	// unnecessary. Desktop daemon + Android keep the updater.
	if client.CacheDir != "" && !cfg.NetworkExtension {
		rebuildFn := func(cacheDir string) ([]rule.RouteEntry, []rule.NamedBundle, error) {
			return rebuildEntriesForUpdater(client.Routes, outboundMap, cacheDir)
		}
		rule.StartUpdaterNamed(ctx, ruleEngine, client.CacheDir, sources, rebuildFn)
	}

	e.setStateLocked(StateConnected)
	handler = e.handler
	status = e.buildStatusLocked()
	e.mu.Unlock()
	if handler != nil {
		handler.OnStatus(status)
	}
	slog.Info("engine: connected", "totalMs", time.Since(startTime).Milliseconds())
	slog.Info("DIAG: connected",
		"server", extractServerHost(client.Server),
		"mode", client.Mode,
		"disableSniff", ruleEngine.DisableSniff(),
		"dialMs", time.Since(startTime).Milliseconds(),
		"dnsTransport", "tcpws",
	)

	// Network monitor already started in step 4.1 (before wire probe).

	return nil
}

// reconnect resets wire transport connections.
// Shared by OnNetworkChanged (network switch) and onHealthCritical (quality degradation).
// Returns true if reconnect was performed (engine was connected and not paused).
func (e *Engine) reconnect() bool {
	e.mu.Lock()
	if e.state != StateConnected || e.paused {
		e.mu.Unlock()
		return false
	}
	handler := e.handler
	outboundTMs := e.outboundTMs
	pdns := e.proxyDNS
	dh := e.dnsH
	e.mu.Unlock()

	// Notify handler of transient reconnecting state (for UI feedback).
	if handler != nil {
		handler.OnStatus(Status{State: StateReconnecting})
	}

	// Reset order matters (shared transports — see comment in Start):
	//   1. CancelInflight — abort pending upstream queries (closes race window)
	//   2. TM resets — resets QUIC + TCP-WS sessions (DNS primary uses TM)
	//   3. pdns.ResetConnections — cleans both primary and fallback proxyConn pools
	// Step 1 MUST precede step 2 so in-flight DNS queries exit via ctx
	// before the smux session is torn down underneath them.
	if dh != nil {
		dh.CancelInflight()
	}
	for _, ot := range outboundTMs {
		ot.tm.Reset()
	}
	if pdns != nil {
		pdns.ResetConnections()
	}

	// Reset health monitor — stale data from old connection is irrelevant.
	e.mu.Lock()
	hm := e.health
	e.mu.Unlock()
	if hm != nil {
		hm.resetCounters()
	}

	return true
}

// doNetworkReconnect performs a full reconnect with DNS counter reset
// and stale error clearing. Called by netCoordinator on network recovery
// or interface change.
func (e *Engine) doNetworkReconnect() {
	slog.Info("engine: network reconnect")

	// Reset DNS failure counter — old failures are stale after network change.
	e.mu.Lock()
	dh := e.dnsH
	e.mu.Unlock()
	if dh != nil {
		dh.ResetCounter()
	}

	if !e.reconnect() {
		return
	}

	// Clear non-client wire errors — network errors are stale after network change.
	e.mu.Lock()
	handler := e.handler
	if e.lastError != nil && e.lastError.Category != CategoryClient {
		slog.Debug("engine: network change cleared stale error",
			"prevCode", e.lastError.Code,
			"prevCategory", e.lastError.Category,
		)
		e.lastError = nil
	}
	s := e.buildStatusLocked()
	e.mu.Unlock()
	if handler != nil {
		handler.OnStatus(s)
	}
}

// OnNetworkChanged should be called by the platform layer when a network
// change is detected (e.g., WiFi to cellular). Resets wire transports and
// clears stale network errors. Auth errors survive (WiFi→cellular doesn't
// fix expired subscriptions).
func (e *Engine) OnNetworkChanged() {
	e.mu.Lock()
	nc := e.netCoord
	e.mu.Unlock()
	if nc != nil {
		nc.handleEvent(&NetEvent{Signal: SignalChanged, Source: "singtun"})
		return
	}
	// Fallback when netCoord not initialized.
	e.doNetworkReconnect()
}

// OnNetEvent receives a network state change from the platform layer.
// Called from appext (gomobile bridge) with platform-specific information.
func (e *Engine) OnNetEvent(event *NetEvent) {
	e.mu.Lock()
	nc := e.netCoord
	e.mu.Unlock()
	if nc != nil {
		nc.handleEvent(event)
	}
}

// onHealthCritical is called by healthMonitor when wire quality is critically degraded.
// Unlike OnNetworkChanged, it does NOT clear wire errors (degradation ≠ network switch).
func (e *Engine) onHealthCritical() {
	// Skip reconnect if network is known to be down — avoids futile work.
	e.mu.Lock()
	nc := e.netCoord
	e.mu.Unlock()
	if nc != nil && !nc.isNetworkUp() {
		slog.Debug("engine: health critical but network is down, skipping reconnect")
		return
	}

	slog.Warn("engine: health critical, reconnecting")
	if !e.reconnect() {
		return
	}

	// Rebuild and broadcast status (reconnecting → connected).
	e.mu.Lock()
	handler := e.handler
	s := e.buildStatusLocked()
	e.mu.Unlock()
	if handler != nil {
		handler.OnStatus(s)
	}
}

// Pause releases wire transport resources for memory conservation (iOS).
// The provider and gVisor stack remain alive. Call Wake() to restore.
func (e *Engine) Pause() {
	e.mu.Lock()
	if e.state != StateConnected || e.paused {
		e.mu.Unlock()
		return
	}
	e.paused = true
	slog.Info("engine: paused")
	handler := e.handler
	outboundTMs := e.outboundTMs
	pdns := e.proxyDNS
	e.mu.Unlock()

	// Pause has no CancelInflight — no in-flight DNS during iOS memory pause.
	// TM→pdns order: shared tcpwsDialer (see Start comment).
	for _, ot := range outboundTMs {
		ot.tm.Reset()
	}
	if pdns != nil {
		pdns.ResetConnections()
	}
	if handler != nil {
		handler.OnStatus(Status{State: StatePaused})
	}
}

// Wake restores the engine after a Pause, forcing fresh transport connections.
func (e *Engine) Wake() {
	e.mu.Lock()
	if e.state != StateConnected || !e.paused {
		e.mu.Unlock()
		return
	}
	e.paused = false
	slog.Info("engine: woke")
	handler := e.handler
	outboundTMs := e.outboundTMs
	pdns := e.proxyDNS
	s := e.buildStatusLocked()
	e.mu.Unlock()

	for _, ot := range outboundTMs {
		ot.tm.Reset()
	}
	if pdns != nil {
		pdns.ResetConnections()
	}
	if handler != nil {
		handler.OnStatus(s)
	}
}

// closeOutboundTMs closes all outbound TransportManagers concurrently and
// blocks until every Close has returned. Each TM.Close internally bounds at
// ~15s (5s QUIC conn + 5s QUIC transport + 5s smux); calling them serially
// makes total wait scale with N outbounds, exceeding the daemon's outer Stop
// budget. Parallel close keeps total wait at the slowest single TM.
//
// Safety: each TM owns its own state lock; engine has already extracted the
// slice and nil'd e.outboundTMs before calling this. Callers must invoke
// outside any engine lock.
func closeOutboundTMs(outboundTMs []*outboundTransport) {
	if len(outboundTMs) == 0 {
		return
	}
	var wg sync.WaitGroup
	for _, ot := range outboundTMs {
		wg.Add(1)
		ot := ot
		safego.Go(func() {
			defer wg.Done()
			ot.tm.Close()
		})
	}
	wg.Wait()
}

// Stop tears down the tunnel and returns the engine to disconnected.
func (e *Engine) Stop() error {
	e.mu.Lock()
	if e.state == StateDisconnected {
		e.mu.Unlock()
		return nil
	}
	slog.Info("engine: stop",
		"previousState", e.state,
		"hadError", e.lastError != nil,
	)

	// Capture session metrics before teardown (fields still valid under e.mu).
	var sessionUptime time.Duration
	var sessionTx, sessionRx int64
	if !e.connectedAt.IsZero() {
		sessionUptime = time.Since(e.connectedAt)
	}
	if e.tunnel != nil {
		sessionTx, sessionRx = e.tunnel.TrafficBytes()
	}

	// Mark disconnected immediately — blocks OnNetworkChanged from starting
	// new work (it checks state != StateConnected and returns early).
	e.state = StateDisconnected

	// Extract goroutine owners whose callbacks may need e.mu.
	hm := e.health
	e.health = nil
	reRaceCancel := e.reRaceCancel
	e.reRaceCancel = nil
	nc := e.netCoord
	e.netCoord = nil
	e.dnsH = nil
	e.mu.Unlock()

	// Stop goroutines WITHOUT holding e.mu — health monitor's onReconnect
	// callback calls OnNetworkChanged() which briefly acquires e.mu.
	// Holding e.mu here would cross-deadlock with hm.wg.Wait().
	if nc != nil {
		nc.close()
	}
	if hm != nil {
		hm.stop()
	}
	if reRaceCancel != nil {
		reRaceCancel()
	}

	// Extract remaining resources under lock, nil them out, fire status,
	// then close everything OUTSIDE the lock. This prevents deadlock:
	// prov.Close() can block on dnsOverride CGo, tunnel.Close() drains
	// handlers that call ReportWireError() which needs e.mu.
	e.mu.Lock()
	nm := e.networkMonitor
	e.networkMonitor = nil
	cancel := e.cancel
	e.cancel = nil
	prov := e.prov
	e.prov = nil
	outboundTMs := e.outboundTMs
	e.outboundTMs = nil
	e.primaryTM = nil
	tunnel := e.tunnel
	e.tunnel = nil
	e.proxyDNS = nil // no explicit close needed — tm.Close() closes shared tcpws → smux dies → recvLoops exit
	re := e.ruleEngine
	e.ruleEngine = nil
	// Detach the rule-miss reporter BEFORE dropping the rule engine so
	// late matches on in-flight requests see a nil hook (rule.Engine
	// owns its own pointer, so we also clear that here — the ruleEngine
	// reference is about to be dropped anyway, but be explicit).
	missRep := e.missReporter
	e.missReporter = nil
	if re != nil && missRep != nil {
		re.SetMissReporter(nil)
	}
	outbounds := e.outbounds
	e.outbounds = nil
	e.client = nil
	e.wireMode = ""
	e.outboundProvider = nil
	e.connectedAt = time.Time{}
	e.paused = false
	e.lastError = nil // unconditional clear on stop
	e.setStateLocked(StateDisconnected)
	stopHandler := e.handler
	stopStatus := e.buildStatusLocked()
	e.mu.Unlock()
	if stopHandler != nil {
		stopHandler.OnStatus(stopStatus)
	}

	// Teardown outside lock — ordering per Resource Ownership Rules:
	// networkMonitor first (fires callbacks), cancel context, provider
	// (stops new TUN packets + DNS override), transport (drops wire),
	// tunnel last (drains in-flight handlers — they fail fast because
	// prov/tm are closed, and ReportWireError returns early since
	// e.state is already StateDisconnected).
	if nm != nil {
		nm.Close()
	}
	if cancel != nil {
		cancel()
	}
	if prov != nil {
		prov.Close()
	}
	closeOutboundTMs(outboundTMs)
	// Close all outbounds. directOutbound and wireOutbound have no-op Close()
	// (engine owns tm, closed via outboundTMs). k2pOutbound closes its ICE/QUIC connection.
	for _, ob := range outbounds {
		if ob != nil {
			ob.Close()
		}
	}
	if tunnel != nil {
		tunnel.Close()
	}
	if re != nil {
		re.Close()
	}
	// Stop the rule-miss reporter last — its drain goroutine may still
	// flush a final batch. Stop() blocks up to 5s.
	if missRep != nil {
		missRep.Stop()
	}
	if sessionUptime > 0 {
		slog.Info("DIAG: session-end",
			"uptimeS", int(sessionUptime.Seconds()),
			"txMB", fmt.Sprintf("%.1f", float64(sessionTx)/(1024*1024)),
			"rxMB", fmt.Sprintf("%.1f", float64(sessionRx)/(1024*1024)),
		)
	}
	return nil
}

// StatsHistory returns the health monitor's ring buffer (oldest-first).
func (e *Engine) StatsHistory() []HealthSnapshot {
	e.mu.Lock()
	hm := e.health
	e.mu.Unlock()
	if hm == nil {
		return nil
	}
	return hm.History()
}

// GetStatus returns the current engine state as a typed snapshot.
func (e *Engine) GetStatus() Status {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.buildStatusLocked()
}

// buildStatusLocked builds a Status from current fields. Caller must hold e.mu.
//
// e.state itself stays StateConnected while paused (Pause()/Wake() only flip
// e.paused — see the StatePaused comment in event.go). Any status query
// (native getStatusJSON, webapp initial fetch, 15s safety-net poll) must see
// "paused" here too, otherwise the one-shot OnStatus(StatePaused) push gets
// immediately overwritten by the next poll reporting "connected".
func (e *Engine) buildStatusLocked() Status {
	state := e.state
	if e.paused {
		state = StatePaused
	}
	return Status{
		State:       state,
		Error:       e.lastError,
		ConnectedAt: e.connectedAt,
	}
}

// StatusJSON returns the engine status as a JSON string.
// The format uses snake_case keys for consistency with the daemon API.
func (e *Engine) StatusJSON() string {
	b, _ := json.Marshal(e.GetStatus())
	return string(b)
}

// setStateLocked updates state fields only. Must be called with e.mu held.
// Caller is responsible for notifying handler OUTSIDE the lock.
func (e *Engine) setStateLocked(s string) {
	if s == StateConnected {
		e.connectedAt = time.Now()
	}
	e.state = s
}

// fail records an error, cancels any in-flight context, resets to
// disconnected, and returns the error. The OnStatus event carries both
// the disconnected state and the classified error atomically.
// prefetchRulesForStart warms the rule cache before the engine builds routes.
// On the Network Extension the networked EnsureBundles is skipped entirely —
// the NE runs in a ~50MB jetsam process and must never download. It seeds the
// embedded floor (so every region is present and missingConfiguredRegions
// won't false-504) and relies on the main app's PrefetchRules for CDN refresh
// into the shared App-Group cache. Returns a non-nil *EngineError only on a
// genuine cold-start (no bundles at all on the non-NE path); the caller fails
// the Start with it.
func (e *Engine) prefetchRulesForStart(ctx context.Context, client *config.ClientConfig, sources []string, networkExtension bool) *EngineError {
	if client.CacheDir == "" {
		// Defensive: platforms are expected to provide CacheDir. Empty means
		// the gate is a no-op, re-exposing the routes=0 cold-start bug if
		// bundles aren't already on disk. Log loudly so a future regression
		// in platform wiring surfaces at first connect.
		slog.Warn("engine: Start without CacheDir — rule bundle prefetch skipped",
			"risk", "routes=0 cold-start bug may reappear if bundles absent")
		return nil
	}
	// Always seed the offline floor — instant, no network. Censorship-proof and
	// satisfies the missingConfiguredRegions 504 guard even on the NE path.
	if err := rule.SeedFromEmbedIfEmpty(client.CacheDir); err != nil {
		slog.Warn("engine: seed from embed failed", "err", err, "cacheDir", client.CacheDir)
	}
	if networkExtension {
		// The NE must never run networked EnsureBundles — OOM risk in the
		// jetsam process. The main app's PrefetchRules owns CDN refresh into
		// the shared App-Group cache; the NE only reads it.
		slog.Info("engine: NE path — skipping networked rule refresh (main app owns it)",
			"cacheDir", client.CacheDir)
		return nil
	}
	ensureCtx, cancelEnsure := context.WithTimeout(ctx, 10*time.Second)
	defer cancelEnsure()
	if ensureErr := rule.EnsureBundles(ensureCtx, client.CacheDir, sources); ensureErr != nil {
		existing := rule.ListRegions(client.CacheDir) // count-only; no heap expand
		if len(existing) == 0 {
			// True cold start: no disk cache to fall back to.
			return &EngineError{
				Code:     ErrCodeRuleBundlesUnavailable,
				Category: CategoryServer,
				Message:  fmt.Sprintf("rule bundles unavailable: %v", ensureErr),
			}
		}
		slog.Warn("DIAG: rule-bundle-stale",
			"err", ensureErr.Error(),
			"bundleCount", len(existing),
			"cacheDir", client.CacheDir,
		)
	}
	return nil
}

func (e *Engine) fail(err error) error {
	e.mu.Lock()
	e.lastError = ClassifyError(err)
	slog.Warn("engine: fail",
		"code", e.lastError.Code,
		"category", e.lastError.Category,
		"message", e.lastError.Message,
	)

	// Extract goroutine owners whose callbacks may need e.mu.
	// Must mirror Stop() — without this, healthMonitor/outboundReplaceLoop
	// keep running with stale references, the heartbeat keeps logging
	// transport="" against a closed TM, and wake/network reconnect fires
	// no-ops because reconnect() guards on state == StateConnected.
	hm := e.health
	e.health = nil
	reRaceCancel := e.reRaceCancel
	e.reRaceCancel = nil
	nc := e.netCoord
	e.netCoord = nil
	e.dnsH = nil

	// Extract resources under lock (Rule #4: no Close under state lock).
	nm := e.networkMonitor
	e.networkMonitor = nil
	outboundTMs := e.outboundTMs
	e.outboundTMs = nil
	e.primaryTM = nil
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
	prov := e.prov
	e.prov = nil
	tunnel := e.tunnel
	e.tunnel = nil
	e.proxyDNS = nil // tm.Close() closes shared tcpws → smux dies → recvLoops exit
	re := e.ruleEngine
	e.ruleEngine = nil
	missRep := e.missReporter
	e.missReporter = nil
	if re != nil && missRep != nil {
		re.SetMissReporter(nil)
	}
	outbounds := e.outbounds
	e.outbounds = nil

	e.setStateLocked(StateDisconnected)
	handler := e.handler
	status := e.buildStatusLocked()
	e.mu.Unlock()

	// Stop goroutines OUTSIDE lock — health monitor's onReconnect callback
	// calls OnNetworkChanged() which briefly acquires e.mu; holding e.mu
	// here would cross-deadlock with hm.wg.Wait() (mirrors Stop()).
	if nc != nil {
		nc.close()
	}
	if hm != nil {
		hm.stop()
	}
	if reRaceCancel != nil {
		reRaceCancel()
	}

	// External calls outside lock — ordering per Resource Ownership Rules
	// (mirrors Stop()): networkMonitor → provider → transport → outbounds
	// → tunnel → ruleEngine → missReporter.
	if handler != nil {
		handler.OnStatus(status)
	}
	if nm != nil {
		nm.Close()
	}
	if prov != nil {
		prov.Close()
	}
	closeOutboundTMs(outboundTMs)
	for _, ob := range outbounds {
		if ob != nil {
			ob.Close()
		}
	}
	if tunnel != nil {
		tunnel.Close()
	}
	if re != nil {
		re.Close()
	}
	if missRep != nil {
		missRep.Stop()
	}
	return err
}

// anchorDirectEntry is the built-in route entry that always sends k2r's
// discovery/control anchor address (config.RouterAnchorIP) direct — never
// user-configurable, never tunneled. See config/anchor.go for the shared
// constant and gateway/anchor.go for the matching gateway-side PREROUTING
// DNAT rule (2026-07-18 spec revision, anchor-only discovery for the k2r
// router-control feature). Built via krs.BuildSet the same way inline
// IPCIDR routes are (engine.go buildRouteEntries), so it participates in
// rule.Engine's normal IP-match path (matchRouteIP / r.Sets).
func anchorDirectEntry() rule.RouteEntry {
	anchorSet, err := krs.BuildSet(krs.NamedSet{CIDRs: []string{config.RouterAnchorIP + "/32"}})
	if err != nil {
		// Unreachable in practice — config.RouterAnchorIP is a hardcoded
		// valid literal — but fail safe (inert entry) rather than panic.
		slog.Error("engine: anchor route build failed — anchor direct rule NOT installed", "err", err)
		return rule.RouteEntry{}
	}
	return rule.RouteEntry{Target: outboundDirect, Sets: []krs.Matcher{anchorSet}}
}

// withAnchorFirst prepends the built-in anchor entry ahead of every other
// route entry. rule.Engine.Match/MatchConn are first-match-wins over
// entries, then fall through to `fallback` — prepending here is what makes
// the anchor address win over a match:all catch-all in Global proxy mode
// (fallback would otherwise be the proxy outbound).
//
// MUST be applied at every site that either constructs a rule.Engine OR
// feeds a []rule.RouteEntry into a live engine's Reload/ReloadNamed/
// ReloadWithClosers — a reload wholesale-replaces e.routes (rule/engine.go
// reload), silently dropping any entry not present in what's passed in.
// Audited call sites (2026-07-18 review; grep `buildRouteEntries(` /
// `rule.NewEngine` / `\.Reload` across the repo to re-verify if you add a
// new one):
//  1. buildRuleEngine's match:all single-route fast path — rule.NewEngine.
//  2. buildRuleEngine's heap path — rule.NewEngineNamed.
//  3. buildRuleEngineDisk (NE mmap path) — rule.NewEngineWithClosers.
//  4. rebuildEntriesForUpdater, wired into rule.StartUpdaterNamed in
//     Start() — the 24h background rule-bundle updater's rebuild function,
//     whose output feeds Engine.ReloadNamed on every tick (including the
//     immediate first tick, seconds after connect). Missing this site was
//     a shipped Critical bug: the anchor entry present right after
//     construction was stripped by the first updater reload on desktop
//     daemon + Android (client.CacheDir != "" && !NetworkExtension) —
//     anchor routing worked at connect, then broke minutes later. See
//     TestAnchorRoute_SurvivesUpdaterReload.
func withAnchorFirst(entries []rule.RouteEntry) []rule.RouteEntry {
	return append([]rule.RouteEntry{anchorDirectEntry()}, entries...)
}

// rebuildEntriesForUpdater reproduces buildRuleEngine's heap-path route
// construction (load bundles from cacheDir, index, buildRouteEntries) for
// the 24h background rule-bundle updater. Extracted as a standalone,
// directly testable function — rather than left inline as a closure in
// Start() — specifically so a test can drive the exact code path
// rule.StartUpdaterNamed calls on every tick and confirm withAnchorFirst
// survives it (see TestAnchorRoute_SurvivesUpdaterReload). Matches
// rule.RebuildFuncNamed's signature modulo the extra (routes, outboundMap)
// closure-over params, which Start()'s rebuildFn supplies from client.Routes
// and the already-built outboundMap.
func rebuildEntriesForUpdater(routes []config.RouteConfig, outboundMap map[string]rule.Target, cacheDir string) ([]rule.RouteEntry, []rule.NamedBundle, error) {
	named, err := rule.LoadNamed(cacheDir)
	if err != nil {
		return nil, nil, err
	}
	bundles := make([]*krs.Bundle, 0, len(named))
	regionBundlesRebuild := make(map[string]*krs.Bundle, len(named))
	for _, nb := range named {
		bundles = append(bundles, nb.Bundle)
		if nb.Region != "" {
			regionBundlesRebuild[nb.Region] = nb.Bundle
		}
	}
	idx := matcherIndex(krs.Index(bundles))

	entries, _, _ := buildRouteEntries(routes, idx, outboundMap, heapRegionExpander(regionBundlesRebuild))
	return withAnchorFirst(entries), named, nil
}

// buildRouteEntries converts config routes to rule.RouteEntry list using the given bundle index.
// outboundMap maps via URL → rule.Target for wire outbounds.
// Shared by buildRuleEngine (startup) and updater rebuild (hot-reload) — single code path
// ensures both always produce identical routing tables from the same config+bundles.
func buildRouteEntries(routes []config.RouteConfig, idx map[string]krs.Matcher, outboundMap map[string]rule.Target, expandRegion func(region string, target rule.Target) []rule.RouteEntry) (entries []rule.RouteEntry, fallback rule.Target, disableSniff bool) {
	fallback = outboundDirect

	for i, route := range routes {
		var target rule.Target
		switch {
		case route.Via == "" || route.Via == "direct":
			target = outboundDirect
		case route.Via == "reject":
			target = outboundReject
		default:
			if t, ok := outboundMap[route.Via]; ok {
				target = t
			} else {
				target = outboundDirect // unknown via → direct
			}
		}

		if route.Match.All {
			// Bulk traffic proxies → disable sniffing. This intentionally stays
			// true even if exception routes were accumulated above (those are in
			// `entries`); the sniff decision is about the catch-all, not whether
			// exceptions exist. DNS-pin learning keys off route presence, not this.
			disableSniff = (target != outboundDirect && target != outboundReject)
			fallback = target
			break
		}

		entry := rule.RouteEntry{Target: target}

		// Bundle-based sets.
		names := rule.ExpandMatch(route.Match)
		for _, name := range names {
			if bs, ok := idx[name]; ok {
				entry.Sets = append(entry.Sets, bs)
			}
		}

		// Inline rules — compiled into a krs.Matcher via BuildSet and folded
		// into entry.Sets. Skip when the build yields nothing (all-invalid
		// input), so an empty matcher never grants spurious host criteria.
		if len(route.Match.DomainSuffix) > 0 || len(route.Match.IPCIDR) > 0 {
			ns, err := krs.BuildSet(krs.NamedSet{
				DomainSuffixes: route.Match.DomainSuffix,
				CIDRs:          route.Match.IPCIDR,
			})
			if err != nil {
				slog.Warn("rule: skip inline set (build failed)", "err", err)
			} else if !ns.IsEmpty() {
				entry.Sets = append(entry.Sets, ns)
			}
		}

		// Meta criteria.
		entry.ProcessNames = route.Match.ProcessName
		entry.PackageNames = route.Match.PackageName
		entry.NetworkMatch = route.Match.Network
		entry.IPIsPrivate = route.Match.IPIsPrivate

		// v2: prefix + installer.
		entry.ProcessNamePrefixes = lowerCopy(route.Match.ProcessNamePrefix)
		entry.PackageNamePrefixes = append([]string(nil), route.Match.PackageNamePrefix...)
		entry.InstallerPackages = append([]string(nil), route.Match.InstallerPackage...)

		// Plan B: expand match.region into one host entry (Sets) + one meta
		// entry (process/package criteria from bundle.Apps[GOOS]). The two
		// entries are appended directly to `entries` — the per-route `entry`
		// below is legacy (preset/names/inline/meta) and only appended if it
		// has its own criteria. region/apps and legacy fields can coexist on
		// one route, though webapp emits them on separate routes.
		if route.Match.Region != "" {
			regionEntries := expandRegion(route.Match.Region, target)
			if len(regionEntries) == 0 {
				slog.Warn("engine: match.region bundle not loaded — skipping route",
					"region", route.Match.Region, "via", config.TruncateVia(route.Via))
			}
			entries = append(entries, regionEntries...)
		}
		if len(route.Match.Apps) > 0 {
			if meta := expandAppsRoute(route.Match.Apps, target); meta != nil {
				entries = append(entries, *meta)
			}
		}

		hasAnyCriteria := len(entry.Sets) > 0 ||
			len(entry.ProcessNames) > 0 || len(entry.PackageNames) > 0 ||
			len(entry.ProcessNamePrefixes) > 0 || len(entry.PackageNamePrefixes) > 0 ||
			len(entry.InstallerPackages) > 0 ||
			entry.NetworkMatch != "" || entry.IPIsPrivate
		if hasAnyCriteria {
			entries = append(entries, entry)
		}

		// Last route without match criteria = fallback.
		if i == len(routes)-1 && !route.Match.HasCriteria() {
			fallback = target
		}
	}
	return entries, fallback, disableSniff
}

// buildOutboundMap assigns Target indices to each unique outbound URL in routes.
// Index 0 and 1 are reserved (direct/reject). Wire outbound indices start at 2.
//
// Reserved schemes (device://, auto://) are skipped — ValidateClient rejects them
// before assembly, so this branch is a defensive guard in case validation is bypassed.
// Unknown/reserved via strings silently fall through to direct in buildRouteEntries.
func buildOutboundMap(routes []config.RouteConfig) map[string]rule.Target {
	m := map[string]rule.Target{}
	nextIdx := rule.Target(2) // 0=direct, 1=reject, 2+=wire outbounds
	for _, route := range routes {
		via := route.Via
		if via == "" || via == "direct" || via == "reject" {
			continue
		}
		if config.IsReservedVia(via) {
			slog.Warn("engine: route via uses reserved scheme, treating as direct",
				"via", config.TruncateVia(via), "scheme", config.ViaScheme(via))
			continue
		}
		if _, exists := m[via]; exists {
			continue
		}
		m[via] = nextIdx
		nextIdx++
	}
	return m
}

// setupOutboundTransportFn is the function used by the outbound-replacement
// path to build a fresh TransportManager for a new k2v5:// URL. It is set
// to setupOutboundTransport in production; tests override it to drive the
// replacement loop deterministically. The Start-time outbound setup calls
// setupOutboundTransport directly (not this var) — only Phase-B replacement
// is test-injectable.
var setupOutboundTransportFn = setupOutboundTransport

// setupOutboundTransport creates a TransportManager for a single k2v5:// URL.
// Performs: ParseURL → resolveDialAddr → RaceTransport → TM setup.
// The caller must close the returned outboundTransport on error paths.
// Returns the outboundTransport and the TCP-WS dialer (for DNS transport from primary outbound).
func setupOutboundTransport(
	ctx context.Context,
	via string,
	directDialer wire.DirectDialer,
	wireMode string,
) (*outboundTransport, wire.Dialer, error) {
	wireCfg, err := wire.ParseURL(via)
	if err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", extractServerHost(via), err)
	}
	wireCfg.Mode = wireMode // binary identity (e.g., "gateway" for k2r)

	dialAddr, err := resolveDialAddr(ctx, wireCfg, directDialer)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve %s: %w", wireCfg.Host, err)
	}
	if dialAddr != "" {
		wireCfg.DialAddr = dialAddr
	}

	raceCtx, raceCancel := context.WithTimeout(ctx, 15*time.Second)
	raceResult, err := wire.RaceTransport(raceCtx, wireCfg, wireCfg.DialAddr, directDialer)
	raceCancel()
	if err != nil {
		return nil, nil, fmt.Errorf("transport race %s: %w", wireCfg.Host, err)
	}

	tm := wire.NewTransportManager()

	// When tcpws won the race but QUIC also connected, install QUIC as
	// primary with fallback=true. The background probe (30s) will verify
	// QUIC and switch to it — typically faster for sustained throughput.
	if raceResult.QUICBackup != nil {
		tm.SetPrimary(raceResult.QUICBackup, raceResult.QUICBackupType)
		tm.SetTCPWS(raceResult.Dialer) // tcpws as active transport
		tm.SetFallbackActiveWithReason(true, "quic-race-lost")
		slog.Info("engine: QUIC installed as primary (race loser), tcpws serving traffic",
			"quicType", raceResult.QUICBackupType,
		)
	} else {
		tm.SetPrimary(raceResult.Dialer, raceResult.TransportType)
	}

	// Ensure TCP-WS is available as fallback and DNS transport.
	var tcpwsDialer wire.Dialer
	if raceResult.QUICBackup != nil {
		// Already set above — tcpws is raceResult.Dialer.
		tcpwsDialer = raceResult.Dialer
	} else if raceResult.TCPWS != nil {
		tm.SetTCPWS(raceResult.TCPWS)
		tcpwsDialer = raceResult.TCPWS
	} else if raceResult.TransportType != "tcpws" {
		tcpwsClient := wire.NewTCPWSClient(wireCfg, nil)
		if directDialer != nil {
			tcpwsClient.SetDirectDialer(directDialer)
		}
		tm.SetTCPWS(tcpwsClient)
		tcpwsDialer = tcpwsClient
	} else {
		tcpwsDialer = raceResult.Dialer
	}

	tm.StartQuicProbe(ctx)

	ot := &outboundTransport{
		tm:       tm,
		wireCfg:  wireCfg,
		dialAddr: dialAddr,
		name:     fmt.Sprintf("k2v5-%s", wireCfg.Host),
		url:      via,
	}
	// Install the indirection wrapper that core.WireOutbound and
	// ProxyDNSClient will dial through. See outboundTransport.swappable doc.
	ot.swappable = wire.NewSwappableDialer(tm)

	slog.Info("engine: transport ready",
		"outbound", ot.name,
		"transport", raceResult.TransportType,
		"dialAddr", wireCfg.DialAddr,
	)

	return ot, tcpwsDialer, nil
}

// buildDNSRouter returns a function that maps outbound Target → proxy DNS (true) or direct DNS (false).
// Only k2v5 outbounds use proxy DNS (to avoid GFW poisoning). k2p and direct use direct DNS.
func buildDNSRouter(outboundMap map[string]rule.Target) func(rule.Target) bool {
	proxyDNSTargets := map[rule.Target]bool{}
	for via, target := range outboundMap {
		if strings.HasPrefix(via, "k2v5://") {
			proxyDNSTargets[target] = true
		}
	}
	return func(t rule.Target) bool {
		return proxyDNSTargets[t]
	}
}

// fallbackRegion is the region bundle substituted when a configured match.region
// route has no bundle of its own on disk. cn.krs is always embedded in the binary
// (rule/embed), so it is the one region guaranteed loadable even before any CDN
// refresh succeeds. The region expanders (heapRegionExpander / diskRegionExpander)
// serve cn's data for the missing region, and missingConfiguredRegions treats the
// region as satisfied — so an abroad user connects on cn rules until their own
// bundle downloads. The original all-proxy disaster guard survives: if cn itself
// is absent (corrupt embed), the missing region is still flagged and Start 504s.
const fallbackRegion = "cn"

// missingConfiguredRegions returns the names of match.region routes whose bundle
// is not usable in cacheDir — either absent or present-but-unparseable (LoadNamed
// skips bundles that fail to parse). A non-empty result for a split-tunnel config
// means the direct route would silently degrade to all-proxy, so Start treats it
// as a retryable rule-bundle failure rather than connecting with broken routing.
func missingConfiguredRegions(routes []config.RouteConfig, cacheDir string) []string {
	var want []string
	for _, r := range routes {
		if r.Match.Region != "" {
			want = append(want, r.Match.Region)
		}
	}
	if len(want) == 0 {
		return nil
	}

	have := make(map[string]bool)
	for _, region := range rule.ListRegions(cacheDir) { // glob basenames; no heap expand
		have[region] = true
	}

	var missing []string
	for _, region := range want {
		// Satisfied if the region's own bundle is present, or — for a non-cn
		// region — the cn fallback bundle is present. A missing cn route (region
		// == fallbackRegion with cn absent) still flags, preserving the guard.
		if !have[region] && !have[fallbackRegion] {
			missing = append(missing, region)
		}
	}
	return missing
}

// buildRuleEngine creates a rule.Engine from the client's route configuration.
// CacheDir must be set by the platform shell (daemon/appext) before calling engine.
func buildRuleEngine(
	client *config.ClientConfig,
	outboundMap map[string]rule.Target,
	useDiskReader bool,
) *rule.Engine {
	finalRoutes := client.Routes

	// Simple case: single route with match:all and wire outbound → all proxy, no routes.
	if client.IsAllProxy() && len(finalRoutes) == 1 {
		target := outboundMap[finalRoutes[0].Via]
		return rule.NewEngine(withAnchorFirst(nil), target, nil, true)
	}

	if useDiskReader {
		return buildRuleEngineDisk(client, finalRoutes, outboundMap)
	}

	// Heap path (desktop daemon + Android): full-expand rule bundles into heap.
	named, err := rule.LoadNamed(client.CacheDir)
	if err != nil {
		slog.Warn("engine: rule load failed", "err", err)
	}
	bundles := make([]*krs.Bundle, 0, len(named))
	regionBundles := make(map[string]*krs.Bundle, len(named))
	for _, nb := range named {
		bundles = append(bundles, nb.Bundle)
		if nb.Region != "" {
			regionBundles[nb.Region] = nb.Bundle
		}
	}
	idx := matcherIndex(krs.Index(bundles))
	entries, fallback, disableSniff := buildRouteEntries(finalRoutes, idx, outboundMap, heapRegionExpander(regionBundles))
	entries = withAnchorFirst(entries)

	logRuleEngineState(client.CacheDir, len(bundles) > 0, entries, fallback)
	return rule.NewEngineNamed(entries, fallback, named, disableSniff)
}

// buildRuleEngineDisk is the NetworkExtension (iOS NE) variant of
// buildRuleEngine. It mmaps the rule bundles via rule.OpenNamed — keeping
// resident dirty heap O(set count) (~tens of KB vs ~19 MB) so the NE survives
// the 50 MB jetsam ceiling — and hands the matchers + unmap closers to the
// engine. Region routes resolve host-only (expandRegionRouteDisk); region-Apps
// meta is omitted because iOS cannot attribute processes anyway.
func buildRuleEngineDisk(client *config.ClientConfig, finalRoutes []config.RouteConfig, outboundMap map[string]rule.Target) *rule.Engine {
	named, closers, err := rule.OpenNamed(client.CacheDir)
	if err != nil {
		slog.Warn("engine: rule mmap open failed", "err", err)
	}
	idx := rule.IndexDisk(named)
	regionDisk := make(map[string]*krs.DiskBundle, len(named))
	for _, nb := range named {
		if nb.Region != "" {
			regionDisk[nb.Region] = nb.Bundle
		}
	}
	entries, fallback, disableSniff := buildRouteEntries(finalRoutes, idx, outboundMap, diskRegionExpander(regionDisk))
	entries = withAnchorFirst(entries)

	logRuleEngineState(client.CacheDir, len(named) > 0, entries, fallback)
	return rule.NewEngineWithClosers(entries, fallback, closers, disableSniff)
}

// logRuleEngineState emits the "rule engine loaded" / "fallback only" line,
// shared by the heap and disk builders so both report identically.
func logRuleEngineState(cacheDir string, hasBundles bool, entries []rule.RouteEntry, fallback rule.Target) {
	hasRules := hasBundles
	for _, e := range entries {
		if len(e.Sets) > 0 {
			hasRules = true
			break
		}
		if len(e.ProcessNames) > 0 || len(e.PackageNames) > 0 ||
			len(e.ProcessNamePrefixes) > 0 || len(e.PackageNamePrefixes) > 0 ||
			len(e.InstallerPackages) > 0 {
			hasRules = true
			break
		}
	}
	if !hasRules {
		slog.Info("engine: no rule bundles or inline rules, using fallback only",
			"cacheDir", cacheDir, "fallback", fallback)
	} else {
		slog.Info("engine: rule engine loaded", "routes", len(entries), "fallback", fallback)
	}
}

// matcherIndex widens a name→*NamedSet index (heap) to name→krs.Matcher so the
// shared buildRouteEntries is agnostic to heap vs mmap sets.
func matcherIndex(ns map[string]*krs.NamedSet) map[string]krs.Matcher {
	m := make(map[string]krs.Matcher, len(ns))
	for k, v := range ns {
		m[k] = v
	}
	return m
}

// heapRegionExpander builds the region-expansion closure for the heap path:
// host (Sets) + meta (Apps[GOOS]) entries from a *krs.Bundle.
func heapRegionExpander(regionBundles map[string]*krs.Bundle) func(string, rule.Target) []rule.RouteEntry {
	return func(region string, target rule.Target) []rule.RouteEntry {
		b, ok := regionBundles[region]
		if !ok || b == nil {
			// cn fallback: serve the always-embedded cn bundle for a region whose
			// own bundle hasn't loaded yet (see fallbackRegion). rebuildFn re-runs
			// this closure after the updater fetches the real bundle, hot-swapping
			// to correct data without a reconnect.
			if region != fallbackRegion {
				b, ok = regionBundles[fallbackRegion]
			}
			if !ok || b == nil {
				return nil
			}
		}
		host, meta := expandRegionRoute(b, target)
		var out []rule.RouteEntry
		if host != nil {
			out = append(out, *host)
		}
		if meta != nil {
			out = append(out, *meta)
		}
		return out
	}
}

// diskRegionExpander builds the region-expansion closure for the NE/mmap path:
// host-only entry from a *krs.DiskBundle (no Apps meta — see buildRuleEngineDisk).
func diskRegionExpander(regionDisk map[string]*krs.DiskBundle) func(string, rule.Target) []rule.RouteEntry {
	return func(region string, target rule.Target) []rule.RouteEntry {
		db, ok := regionDisk[region]
		if !ok || db == nil {
			// cn fallback (see fallbackRegion) — NE/mmap path. Unlike the heap path,
			// iOS NE skips the background updater (engine.go), so this does NOT
			// hot-swap mid-session: the fallback holds until the next cold start
			// re-opens the cache after EnsureBundles fetches the real bundle.
			if region != fallbackRegion {
				db, ok = regionDisk[fallbackRegion]
			}
			if !ok || db == nil {
				return nil
			}
		}
		if host := expandRegionRouteDisk(db, target); host != nil {
			return []rule.RouteEntry{*host}
		}
		return nil
	}
}

// computeDNSExclude builds the list of IPs to exclude from TUN routing.
// This includes DNS direct server IPs and the wire server's resolved IPs.
func computeDNSExclude(client *config.ClientConfig) []netip.Addr {
	var addrs []netip.Addr

	// Parse DNS server IPs from direct servers.
	for _, s := range client.DNS.Direct {
		host, _, err := net.SplitHostPort(s)
		if err != nil {
			continue
		}
		if addr, err := netip.ParseAddr(host); err == nil {
			addrs = append(addrs, addr)
		}
	}

	// Extract IPs directly from wire URL ip=/ip6= parameters first.
	// This is reliable even before DNS is working (which matters since TUN
	// hasn't started routing yet when this runs).
	if u, err := url.Parse(client.Server); err == nil {
		q := u.Query()
		for _, key := range []string{"ip", "ip6"} {
			if raw := q.Get(key); raw != "" {
				if addr, err := netip.ParseAddr(raw); err == nil {
					addrs = append(addrs, addr)
				}
			}
		}
	}

	// Also try resolving server hostname (may fail if DNS not ready).
	if serverHost := extractServerHost(client.Server); serverHost != "" {
		if ips, err := net.LookupHost(serverHost); err == nil {
			for _, ip := range ips {
				if addr, err := netip.ParseAddr(ip); err == nil {
					addrs = append(addrs, addr)
				}
			}
		}
	}

	return addrs
}

// extractServerHost extracts the hostname from a k2v5:// URL.
func extractServerHost(wireURL string) string {
	u, err := url.Parse(wireURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// ReportWireError reports a runtime wire transport error.
// Called by tunnel's handleTCPProxy/handleUDPProxy on wire transport failure,
// and by recovery probe on probe failure. Proxy-layer errors (server responded
// to the request) are filtered at the call site — see tunnel.go.
// Debounces: first error fires OnStatus, subsequent non-auth errors are no-ops.
// Auth errors (client category) can upgrade existing network errors.
func (e *Engine) ReportWireError(err error) {
	newErr := ClassifyError(err)
	e.mu.Lock()
	if e.state != StateConnected {
		e.mu.Unlock()
		return
	}
	if e.lastError != nil {
		if e.lastError.Category == CategoryClient {
			e.mu.Unlock()
			return // don't overwrite auth error with anything
		}
		if newErr.Category != CategoryClient {
			e.mu.Unlock()
			return // debounce: non-auth error already reported
		}
		// auth error upgrades existing network error — fall through
	}
	e.lastError = newErr
	slog.Warn("engine: wire error",
		"code", newErr.Code,
		"category", newErr.Category,
		"message", newErr.Message,
	)
	slog.Warn("DIAG: wire-error",
		"code", newErr.Code,
		"category", newErr.Category,
		"message", newErr.Message,
	)
	handler := e.handler
	status := e.buildStatusLocked()
	e.mu.Unlock()
	if handler != nil {
		handler.OnStatus(status)
	}
}

// ClearWireError clears a runtime wire error after a successful proxy dial.
// Does NOT clear client-category errors (401/402/403) — those require user action.
func (e *Engine) ClearWireError() {
	e.mu.Lock()
	if e.lastError == nil || e.lastError.Category == CategoryClient {
		e.mu.Unlock()
		return
	}
	e.lastError = nil
	slog.Info("engine: wire error cleared")
	handler := e.handler
	status := e.buildStatusLocked()
	e.mu.Unlock()
	if handler != nil {
		handler.OnStatus(status)
	}
}

// outboundReplaceLoop waits for wire-health signals (currently: echoLoop's
// 3-consecutive-fail) and responds in two escalating phases:
//
//   Phase A — same-server wire re-race. Runs wire.RaceTransport against the
//     outbound's current URL. On success, hot-swaps the winning Dialer into
//     the existing TransportManager via ReplacePrimary/SwapTCPWS. Same
//     behavior as the pre-Step-3 reRaceLoop.
//
//   Phase B — server replacement. After reRaceFailThresholdForReplace
//     consecutive Phase-A failures, asks OutboundProvider for a DIFFERENT
//     k2v5:// URL, builds a fresh TransportManager in the background, and
//     atomically swaps it into target.swappable. The old TM is Closed
//     immediately — in-flight smux / QUIC streams receive ConnectionClosed
//     within ms and application layers retry. TUN / provider / DNS hijack
//     stay alive throughout (zero DNS-leakage window).
//
// If Phase B's provider is nil (appext / gateway) or returns ErrExhausted
// (all subscription tunnels already tried and failed), the loop fires
// handler.OnOutboundFatal and calls e.fail to terminate the tunnel.
//
// Known TOCTOU (P2, not fixed): If Stop() fires between the ctx.Err() check
// and ReplacePrimary, and tm.Close() wins the tm.mu race, the new Dialer is
// orphaned in a defunct TransportManager. Bounded leak: QUIC idle timeout (30s)
// reclaims it. Fix: add a WaitGroup so Stop() waits for this loop to exit
// before calling tm.Close(). Carry-over from reRaceLoop.
func (e *Engine) outboundReplaceLoop(ctx context.Context) {
	for {
		var target *outboundTransport
		select {
		case <-ctx.Done():
			return
		case target = <-e.reRaceChan:
		}

		e.mu.Lock()
		if e.state != StateConnected {
			e.mu.Unlock()
			continue
		}
		dd := e.directDialer
		provider := e.outboundProvider
		e.mu.Unlock()

		if target == nil || target.tm == nil {
			continue
		}

		// --- Phase A: same-server re-race --------------------------------
		slog.Warn("DIAG: transport-rerace",
			"reason", "echo-consecutive-fail",
			"outbound", target.name,
			"previousTransport", target.tm.PrimaryType(),
		)

		raceCtx, raceCancel := context.WithTimeout(ctx, 15*time.Second)
		result, raceErr := wire.RaceTransport(raceCtx, target.wireCfg, target.wireCfg.DialAddr, dd)
		raceCancel()

		if raceErr == nil {
			if ctx.Err() != nil {
				result.Dialer.Close()
				if result.QUICBackup != nil {
					result.QUICBackup.Close()
				}
				return
			}
			if !e.applyReRaceResult(target, result) {
				// target no longer live — dispose of new Dialer and move on
				result.Dialer.Close()
				if result.QUICBackup != nil {
					result.QUICBackup.Close()
				}
			}
			e.mu.Lock()
			target.reRaceFailStreak = 0
			e.mu.Unlock()
			continue
		}

		// Phase A failed.
		slog.Warn("engine: re-race failed", "outbound", target.name, "err", raceErr)
		e.mu.Lock()
		target.reRaceFailStreak++
		streak := target.reRaceFailStreak
		e.mu.Unlock()

		if streak < reRaceFailThresholdForReplace {
			// Wait for next echo signal; give same server another chance.
			continue
		}
		if provider == nil {
			// No provider = no escalation path. Leave streak high; if the
			// situation ever self-heals (e.g. upstream comes back up), a
			// future Phase-A success will clear it. Matches pre-Step-3
			// behavior (same-server retries forever).
			continue
		}

		// --- Phase B: ask provider for a replacement URL ----------------
		if err := e.replaceOutboundServer(ctx, target, raceErr, dd, provider); err != nil {
			// Exhausted or ctx cancelled. If it was ctx, just exit quietly.
			if ctx.Err() != nil {
				return
			}
			e.mu.Lock()
			handler := e.handler
			outboundName := target.name
			e.mu.Unlock()
			slog.Error("engine: outbound replacement exhausted",
				"outbound", outboundName, "err", err)
			if handler != nil {
				handler.OnOutboundFatal(outboundName, err)
			}
			_ = e.fail(fmt.Errorf("outbound %s exhausted: %w", outboundName, err))
			return
		}
		// Successful swap — reRaceFailStreak reset inside replaceOutboundServer.
	}
}

// applyReRaceResult installs a successful Phase-A RaceTransport result into
// the target's existing TransportManager. Returns false if the target is no
// longer owned by the engine (caller must clean up the result).
func (e *Engine) applyReRaceResult(target *outboundTransport, result *wire.RaceResult) bool {
	e.mu.Lock()
	found := false
	for _, ot := range e.outboundTMs {
		if ot == target {
			found = true
			break
		}
	}
	e.mu.Unlock()
	if !found {
		return false
	}

	if result.QUICBackup != nil {
		// QUIC connected but lost re-race to tcpws — install QUIC as primary
		// with fallback. ReplacePrimary clears fallbackActive, so re-set it.
		target.tm.ReplacePrimary(result.QUICBackup, result.QUICBackupType)
		oldTCPWS := target.tm.SwapTCPWS(result.Dialer)
		if oldTCPWS != nil {
			oldTCPWS.Close()
		}
		target.tm.SetFallbackActiveWithReason(true, "quic-race-lost")
		slog.Info("engine: re-race QUIC installed as primary (race loser), tcpws serving traffic",
			"outbound", target.name,
			"quicType", result.QUICBackupType,
		)
	} else {
		target.tm.ReplacePrimary(result.Dialer, result.TransportType)
	}
	return true
}

// replaceOutboundServer obtains a replacement k2v5:// URL from the provider,
// builds a fresh TransportManager in the background via
// setupOutboundTransport, and atomically hot-swaps it into the target's
// SwappableDialer. Returns nil on successful swap; on provider exhaustion
// or context cancellation, returns the underlying error.
//
// Crucially, this function does NOT tear down TUN, provider, rule engine,
// connTracker, or the DNS hijack: it swaps a single Dialer reference
// underneath the running stack. ProxyDNSClient's pooled smux connections
// are explicitly reset after the swap so the next DNS query dials through
// the new transport.
//
// The old TransportManager is Closed immediately on successful swap —
// quic-go / smux OpenStreamSync callers unblock within ms with
// ConnectionClosed errors, which the application layer retries on new
// connections through the new TM.
func (e *Engine) replaceOutboundServer(
	ctx context.Context,
	target *outboundTransport,
	lastWireErr error,
	dd wire.DirectDialer,
	provider OutboundProvider,
) error {
	e.mu.Lock()
	currentURL := target.url
	wireMode := e.wireMode
	e.mu.Unlock()
	reason := lastWireErr

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		newURL, providerErr := provider.NextURL(currentURL, reason)
		if providerErr != nil {
			return fmt.Errorf("provider: %w", providerErr)
		}
		if newURL == "" {
			return fmt.Errorf("provider returned empty URL")
		}

		slog.Info("engine: attempting outbound replacement",
			"outbound", target.name,
			"newURL", config.TruncateVia(newURL))

		newOT, _, setupErr := setupOutboundTransportFn(ctx, newURL, dd, wireMode)
		if setupErr != nil {
			slog.Warn("engine: outbound replacement setup failed",
				"outbound", target.name,
				"tryURL", config.TruncateVia(newURL),
				"err", setupErr)
			// Tell the provider this URL also failed so it excludes and
			// returns the next candidate on the next call.
			currentURL = newURL
			reason = setupErr
			continue
		}
		if ctx.Err() != nil {
			newOT.tm.Close()
			return ctx.Err()
		}

		// Verify target still alive and owned.
		e.mu.Lock()
		stillConnected := e.state == StateConnected
		found := false
		for _, ot := range e.outboundTMs {
			if ot == target {
				found = true
				break
			}
		}
		proxyDNS := e.proxyDNS
		e.mu.Unlock()
		if !stillConnected || !found {
			newOT.tm.Close()
			return fmt.Errorf("target outbound no longer live")
		}

		// Atomic swap. SwappableDialer returns the previously-installed
		// Dialer, which is the old *TransportManager we must Close.
		oldDialer := target.swappable.Swap(newOT.tm)

		e.mu.Lock()
		target.tm = newOT.tm
		target.wireCfg = newOT.wireCfg
		target.dialAddr = newOT.dialAddr
		target.name = newOT.name
		target.url = newURL
		target.reRaceFailStreak = 0
		e.mu.Unlock()

		// ProxyDNSClient holds the same *SwappableDialer instance as
		// before, so future DialStreamUDP calls now route to the new TM.
		// But any pooled proxyConn (smux session on the old TM) is dead;
		// reset so the next query redials. Mirrors reconnect() step 3.
		if proxyDNS != nil {
			proxyDNS.ResetConnections()
		}

		// Close old TM outside any lock. quic-go Conn.destroy / smux
		// Session.Close immediately unblocks in-flight OpenStreamSync
		// calls with ConnectionClosed — no hang, no drain.
		if oldTM, ok := oldDialer.(*wire.TransportManager); ok && oldTM != nil {
			oldTM.Close()
		}

		slog.Info("engine: outbound replaced",
			"outbound", target.name,
			"newURL", config.TruncateVia(newURL))
		return nil
	}
}

// lowerCopy returns a new slice with every element lowercased.
// Used by buildRouteEntries to pre-lowercase ProcessNamePrefix entries at
// compile time so matchMeta's per-flow hot path only lowercases the
// observed process name once. Returns nil when in is empty so
// hasAnyCriteria len-checks stay correct.
func lowerCopy(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = strings.ToLower(s)
	}
	return out
}

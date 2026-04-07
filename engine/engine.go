package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/sasha-s/go-deadlock"

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
	tm             *wire.TransportManager
	prov           provider.Provider
	networkMonitor NetworkChangeNotifier
	health         *healthMonitor
	netCoord       *netCoordinator
	dnsH           *dnsHandler // for counter reset on network recovery
	proxyDNS       *dns.ProxyDNSClient // proxy DNS connection pool (nil in proxy mode)
	ruleEngine     *rule.Engine
	outbounds      []core.Outbound // owned by engine, closed in Stop()

	wireCfg      wire.Config
	directDialer wire.DirectDialer
	reRaceChan   chan struct{}
	reRaceCancel context.CancelFunc
}

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

	// 1. Download rule bundles.
	bundleNames := rule.BundlesForConfig(extractMatches(client))
	sources := client.RuleSources
	if len(sources) == 0 {
		sources = rule.DefaultSources
	}
	if len(bundleNames) > 0 && client.CacheDir != "" {
		dlCtx, dlCancel := context.WithTimeout(ctx, 8*time.Second)
		err := rule.Download(dlCtx, client.CacheDir, bundleNames, sources)
		dlCancel()
		if err != nil && dlCtx.Err() == context.DeadlineExceeded {
			slog.Warn("rule: download timeout, starting with fallback routing")
		} else if err != nil {
			return e.fail(fmt.Errorf("rule download: %w", err))
		}
	}

	// 1b. Build outbound map (via URL → Target index) and rule engine.
	outboundMap := buildOutboundMap(client.Routes)
	ruleEngine := buildRuleEngine(client, outboundMap)

	slog.Info("engine: start",
		"server", extractServerHost(client.Server),
		"mode", client.Mode,
		"allProxy", ruleEngine.IsAllProxy(),
		"dnsDirect", client.DNS.Direct,
		"dnsProxy", client.DNS.Proxy,
		"fd", cfg.FileDescriptor,
		"hasDirectDialer", cfg.DirectDialer != nil,
		"hasNetworkMonitor", cfg.NetworkMonitor != nil,
	)
	slog.Info("engine: step 1 - rule engine init done", "elapsed", time.Since(startTime))

	// 2. Parse k2v5:// URL.
	wireCfg, err := wire.ParseURL(client.Server)
	if err != nil {
		return e.fail(err)
	}

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

	// 3. Resolve dial address via Happy Eyeballs (RFC 8305).
	// When both IPv6 and IPv4 are available, races them with IPv6 head start.
	// Must complete BEFORE step 4 — NewQUICClient/NewTCPWSClient cache DialAddr.
	dialAddr, err := resolveDialAddr(ctx, wireCfg, cfg.DirectDialer)
	if err != nil {
		if cfg.NetworkMonitor != nil {
			cfg.NetworkMonitor.Close()
		}
		return e.fail(fmt.Errorf("address resolution: %w", err))
	}
	if dialAddr != "" {
		wireCfg.DialAddr = dialAddr
	} else {
		slog.Warn("engine: k2v5 URL missing ip= parameter, using Host for dial (deprecated)")
	}
	slog.Info("engine: step 3 - dial address resolved", "addr", wireCfg.DialAddr, "elapsed", time.Since(startTime))

	// 4. Transport race: three-way Happy Eyeballs.
	// Races QUIC-443, QUIC-hop, TCP-WS with staggered start and echo probe
	// verification. First candidate with verified data flow wins. Replaces
	// the serial build + handshake probe (old step 4 + 4.5).
	slog.Info("engine: step 4 - transport race", "elapsed", time.Since(startTime))
	raceCtx, raceCancel := context.WithTimeout(ctx, 15*time.Second)
	raceResult, raceErr := wire.RaceTransport(raceCtx, wireCfg, wireCfg.DialAddr, cfg.DirectDialer)
	raceCancel()

	if raceErr != nil {
		slog.Warn("engine: transport race failed", "err", raceErr)
		if cfg.NetworkMonitor != nil {
			cfg.NetworkMonitor.Close()
		}
		return e.fail(fmt.Errorf("transport race: %w", raceErr))
	}

	tm := wire.NewTransportManager()
	tm.SetPrimary(raceResult.Dialer, raceResult.TransportType)

	// Ensure TCP-WS is available as TM fallback AND as DNS transport.
	// DNS is control-plane traffic — use TCP-WS directly (most reliable)
	// instead of TransportManager (QUIC-first, susceptible to Short Header blocking).
	//
	// tcpwsDialer is intentionally the SAME instance as tm.tcpws (shared).
	// Why not a separate TCPWSClient for DNS?
	//   1. reconnect() calls tm.ResetConnections() which resets the shared tcpws
	//      smux session — DNS's next DialStreamUDP() then connect()s through a
	//      fresh session on the new network path. A separate instance would keep
	//      its stale session and timeout on the old path unless explicitly reset.
	//   2. In-flight DNS queries during reset are already cancelled by
	//      CancelInflight() BEFORE tm.ResetConnections() — the race window
	//      (new query enters between CancelInflight and Reset) is sub-microsecond
	//      and harmless (query fails, racing resolver retries on fresh session).
	//   3. One fewer TCP connection and smux session per engine lifecycle.
	// If this assumption ever breaks, grep for "DIAG: dns-proxy-conn-dead"
	// during reconnect — a burst of these without preceding CancelInflight
	// would indicate the shared-instance race is wider than expected.
	var tcpwsDialer wire.Dialer
	if raceResult.TCPWS != nil {
		tm.SetTCPWS(raceResult.TCPWS)
		tcpwsDialer = raceResult.TCPWS
	} else if raceResult.TransportType != "tcpws" {
		// Build a standalone TCP-WS client as fallback.
		tcpwsClient := wire.NewTCPWSClient(wireCfg, nil)
		if cfg.DirectDialer != nil {
			tcpwsClient.SetDirectDialer(cfg.DirectDialer)
		}
		tm.SetTCPWS(tcpwsClient)
		tcpwsDialer = tcpwsClient
	} else {
		// TCP-WS won race: primary IS tcpws, use it for DNS too.
		tcpwsDialer = raceResult.Dialer
	}
	// Start background QUIC recovery probe (active during TCP-WS fallback).
	// Complements echoLoop: quicProbeLoop handles handshake-level failure (UDP fully blocked),
	// echoLoop handles data-path failure (Short Header dropped). No overlap.
	tm.StartQuicProbe(ctx)

	slog.Info("engine: step 4 - transport race winner",
		"transport", raceResult.TransportType,
		"elapsed", time.Since(startTime),
	)
	slog.Info("DIAG: wire-handshake",
		"handshakeMs", time.Since(startTime).Milliseconds(),
		"transport", raceResult.TransportType,
	)

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

	slog.Debug("engine: step 6 - provider created", "provider", prov.Name(), "mode", client.Mode, "elapsed", time.Since(startTime))

	// Check cancellation before starting the tunnel (the expensive part).
	if ctx.Err() != nil {
		if cfg.NetworkMonitor != nil {
			cfg.NetworkMonitor.Close()
		}
		tm.Close()
		return e.fail(ctx.Err())
	}

	// 6.5. Build outbound table, indexed by rule.Target values from outboundMap.
	// outboundMap assigns indices: each unique via URL gets a Target starting at 2.
	outbounds := make([]core.Outbound, 2+len(outboundMap))
	outbounds[outboundDirect] = core.NewDirectOutbound(cfg.DirectDialer) // 0 = direct
	outbounds[outboundReject] = nil                                      // 1 = reject

	for via, target := range outboundMap {
		switch {
		case strings.HasPrefix(via, "k2v5://"):
			outbounds[target] = core.NewWireOutbound("k2v5", tm, e)
		case strings.HasPrefix(via, "k2p://"):
			k2pOut, k2pErr := core.NewK2POutbound(via)
			if k2pErr != nil {
				slog.Warn("engine: k2p outbound failed", "err", k2pErr)
				// Leave nil → reject behavior for this target.
				continue
			}
			outbounds[target] = k2pOut
		}
	}

	// 7. Build client tunnel.
	tunnelCfg := core.ClientTunnelConfig{
		Provider:        prov,
		Outbounds:       outbounds,
		DirectTarget:    outboundDirect,
		DefaultTarget:   outboundDirect, // router is always set; fallback for tests
		Router:          router,
		ProcessSearcher: cfg.ProcessSearcher,
		SniffBT:   !ruleEngine.IsAllProxy(), // sniff BT in smart mode only
		SniffSNI:  !ruleEngine.IsAllProxy(), // sniff TLS/QUIC SNI in smart mode only
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
			Dialer:  tcpwsDialer, // control-plane: TCP-WS directly, bypass QUIC
			Servers: client.DNS.Proxy,
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
			tm.Close()
			return e.fail(err)
		}
	} else {
		// Proxy mode: no TUN, no DNS interception.
		if err := tunnel.Start(ctx); err != nil {
			if cfg.NetworkMonitor != nil {
				cfg.NetworkMonitor.Close()
			}
			tm.Close()
			return e.fail(err)
		}
	}

	// 9. Commit — verify not cancelled before saving resources.
	e.mu.Lock()
	if ctx.Err() != nil {
		// Stop() was called during Start() — clean up ALL started resources.
		// Order follows resource ownership: prov → tm → tunnel (same as Stop()).
		e.mu.Unlock()
		if cfg.NetworkMonitor != nil {
			cfg.NetworkMonitor.Close()
		}
		prov.Close()
		tm.Close()
		tunnel.Close()
		return ctx.Err()
	}
	e.tunnel = tunnel
	e.tm = tm
	e.prov = prov
	e.outbounds = outbounds
	e.proxyDNS = proxyDNS
	e.ruleEngine = ruleEngine
	e.networkMonitor = cfg.NetworkMonitor

	// Start health monitor (1Hz wire stats sampling + health FSM).
	hm := newHealthMonitor(tm, tunnel, cfg.HealthCallback, func() { e.onHealthCritical() }, time.Now())
	hm.start()
	e.health = hm

	// Store wireCfg + directDialer for re-race (avoids re-parsing URL).
	e.wireCfg = wireCfg
	e.directDialer = cfg.DirectDialer

	// Re-race loop: replaces recoveryProbe.
	e.reRaceChan = make(chan struct{}, 1)
	reRaceCtx, reRaceCancel := context.WithCancel(ctx)
	e.reRaceCancel = reRaceCancel
	safego.Go(func() { e.reRaceLoop(reRaceCtx) })

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
	if len(bundleNames) > 0 && client.CacheDir != "" {
		rebuildFn := func(cacheDir string) ([]rule.RouteEntry, []*rule.Bundle, error) {
			newBundles, err := rule.Load(cacheDir)
			if err != nil {
				return nil, nil, err
			}
			idx := rule.Index(newBundles)
			entries, _, _ := buildRouteEntries(client.Routes, idx, outboundMap)
			return entries, newBundles, nil
		}
		rule.StartUpdater(ctx, ruleEngine, client.CacheDir, bundleNames, sources, rebuildFn)
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
		"allProxy", ruleEngine.IsAllProxy(),
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
	tm := e.tm
	pdns := e.proxyDNS
	dh := e.dnsH
	e.mu.Unlock()

	// Notify handler of transient reconnecting state (for UI feedback).
	if handler != nil {
		handler.OnStatus(Status{State: StateReconnecting})
	}

	// Reset order matters (shared tcpwsDialer — see comment in Start):
	//   1. CancelInflight — abort pending upstream queries (closes race window)
	//   2. tm.ResetConnections — resets shared tcpws smux session
	//   3. pdns.ResetConnections — cleans proxyConn pool
	// Step 1 MUST precede step 2 so in-flight DNS queries exit via ctx
	// before the smux session is torn down underneath them.
	if dh != nil {
		dh.CancelInflight()
	}
	if tm != nil {
		tm.ResetConnections()
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
	tm := e.tm
	pdns := e.proxyDNS
	e.mu.Unlock()

	// Pause has no CancelInflight — no in-flight DNS during iOS memory pause.
	// tm→pdns order: shared tcpwsDialer (see Start comment).
	if tm != nil {
		tm.ResetConnections()
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
	tm := e.tm
	pdns := e.proxyDNS
	s := e.buildStatusLocked()
	e.mu.Unlock()

	if tm != nil {
		tm.ResetConnections()
	}
	if pdns != nil {
		pdns.ResetConnections()
	}
	if handler != nil {
		handler.OnStatus(s)
	}
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
	e.netCoord = nil
	e.dnsH = nil
	e.mu.Unlock()

	// Stop goroutines WITHOUT holding e.mu — health monitor's onReconnect
	// callback calls OnNetworkChanged() which briefly acquires e.mu.
	// Holding e.mu here would cross-deadlock with hm.wg.Wait().
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
	tm := e.tm
	e.tm = nil
	tunnel := e.tunnel
	e.tunnel = nil
	e.proxyDNS = nil // no explicit close needed — tm.Close() closes shared tcpws → smux dies → recvLoops exit
	re := e.ruleEngine
	e.ruleEngine = nil
	outbounds := e.outbounds
	e.outbounds = nil
	e.client = nil
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
	if tm != nil {
		tm.Close()
	}
	// Close all outbounds. directOutbound and wireOutbound have no-op Close()
	// (engine owns tm, closed above). k2pOutbound closes its ICE/QUIC connection.
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
func (e *Engine) buildStatusLocked() Status {
	return Status{
		State:       e.state,
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
func (e *Engine) fail(err error) error {
	e.mu.Lock()
	e.lastError = ClassifyError(err)
	slog.Warn("engine: fail",
		"code", e.lastError.Code,
		"category", e.lastError.Category,
		"message", e.lastError.Message,
	)
	// Extract resources under lock (Rule #4: no Close under state lock).
	nm := e.networkMonitor
	e.networkMonitor = nil
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
	e.setStateLocked(StateDisconnected)
	handler := e.handler
	status := e.buildStatusLocked()
	e.mu.Unlock()

	// External calls outside lock.
	if handler != nil {
		handler.OnStatus(status)
	}
	if nm != nil {
		nm.Close()
	}
	return err
}

// buildRouteEntries converts config routes to rule.RouteEntry list using the given bundle index.
// outboundMap maps via URL → rule.Target for wire outbounds.
// Shared by buildRuleEngine (startup) and updater rebuild (hot-reload) — single code path
// ensures both always produce identical routing tables from the same config+bundles.
func buildRouteEntries(routes []config.RouteConfig, idx map[string]*rule.BundleSet, outboundMap map[string]rule.Target) (entries []rule.RouteEntry, fallback rule.Target, allProxy bool) {
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
			allProxy = (target != outboundDirect && target != outboundReject)
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

		// Inline rules.
		if len(route.Match.DomainSuffix) > 0 || len(route.Match.IPCIDR) > 0 {
			inline := rule.BuildInlineSet(route.Match.DomainSuffix, route.Match.IPCIDR)
			entry.Inline = &inline
		}

		// Meta criteria.
		entry.ProcessNames = route.Match.ProcessName
		entry.PackageNames = route.Match.PackageName
		entry.NetworkMatch = route.Match.Network
		entry.IPIsPrivate = route.Match.IPIsPrivate

		hasAnyCriteria := len(entry.Sets) > 0 ||
			(entry.Inline != nil && !entry.Inline.IsEmpty()) ||
			len(entry.ProcessNames) > 0 || len(entry.PackageNames) > 0 ||
			entry.NetworkMatch != "" || entry.IPIsPrivate
		if hasAnyCriteria {
			entries = append(entries, entry)
		}

		// Last route without match criteria = fallback.
		if i == len(routes)-1 && !route.Match.HasCriteria() {
			fallback = target
		}
	}
	return entries, fallback, allProxy
}

// buildOutboundMap assigns Target indices to each unique outbound URL in routes.
// Index 0 and 1 are reserved (direct/reject). Wire outbound indices start at 2.
func buildOutboundMap(routes []config.RouteConfig) map[string]rule.Target {
	m := map[string]rule.Target{}
	nextIdx := rule.Target(2) // 0=direct, 1=reject, 2+=wire outbounds
	for _, route := range routes {
		via := route.Via
		if via == "" || via == "direct" || via == "reject" {
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

// buildRuleEngine creates a rule.Engine from the client's route configuration.
// CacheDir must be set by the platform shell (daemon/appext) before calling engine.
func buildRuleEngine(client *config.ClientConfig, outboundMap map[string]rule.Target) *rule.Engine {
	// Simple case: single route with match:all and wire outbound → all proxy, no routes.
	if client.IsAllProxy() && len(client.Routes) == 1 {
		target := outboundMap[client.Routes[0].Via]
		return rule.NewEngine(nil, target, nil, true)
	}

	// Load bundles from cache.
	bundles, err := rule.Load(client.CacheDir)
	if err != nil {
		slog.Warn("engine: rule load failed", "err", err)
	}
	idx := rule.Index(bundles)
	entries, fallback, allProxy := buildRouteEntries(client.Routes, idx, outboundMap)

	hasRules := len(bundles) > 0
	for _, e := range entries {
		if e.Inline != nil && !e.Inline.IsEmpty() {
			hasRules = true
			break
		}
		if len(e.ProcessNames) > 0 || len(e.PackageNames) > 0 {
			hasRules = true
			break
		}
	}
	if !hasRules {
		slog.Info("engine: no rule bundles or inline rules, using fallback only",
			"cacheDir", client.CacheDir, "fallback", fallback)
	} else {
		slog.Info("engine: rule engine loaded",
			"routes", len(entries), "fallback", fallback)
	}

	return rule.NewEngine(entries, fallback, bundles, allProxy)
}

// extractMatches collects MatchConfig from each route for BundlesForConfig.
func extractMatches(client *config.ClientConfig) []rule.MatchConfig {
	matches := make([]rule.MatchConfig, len(client.Routes))
	for i, r := range client.Routes {
		matches[i] = r.Match
	}
	return matches
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

// reRaceLoop waits for echo-fail signals and runs a transport re-race to
// hot-swap the primary transport. Replaces recoveryProbe.
//
// Known TOCTOU (P2, not fixed): If Stop() fires between the ctx.Err() check
// and ReplacePrimary, and tm.Close() wins the tm.mu race, the new Dialer is
// orphaned in a defunct TransportManager. Bounded leak: QUIC idle timeout (30s)
// reclaims it. Fix: add a WaitGroup so Stop() waits for this loop to exit
// before calling tm.Close(). Not worth a standalone PR — fold into next
// engine.go change.
func (e *Engine) reRaceLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-e.reRaceChan:
		}

		e.mu.Lock()
		if e.state != StateConnected {
			e.mu.Unlock()
			continue
		}
		tm := e.tm
		wireCfg := e.wireCfg
		dd := e.directDialer
		e.mu.Unlock()

		if tm == nil {
			continue
		}

		slog.Warn("DIAG: transport-rerace", "reason", "echo-consecutive-fail", "previousTransport", tm.PrimaryType())

		raceCtx, raceCancel := context.WithTimeout(ctx, 15*time.Second)
		result, err := wire.RaceTransport(raceCtx, wireCfg, wireCfg.DialAddr, dd)
		raceCancel()

		if err != nil {
			slog.Warn("engine: re-race failed", "err", err)
			continue
		}

		if ctx.Err() != nil {
			result.Dialer.Close()
			return
		}

		e.mu.Lock()
		currentTM := e.tm
		e.mu.Unlock()
		if currentTM == nil {
			result.Dialer.Close()
			continue
		}

		currentTM.ReplacePrimary(result.Dialer, result.TransportType)
	}
}

// ForceConnected sets the engine to connected state and fires OnStatus.
// For testing only — allows daemon tests to simulate a connected engine.
// Releases lock before calling handler to match OnNetworkChanged/Pause/Wake pattern.
func (e *Engine) ForceConnected() {
	e.mu.Lock()
	e.state = StateConnected
	e.connectedAt = time.Now()
	e.lastError = nil
	status := e.buildStatusLocked()
	handler := e.handler
	e.mu.Unlock()
	if handler != nil {
		handler.OnStatus(status)
	}
}

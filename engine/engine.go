package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"net/url"
	"time"

	"github.com/kaitu-io/k2rule"
	"github.com/sasha-s/go-deadlock"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/core"
	"github.com/kaitu-io/k2/core/dns"
	"github.com/kaitu-io/k2/provider"
	"github.com/kaitu-io/k2/wire"
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
	probe          *recoveryProbe
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
	slog.Info("engine: start",
		"server", extractServerHost(client.Server),
		"mode", client.Mode,
		"ruleGlobal", client.Rule.Global,
		"dnsDirect", client.DNS.Direct,
		"dnsProxy", client.DNS.Proxy,
		"fd", cfg.FileDescriptor,
		"hasDirectDialer", cfg.DirectDialer != nil,
		"hasNetworkMonitor", cfg.NetworkMonitor != nil,
	)

	// 1. Rule config — built directly from client.Rule.
	ruleCfg := buildRuleConfig(client)
	if err := k2rule.Init(ruleCfg); err != nil {
		slog.Warn("engine: k2rule init failed, continuing with defaults", "err", err)
	}
	slog.Info("engine: step 1 - k2rule init done", "elapsed", time.Since(startTime))

	// 2. Parse k2v5:// URL.
	wireCfg, err := wire.ParseURL(client.Server)
	if err != nil {
		return e.fail(err)
	}

	// 2.5. Start network monitor — DirectDialer needs DefaultInterface()
	// for interface-bound dialing. Must start before Happy Eyeballs (step 3).
	// Safe: OnNetworkChanged() early-returns when state != connected.
	if cfg.NetworkMonitor != nil {
		if err := cfg.NetworkMonitor.Start(func() { e.OnNetworkChanged() }); err != nil {
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

	// 4. Build transports.
	quicClient := wire.NewQUICClient(wireCfg, nil)
	tcpwsClient := wire.NewTCPWSClient(wireCfg, nil)

	if cfg.DirectDialer != nil {
		quicClient.SetDirectDialer(cfg.DirectDialer)
		tcpwsClient.SetDirectDialer(cfg.DirectDialer)
	}

	tm := wire.NewTransportManager()
	switch wireCfg.Transport {
	case "quic":
		tm.SetQUIC(quicClient)
	case "tcpws":
		tm.SetTCPWS(tcpwsClient)
	default:
		tm.SetQUIC(quicClient)
		tm.SetTCPWS(tcpwsClient)
	}
	// Start background QUIC recovery probe (active during TCP-WS fallback).
	tm.StartQuicProbe(ctx)

	slog.Debug("engine: step 4 - transports configured",
		"transport", wireCfg.Transport,
		"hasQUIC", quicClient != nil,
		"hasTCPWS", tcpwsClient != nil,
	)

	// 4.5 Verify wire handshake (client ↔ server) before setting up TUN/provider.
	// Completes QUIC or TCP-WS handshake only — does NOT proxy to any target.
	// Without this check, engine reports "connected" after TUN starts even if
	// the server is unreachable. DNS failures are silently accumulated (threshold=30)
	// and the health monitor sees no transport data, creating a ~15s false-connected window.
	// The cached connection is reused by subsequent DialTCP/DialUDP (fast path).
	slog.Info("engine: step 4.5 - wire handshake probe", "elapsed", time.Since(startTime))
	hsStart := time.Now()
	hsCtx, hsCancel := context.WithTimeout(ctx, 15*time.Second)
	hsErr := tm.Handshake(hsCtx)
	hsCancel()
	hsMs := time.Since(hsStart).Milliseconds()
	if hsErr != nil {
		slog.Warn("engine: wire handshake failed", "err", hsErr)
		slog.Warn("DIAG: wire-handshake-fail", "handshakeMs", hsMs, "err", hsErr)
		if cfg.NetworkMonitor != nil {
			cfg.NetworkMonitor.Close()
		}
		tm.Close()
		return e.fail(fmt.Errorf("wire handshake: %w", hsErr))
	}
	slog.Info("engine: step 4.5 - wire handshake ok", "elapsed", time.Since(startTime))
	slog.Info("DIAG: wire-handshake", "handshakeMs", hsMs, "transport", tm.WireStats().Transport)

	// 5. Router.
	router := core.NewRouter()

	// 6. Build provider.
	var prov provider.Provider
	if cfg.FileDescriptor >= 0 {
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
	} else if client.Mode == "tproxy" {
		// Linux tproxy mode (OpenWRT, headless Linux).
		prov = provider.NewTProxyProvider(provider.ProviderConfig{
			Mode:        "tproxy",
			IPv4Address: client.Tun.IPv4,
			IPv6Address: client.Tun.IPv6,
		})
	} else {
		// Desktop TUN mode.
		prov = provider.NewTUNProvider(provider.ProviderConfig{
			Mode:             "tun",
			RouteExcludeAddrs: computeDNSExclude(client),
			InterfaceMonitor: cfg.InterfaceMonitor,
			IPv4Address:      client.Tun.IPv4,
			IPv6Address:      client.Tun.IPv6,
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

	// 7. Build client tunnel.
	tunnelCfg := core.ClientTunnelConfig{
		Provider:     prov,
		Dialer:       tm,
		Router:       router,
		WireReporter: e,
		SniffBT:   !client.Rule.Global, // sniff BT in smart mode only
		SniffSNI:  !client.Rule.Global, // sniff TLS/QUIC SNI in smart mode only
		BlockQUIC: client.BlockQUIC,
	}
	if cfg.DirectDialer != nil {
		tunnelCfg.DirectDialer = cfg.DirectDialer
	}
	tunnel := core.NewClientTunnel(tunnelCfg)

	// 8. Start.
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
		dnsMW := dns.NewMiddleware(dns.MiddlewareConfig{
			ProxyDNS: dns.NewProxyDNSClient(dns.ProxyDNSClientConfig{
				Dialer:  tm,
				Servers: client.DNS.Proxy,
			}),
			DirectDNS: dns.NewDirectDNSClient(client.DNS.Direct, dnsOpts...),
			DNSCache:  dns.NewDNSCache(dns.DNSCacheConfig{}),
			Filter:    dns.NewFilter(dns.FilterConfig{}),
		})
		if err := tunnel.StartWithHandler(ctx, &dnsHandler{inner: tunnel, dns: dnsMW, ctx: ctx, engine: e}); err != nil {
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
	e.networkMonitor = cfg.NetworkMonitor

	// Start health monitor (1Hz wire stats sampling + health FSM).
	hm := newHealthMonitor(tm, tunnel, cfg.HealthCallback, func() { e.onHealthCritical() }, time.Now())
	hm.start()
	e.health = hm

	// Recovery probe: actively verifies wire connectivity after network changes.
	e.probe = newRecoveryProbe(tm, e)

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
		"ruleGlobal", client.Rule.Global,
		"dialMs", time.Since(startTime).Milliseconds(),
	)

	// Network monitor already started in step 4.1 (before wire probe).

	return nil
}

// reconnect resets wire transport connections and triggers recovery probe.
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
	e.mu.Unlock()

	// Notify handler of transient reconnecting state (for UI feedback).
	if handler != nil {
		handler.OnStatus(Status{State: StateReconnecting})
	}

	// Reset cached connections — next dial will create fresh ones.
	if tm != nil {
		tm.ResetConnections()
	}

	// Reset health monitor — stale data from old connection is irrelevant.
	e.mu.Lock()
	hm := e.health
	e.mu.Unlock()
	if hm != nil {
		hm.resetCounters()
	}

	// Trigger recovery probe to actively verify wire connectivity.
	e.mu.Lock()
	probe := e.probe
	e.mu.Unlock()
	if probe != nil {
		probe.trigger()
	}
	return true
}

// OnNetworkChanged should be called by the platform layer when a network
// change is detected (e.g., WiFi to cellular). Resets wire transports and
// clears stale network errors. Auth errors survive (WiFi→cellular doesn't
// fix expired subscriptions).
func (e *Engine) OnNetworkChanged() {
	slog.Info("engine: network changed")
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

// onHealthCritical is called by healthMonitor when wire quality is critically degraded.
// Unlike OnNetworkChanged, it does NOT clear wire errors (degradation ≠ network switch).
func (e *Engine) onHealthCritical() {
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
	e.mu.Unlock()

	if tm != nil {
		tm.ResetConnections()
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
	s := e.buildStatusLocked()
	e.mu.Unlock()

	if tm != nil {
		tm.ResetConnections()
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
	probe := e.probe
	e.probe = nil
	e.mu.Unlock()

	// Stop goroutines WITHOUT holding e.mu — health monitor's onReconnect
	// callback calls OnNetworkChanged() which briefly acquires e.mu.
	// Holding e.mu here would cross-deadlock with hm.wg.Wait().
	if hm != nil {
		hm.stop()
	}
	if probe != nil {
		probe.stop()
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
	if tunnel != nil {
		tunnel.Close()
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

// buildRuleConfig creates a k2rule.Config from the client's Rule settings.
// CacheDir must be set by the platform shell (daemon/mobile) before calling engine.
func buildRuleConfig(client *config.ClientConfig) *k2rule.Config {
	rule := client.Rule
	ruleCfg := &k2rule.Config{
		RuleURL:  rule.RuleURL,
		GeoIPURL: rule.GeoIPURL,
		Antiporn: rule.Antiporn,
		PornURL:  rule.PornURL,
		CacheDir: rule.CacheDir,
		IsGlobal: rule.Global,
	}
	if rule.Global {
		ruleCfg.GlobalTarget = k2rule.TargetProxy
	}
	return ruleCfg
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

// ReportWireError reports a runtime wire transport error (proxy dial failure).
// Called by tunnel's handleTCPProxy/handleUDPProxy on proxy dial failure.
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

package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"net/url"
	"sync"
	"time"

	"github.com/kaitu-io/k2rule"

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
// (e.g. daemon.mu). setState/fail call handler.OnStatus under engine.mu.
// EventHandler implementations must NOT call back into engine while holding
// their own lock.
type Engine struct {
	mu          sync.Mutex
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

	e.setState(StateConnecting)
	e.mu.Unlock()

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
	slog.Debug("engine: step 4 - transports configured",
		"transport", wireCfg.Transport,
		"hasQUIC", quicClient != nil,
		"hasTCPWS", tcpwsClient != nil,
	)

	// 4.5 Verify wire connectivity before setting up TUN/provider.
	// Without this check, engine reports "connected" after TUN starts even if
	// wire server is unreachable — DNS handler silently absorbs wire failures
	// and ReportWireError() never triggers (no TCP connections created).
	slog.Info("engine: step 4.5 - wire connectivity probe", "elapsed", time.Since(startTime))
	probeCtx, probeCancel := context.WithTimeout(ctx, 15*time.Second)
	probeConn, probeErr := tm.DialTCP(probeCtx, "1.1.1.1:443")
	probeCancel()
	if probeErr != nil {
		slog.Warn("engine: wire probe failed", "err", probeErr)
		if cfg.NetworkMonitor != nil {
			cfg.NetworkMonitor.Close()
		}
		tm.Close()
		return e.fail(fmt.Errorf("wire probe: %w", probeErr))
	}
	probeConn.Close()
	slog.Info("engine: step 4.5 - wire probe ok", "elapsed", time.Since(startTime))

	// 4.6 Optional UDP probe — non-fatal, logs warning if UDP broken.
	// Runtime datagram health tracking (Step 2) handles fallback.
	udpProbeCtx, udpProbeCancel := context.WithTimeout(ctx, 5*time.Second)
	udpConn, udpErr := tm.DialUDP(udpProbeCtx, "8.8.8.8:53")
	udpProbeCancel()
	if udpErr != nil {
		slog.Warn("engine: wire UDP probe failed (DNS will use fallback)", "err", udpErr)
	} else {
		udpConn.Close()
		slog.Info("engine: step 4.6 - wire UDP probe ok", "elapsed", time.Since(startTime))
	}

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
		BlockQUIC: true,
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
		// Stop() was called during Start() — clean up started resources.
		e.mu.Unlock()
		if cfg.NetworkMonitor != nil {
			cfg.NetworkMonitor.Close()
		}
		prov.Close()
		tm.Close()
		return ctx.Err()
	}
	e.tunnel = tunnel
	e.tm = tm
	e.prov = prov
	e.networkMonitor = cfg.NetworkMonitor

	// Start health monitor (1Hz wire stats sampling + health FSM).
	hm := newHealthMonitor(tm, tunnel, cfg.HealthCallback, func() { e.OnNetworkChanged() })
	hm.start()
	e.health = hm

	e.setState(StateConnected)
	e.mu.Unlock()
	slog.Info("engine: connected", "totalMs", time.Since(startTime).Milliseconds())

	// Network monitor already started in step 4.1 (before wire probe).

	return nil
}

// OnNetworkChanged should be called by the platform layer when a network
// change is detected (e.g., WiFi to cellular). If the engine is connected,
// it resets wire transport connections to force fresh connections on the
// next dial attempt, avoiding dead cached connections.
func (e *Engine) OnNetworkChanged() {
	e.mu.Lock()
	if e.state != StateConnected || e.paused {
		e.mu.Unlock()
		return
	}
	handler := e.handler
	tm := e.tm
	e.mu.Unlock()
	slog.Info("engine: network changed")

	// Notify handler of transient reconnecting state (for UI feedback).
	if handler != nil {
		handler.OnStatus(Status{State: StateReconnecting})
	}

	// Reset cached connections — next dial will create fresh ones.
	if tm != nil {
		tm.ResetConnections()
	}

	// Reset health monitor — stale streaks from old network are irrelevant.
	e.mu.Lock()
	hm := e.health
	e.mu.Unlock()
	if hm != nil {
		hm.resetCounters()
	}

	// Clear non-client wire errors (network errors are stale after network change).
	// Auth errors survive — WiFi→cellular doesn't fix expired subscriptions.
	e.mu.Lock()
	if e.lastError != nil && e.lastError.Category != CategoryClient {
		slog.Debug("engine: network change cleared stale error",
			"prevCode", e.lastError.Code,
			"prevCategory", e.lastError.Category,
		)
		e.lastError = nil
	}
	s := e.buildStatusLocked()
	e.mu.Unlock()

	// Notify handler we're back to connected with full status snapshot.
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
	defer e.mu.Unlock()

	if e.state == StateDisconnected {
		return nil
	}
	slog.Info("engine: stop",
		"previousState", e.state,
		"hadError", e.lastError != nil,
	)

	// Stop health monitor first — its goroutine reads tm and tunnel.
	if e.health != nil {
		e.health.stop()
		e.health = nil
	}
	if e.networkMonitor != nil {
		e.networkMonitor.Close()
		e.networkMonitor = nil
	}
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
	// Close provider and transport first — this forces in-flight pipe/pipePacket
	// goroutines to error out on the closed connections and exit.
	if e.prov != nil {
		e.prov.Close()
		e.prov = nil
	}
	if e.tm != nil {
		e.tm.Close()
		e.tm = nil
	}
	if e.tunnel != nil {
		// tunnel.Close() only drains handler goroutines (connTracker.closeAll + wg.Wait).
		// Provider and dialer are owned by engine and already closed above.
		e.tunnel.Close()
		e.tunnel = nil
	}
	e.client = nil
	e.connectedAt = time.Time{}
	e.paused = false
	e.lastError = nil // unconditional clear on stop

	e.setState(StateDisconnected)
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

// setState updates state and notifies the EventHandler with a full status snapshot.
// Must be called with e.mu held.
func (e *Engine) setState(s string) {
	if s == StateConnected {
		e.connectedAt = time.Now()
	}
	e.state = s
	if e.handler != nil {
		e.handler.OnStatus(e.buildStatusLocked())
	}
}

// fail records an error, cancels any in-flight context, resets to
// disconnected, and returns the error. The OnStatus event carries both
// the disconnected state and the classified error atomically.
func (e *Engine) fail(err error) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.lastError = ClassifyError(err)
	slog.Warn("engine: fail",
		"code", e.lastError.Code,
		"category", e.lastError.Category,
		"message", e.lastError.Message,
	)
	if e.networkMonitor != nil {
		e.networkMonitor.Close()
		e.networkMonitor = nil
	}
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
	e.setState(StateDisconnected)
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

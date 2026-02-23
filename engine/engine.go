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
type Engine struct {
	mu          sync.Mutex
	state       string
	client      *config.ClientConfig
	connectedAt time.Time
	lastError   *EngineError
	handler     EventHandler

	cancel         context.CancelFunc
	tunnel         *core.ClientTunnel
	tm             *wire.TransportManager
	prov           provider.Provider
	networkMonitor NetworkChangeNotifier
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

	// 1. Rule config — built directly from client.Rule.
	ruleCfg := buildRuleConfig(client)
	if err := k2rule.Init(ruleCfg); err != nil {
		slog.Warn("engine: k2rule init failed, continuing with defaults", "err", err)
	}
	slog.Info("engine: step 1 - k2rule init done")

	// 2. Parse k2v5:// URL.
	wireCfg, err := wire.ParseURL(client.Server)
	if err != nil {
		return e.fail(err)
	}

	// 3. Resolve dial address: ip6 (preferred) > ip4 (required).
	// Host stays as TLS SNI domain; DialAddr is used for TCP/UDP dialing.
	if wireCfg.IPv6 != "" {
		wireCfg.DialAddr = wireCfg.IPv6
		slog.Info("engine: using IPv6 for dial", "addr", wireCfg.IPv6)
	} else if wireCfg.IPv4 != "" {
		wireCfg.DialAddr = wireCfg.IPv4
		slog.Info("engine: using IPv4 for dial", "addr", wireCfg.IPv4)
	} else {
		slog.Warn("engine: k2v5 URL missing ip= parameter, using Host for dial (deprecated)")
	}

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

	// 5. Router.
	router := core.NewRouter()

	// 6. Build provider.
	var prov provider.Provider
	if cfg.FileDescriptor >= 0 {
		// Mobile / macOS sysext: platform-provided TUN FD.
		prov = provider.NewTUNProvider(provider.ProviderConfig{
			FileDescriptor:   cfg.FileDescriptor,
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
		// Desktop TUN mode.
		prov = provider.NewTUNProvider(provider.ProviderConfig{
			Mode:             "tun",
			DNSServers:       computeDNSExclude(client),
			InterfaceMonitor: cfg.InterfaceMonitor,
			IPv4Address:      client.Tun.IPv4,
			IPv6Address:      client.Tun.IPv6,
		})
	}

	// Check cancellation before starting the tunnel (the expensive part).
	if ctx.Err() != nil {
		tm.Close()
		return e.fail(ctx.Err())
	}

	// 7. Build client tunnel.
	tunnelCfg := core.ClientTunnelConfig{
		Provider: prov,
		Dialer:   tm,
		Router:   router,
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
		if err := tunnel.StartWithHandler(ctx, &dnsHandler{inner: tunnel, dns: dnsMW}); err != nil {
			tm.Close()
			return e.fail(err)
		}
	} else {
		// Proxy mode: no TUN, no DNS interception.
		if err := tunnel.Start(ctx); err != nil {
			tm.Close()
			return e.fail(err)
		}
	}

	// 9. Commit — verify not cancelled before saving resources.
	e.mu.Lock()
	if ctx.Err() != nil {
		// Stop() was called during Start() — clean up started resources.
		e.mu.Unlock()
		prov.Close()
		tm.Close()
		return ctx.Err()
	}
	e.tunnel = tunnel
	e.tm = tm
	e.prov = prov
	e.networkMonitor = cfg.NetworkMonitor
	e.setState(StateConnected)
	e.mu.Unlock()

	// Start network monitor if provided (desktop only).
	if cfg.NetworkMonitor != nil {
		if err := cfg.NetworkMonitor.Start(func() { e.OnNetworkChanged() }); err != nil {
			slog.Warn("engine: network monitor start failed", "err", err)
		}
	}

	return nil
}

// OnNetworkChanged should be called by the platform layer when a network
// change is detected (e.g., WiFi to cellular). If the engine is connected,
// it resets wire transport connections to force fresh connections on the
// next dial attempt, avoiding dead cached connections.
func (e *Engine) OnNetworkChanged() {
	e.mu.Lock()
	if e.state != StateConnected {
		e.mu.Unlock()
		return
	}
	handler := e.handler
	tm := e.tm
	e.mu.Unlock()

	// Notify handler of transient reconnecting state (for UI feedback).
	if handler != nil {
		handler.OnStateChange("reconnecting")
	}

	// Reset cached connections — next dial will create fresh ones.
	if tm != nil {
		tm.ResetConnections()
	}

	// Notify handler we're back to connected.
	if handler != nil {
		handler.OnStateChange(StateConnected)
	}
}

// Stop tears down the tunnel and returns the engine to disconnected.
func (e *Engine) Stop() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.state == StateDisconnected {
		return nil
	}

	if e.networkMonitor != nil {
		e.networkMonitor.Close()
		e.networkMonitor = nil
	}
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
	if e.prov != nil {
		e.prov.Close()
		e.prov = nil
	}
	if e.tm != nil {
		e.tm.Close()
		e.tm = nil
	}
	e.tunnel = nil
	e.client = nil
	e.connectedAt = time.Time{}

	e.setState(StateDisconnected)
	return nil
}

// StatusJSON returns the engine status as a JSON string.
// The format uses snake_case keys for consistency with the daemon API.
func (e *Engine) StatusJSON() string {
	e.mu.Lock()
	defer e.mu.Unlock()

	info := map[string]any{
		"state": e.state,
	}
	if e.state == StateConnected && !e.connectedAt.IsZero() {
		info["connected_at"] = e.connectedAt.Format(time.RFC3339)
		info["uptime_seconds"] = int(time.Since(e.connectedAt).Seconds())
	}
	if e.lastError != nil {
		info["error"] = e.lastError
	}
	if e.client != nil {
		info["wire_url"] = e.client.Server
	}
	b, _ := json.Marshal(info)
	return string(b)
}

// setState updates state and notifies the EventHandler.
// Must be called with e.mu held.
func (e *Engine) setState(s string) {
	if s == StateConnected {
		e.connectedAt = time.Now()
	}
	e.state = s
	if e.handler != nil {
		e.handler.OnStateChange(s)
	}
}

// fail records an error, cancels any in-flight context, resets to
// disconnected, and returns the error.
func (e *Engine) fail(err error) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.lastError = ClassifyError(err)
	if e.networkMonitor != nil {
		e.networkMonitor.Close()
		e.networkMonitor = nil
	}
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
	if e.handler != nil {
		e.handler.OnError(err.Error())
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

	// Resolve server host to IPs for route exclusion.
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

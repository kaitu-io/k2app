package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"sync"
	"time"

	"github.com/kaitu-io/k2rule"

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
	wireUrl     string
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
	e.wireUrl = cfg.WireURL
	e.lastError = nil

	// Create context early so Stop() can cancel an in-progress Start().
	ctx, cancel := context.WithCancel(context.Background())
	e.cancel = cancel

	e.setState(StateConnecting)
	e.mu.Unlock()

	// 1. Parse rule mode: RuleConfig > RuleMode > URL query "rule=" > default "global".
	ruleCfg := e.buildK2ruleConfig(cfg)
	if err := k2rule.Init(ruleCfg); err != nil {
		log.Printf("engine: k2rule init: %v (continuing with defaults)", err)
	}

	// 2. Parse k2v5:// URL.
	wireCfg, err := wire.ParseURL(cfg.WireURL)
	if err != nil {
		return e.fail(err)
	}

	// 3. Prefer IPv6 when configured and both server and client support it.
	if cfg.PreferIPv6 && wireCfg.IPv6 != "" && wire.HasIPv6() {
		log.Printf("engine: IPv6 available, using %s", wireCfg.IPv6)
		wireCfg.Host = wireCfg.IPv6
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
		// Mobile: platform-provided TUN FD.
		prov = provider.NewTUNProvider(provider.ProviderConfig{
			FileDescriptor: cfg.FileDescriptor,
		})
	} else if cfg.Mode == "proxy" {
		// Desktop proxy mode.
		prov = provider.NewProxyProvider(provider.ProviderConfig{
			Mode:        "proxy",
			ProxyListen: cfg.ProxyListen,
		})
	} else {
		// Desktop TUN mode.
		prov = provider.NewTUNProvider(provider.ProviderConfig{
			Mode:             "tun",
			DNSServers:       cfg.DNSExclude,
			InterfaceMonitor: cfg.InterfaceMonitor,
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
	if cfg.FileDescriptor >= 0 {
		// Mobile path: insert DNS middleware between provider and tunnel.
		dnsMW := dns.NewMiddleware(dns.MiddlewareConfig{
			ProxyDNS: dns.NewProxyDNSClient(dns.ProxyDNSClientConfig{Dialer: tm}),
			DNSCache: dns.NewDNSCache(dns.DNSCacheConfig{}),
			Filter:   dns.NewFilter(dns.FilterConfig{}),
		})
		if err := prov.Start(ctx, &dnsHandler{inner: tunnel, dns: dnsMW}); err != nil {
			tm.Close()
			return e.fail(err)
		}
	} else {
		// Desktop path: tunnel.Start wires provider directly.
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
			log.Printf("engine: network monitor start failed: %v", err)
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
	e.wireUrl = ""
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
	if e.wireUrl != "" {
		info["wire_url"] = e.wireUrl
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

// buildK2ruleConfig resolves the k2rule configuration from Config fields.
// Priority: RuleConfig > RuleMode > URL query "rule=" > default "global".
func (e *Engine) buildK2ruleConfig(cfg Config) *k2rule.Config {
	// If caller provides a full RuleConfig, use it directly.
	if cfg.RuleConfig != nil {
		return cfg.RuleConfig
	}

	// Resolve rule mode: RuleMode > URL query param > default.
	ruleMode := cfg.RuleMode
	if ruleMode == "" {
		ruleMode = parseRuleFromURL(cfg.WireURL)
	}
	if ruleMode == "" {
		ruleMode = "global"
	}

	ruleCfg := &k2rule.Config{
		CacheDir: cfg.DataDir,
		IsGlobal: ruleMode == "global",
	}
	if ruleCfg.IsGlobal {
		ruleCfg.GlobalTarget = k2rule.TargetProxy
	}
	return ruleCfg
}

// parseRuleFromURL extracts the "rule" query parameter from a URL string.
// Returns empty string if the URL is invalid or has no rule parameter.
func parseRuleFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Query().Get("rule")
}

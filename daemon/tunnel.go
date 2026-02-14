package daemon

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/netip"

	"github.com/kaitu-io/k2rule"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/core"
	"github.com/kaitu-io/k2/directdial"
	"github.com/kaitu-io/k2/provider"
	"github.com/kaitu-io/k2/wire"
)

// BuildTunnel creates the full tunnel stack from a client config.
// Returns a started ClientTunnel ready for traffic.
func BuildTunnel(ctx context.Context, cfg *config.ClientConfig) (*core.ClientTunnel, error) {
	// 0. Initialize k2rule routing engine.
	ruleCfg := &k2rule.Config{
		RuleURL:  cfg.Rule.RuleURL,
		GeoIPURL: cfg.Rule.GeoIPURL,
		PornURL:  cfg.Rule.PornURL,
		CacheDir: cfg.Rule.CacheDir,
		IsGlobal: cfg.Rule.Global,
	}
	if cfg.Rule.Global {
		ruleCfg.GlobalTarget = k2rule.TargetProxy
	}
	if err := k2rule.Init(ruleCfg); err != nil {
		log.Printf("k2rule init: %v (continuing with defaults)", err)
	}

	// 1. Parse wire URL → build transports.
	wireCfg, err := wire.ParseURL(cfg.Server)
	if err != nil {
		return nil, fmt.Errorf("parse wire URL: %w", err)
	}

	// Prefer IPv6 when both server and client support it.
	if wireCfg.IPv6 != "" && wire.HasIPv6() {
		log.Printf("tunnel: IPv6 available, using %s", wireCfg.IPv6)
		wireCfg.Host = wireCfg.IPv6
	}

	// Create DirectDialer for physical interface binding (bypasses TUN).
	dd := directdial.New()
	if dd.InterfaceName() != "" {
		log.Printf("tunnel: direct dialer bound to %s", dd.InterfaceName())
	}

	quicClient := wire.NewQUICClient(wireCfg, nil)
	quicClient.SetDirectDialer(dd)
	tcpwsClient := wire.NewTCPWSClient(wireCfg, nil)
	tcpwsClient.SetDirectDialer(dd)
	tm := wire.NewTransportManager()
	switch wireCfg.Transport {
	case "quic":
		tm.SetQUIC(quicClient)
		log.Println("tunnel: transport forced to QUIC only")
	case "tcpws":
		tm.SetTCPWS(tcpwsClient)
		log.Println("tunnel: transport forced to TCP-WS only")
	default:
		tm.SetQUIC(quicClient)
		tm.SetTCPWS(tcpwsClient)
	}

	// 2. Build router.
	router := core.NewRouter()

	// 3. Parse DNS server IPs + server IPs for route exclusion.
	var excludeAddrs []netip.Addr
	for _, s := range cfg.DNS.Direct {
		host, _, err := splitHostPort(s)
		if err != nil {
			continue
		}
		if addr, err := netip.ParseAddr(host); err == nil {
			excludeAddrs = append(excludeAddrs, addr)
		}
	}

	// Resolve server host to IPs for route exclusion (belt + suspenders with DirectDialer).
	serverHost := wireCfg.Host
	if ips, err := net.LookupHost(serverHost); err == nil {
		for _, ip := range ips {
			if addr, err := netip.ParseAddr(ip); err == nil {
				excludeAddrs = append(excludeAddrs, addr)
			}
		}
	}

	// 4. Create provider.
	var prov provider.Provider
	switch cfg.Mode {
	case "proxy":
		prov = provider.NewProxyProvider(provider.ProviderConfig{
			Mode:        "proxy",
			ProxyListen: cfg.Proxy.Listen,
		})
	default: // "tun"
		prov = provider.NewTUNProvider(provider.ProviderConfig{
			Mode:       "tun",
			DNSServers: excludeAddrs,
		})
	}

	// 5. Create and start tunnel.
	tunnel := core.NewClientTunnel(core.ClientTunnelConfig{
		Provider:     prov,
		Dialer:       tm,
		Router:       router,
		DirectDialer: dd,
	})

	if err := tunnel.Start(ctx); err != nil {
		tunnel.Close()
		return nil, fmt.Errorf("start tunnel: %w", err)
	}

	return tunnel, nil
}

func splitHostPort(hostport string) (string, string, error) {
	for i := len(hostport) - 1; i >= 0; i-- {
		if hostport[i] == ':' {
			return hostport[:i], hostport[i+1:], nil
		}
	}
	return hostport, "", fmt.Errorf("missing port in %q", hostport)
}

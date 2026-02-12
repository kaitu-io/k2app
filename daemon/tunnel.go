package daemon

import (
	"context"
	"fmt"
	"log"
	"net/netip"

	"github.com/kaitu-io/k2rule"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/core"
	"github.com/kaitu-io/k2/core/dns"
	"github.com/kaitu-io/k2/provider"
	"github.com/kaitu-io/k2/wire"
)

// BuildTunnel creates the full tunnel stack from a client config.
// Returns a started ClientTunnel ready for traffic.
func BuildTunnel(ctx context.Context, cfg *config.ClientConfig) (*core.ClientTunnel, error) {
	// 0. Initialize k2rule routing engine.
	ruleCfg := &k2rule.Config{
		RuleURL:   cfg.Rule.RuleURL,
		RuleFile:  cfg.Rule.RuleFile,
		GeoIPURL:  cfg.Rule.GeoIPURL,
		GeoIPFile: cfg.Rule.GeoIPFile,
		PornURL:   cfg.Rule.PornURL,
		PornFile:  cfg.Rule.PornFile,
		CacheDir:  cfg.Rule.CacheDir,
		IsGlobal:  cfg.Rule.Global,
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

	quicClient := wire.NewQUICClient(wireCfg, nil)
	tcpwsClient := wire.NewTCPWSClient(wireCfg, nil)
	tm := wire.NewTransportManager()
	tm.SetQUIC(quicClient)
	tm.SetTCPWS(tcpwsClient)

	// 2. Build DNS IP cache + router.
	ipCache := dns.NewDynamicIPCache()
	router := core.NewRouter(ipCache)

	// 3. Parse DNS server IPs for route exclusion.
	var dnsAddrs []netip.Addr
	for _, s := range cfg.DNS.Direct {
		host, _, err := splitHostPort(s)
		if err != nil {
			continue
		}
		if addr, err := netip.ParseAddr(host); err == nil {
			dnsAddrs = append(dnsAddrs, addr)
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
			DNSServers: dnsAddrs,
		})
	}

	// 5. Create and start tunnel.
	tunnel := core.NewClientTunnel(core.ClientTunnelConfig{
		Provider: prov,
		Dialer:   tm,
		Router:   router,
		IPCache:  ipCache,
	})

	if err := tunnel.Start(ctx); err != nil {
		tm.Close()
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

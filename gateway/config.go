//go:build linux

package gateway

import (
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/engine"
	"github.com/kaitu-io/k2/provider"
	"github.com/kaitu-io/k2/rule"
	"gopkg.in/yaml.v3"
)

type GatewayConfig struct {
	config.ClientConfig `yaml:",inline"`
	ListenPort          int      `yaml:"listen_port"`
	LANSubnets          []string `yaml:"lan_subnets"`
	DNSRedirect         bool     `yaml:"dns_redirect"`
	DNSPort             int      `yaml:"dns_port" json:"dnsPort"`            // default: ListenPort + 1
	MaxLanClient        int      `yaml:"max_lan_client" json:"maxLanClient"` // from user Tier (0=no limit, -1=unlimited)
}

const DefaultGatewayAddr = "127.0.0.1:1779"

func LoadGateway(path string) (*GatewayConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg GatewayConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	SetGatewayDefaults(&cfg)
	return &cfg, nil
}

func GatewayFromURL(wireURL string) *GatewayConfig {
	cfg := &GatewayConfig{}
	cfg.Routes = []config.RouteConfig{{Via: wireURL, Match: rule.MatchConfig{All: true}}}
	SetGatewayDefaults(cfg)
	return cfg
}

func SetGatewayDefaults(cfg *GatewayConfig) {
	config.SetDefaults(&cfg.ClientConfig)
	if cfg.ListenPort == 0 {
		cfg.ListenPort = DefaultListenPort
	}
	// config.SetDefaults sets Listen to "127.0.0.1:1777" (daemon port).
	// Gateway uses 1779 to avoid collision.
	if cfg.Listen == "" || cfg.Listen == "127.0.0.1:1777" {
		cfg.Listen = DefaultGatewayAddr
	}
	if cfg.DNSPort == 0 {
		cfg.DNSPort = cfg.ListenPort + 1
	}
}

// engineConfig assembles the engine.Config the gateway hands to engine.Start.
//
// The first argument is the ClientConfig the engine should run against — for
// k2subs:// routes, this is the *resolved* (k2v5://) client built by
// subscription.ApplyResolved. The unresolved cfg.ClientConfig is preserved
// elsewhere for persistence + Resolve replay.
//
// outboundProvider is the gateway's subscription.Manager — wired into
// engine.Config.OutboundProvider so the engine's outboundReplaceLoop can
// hot-swap k2v5:// targets when one fails persistently.
func engineConfig(client *config.ClientConfig, prov provider.Provider, healthCb func(engine.HealthSnapshot), outboundProvider engine.OutboundProvider) engine.Config {
	return engine.Config{
		Client:           client,
		FileDescriptor:   -1,
		Provider:         prov,
		HealthCallback:   healthCb,
		WireMode:         "gateway",
		OutboundProvider: outboundProvider,
	}
}

// extractRouteHosts returns the host portion of every wire-outbound route
// (k2v5://, k2p://). Returns empty when no wire route exists or all are
// "direct"/"reject"/k2subs:// (k2subs is unresolved at this point and must
// not be queried for IP exclusion — its host would be the subscription API,
// not the actual wire server).
func extractRouteHosts(routes []config.RouteConfig) []string {
	var hosts []string
	for _, r := range routes {
		scheme := config.ViaScheme(r.Via)
		if scheme != config.ViaSchemeK2v5 && scheme != config.ViaSchemeK2p {
			continue
		}
		host := extractServerHost(r.Via)
		if host == "" {
			continue
		}
		hosts = append(hosts, host)
	}
	return hosts
}

func resolveExcludeIPs(hosts []string, directDNS []string) ([]string, error) {
	if len(hosts) == 0 {
		return nil, fmt.Errorf("gateway: no wire-outbound routes to derive exclude IPs from")
	}
	exclude := make([]string, 0, len(hosts)*2+len(directDNS)+8)
	for _, host := range hosts {
		serverIPs, err := net.LookupHost(host)
		if err != nil {
			return nil, fmt.Errorf("resolve server %s: %w", host, err)
		}
		exclude = append(exclude, serverIPs...)
	}

	for _, dns := range directDNS {
		h, _, _ := net.SplitHostPort(dns)
		if h == "" {
			h = dns
		}
		if ip := net.ParseIP(h); ip != nil {
			exclude = append(exclude, ip.String())
		}
	}

	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			exclude = append(exclude, ipNet.IP.String())
		}
	}
	return exclude, nil
}

func extractServerHost(serverURL string) string {
	s := serverURL
	if idx := strings.Index(s, "://"); idx >= 0 {
		s = s[idx+3:]
	}
	if idx := strings.Index(s, "@"); idx >= 0 {
		s = s[idx+1:]
	}
	if idx := strings.Index(s, "/"); idx >= 0 {
		s = s[:idx]
	}
	host, _, err := net.SplitHostPort(s)
	if err != nil {
		return s
	}
	return host
}

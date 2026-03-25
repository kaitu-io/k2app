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
	"gopkg.in/yaml.v3"
)

type GatewayConfig struct {
	config.ClientConfig `yaml:",inline"`
	ListenPort          int      `yaml:"listen_port"`
	LANSubnets          []string `yaml:"lan_subnets"`
	DNSRedirect         bool     `yaml:"dns_redirect"`
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
	cfg.Server = wireURL
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
}

func engineConfig(cfg *GatewayConfig, prov provider.Provider, healthCb func(engine.HealthSnapshot)) engine.Config {
	return engine.Config{
		Client:         &cfg.ClientConfig,
		FileDescriptor: -1,
		Provider:       prov,
		HealthCallback: healthCb,
		WireMode:       "gateway",
	}
}

func resolveExcludeIPs(serverURL string, directDNS []string) ([]string, error) {
	host := extractServerHost(serverURL)
	if host == "" {
		return nil, fmt.Errorf("gateway: cannot extract host from server URL")
	}
	serverIPs, err := net.LookupHost(host)
	if err != nil {
		return nil, fmt.Errorf("resolve server %s: %w", host, err)
	}
	exclude := make([]string, 0, len(serverIPs)+len(directDNS)+8)
	exclude = append(exclude, serverIPs...)

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

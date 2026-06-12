//go:build linux

package gateway

import (
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/engine"
	"github.com/kaitu-io/k2/k2p"
	"github.com/kaitu-io/k2/provider"
	"github.com/kaitu-io/k2/rule"
	"github.com/kaitu-io/k2/wire"
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

// PersistConfig writes the gateway config to the source config file k2r.yml,
// rooted at stateDir() (production: /etc/k2r, tests override via
// stateDirOverride). Both `k2r setup` (cmdSetup) and the panel set-credential
// action share this single path — there is exactly one source-of-truth file
// the `k2r run` restart path reads back (Cleanup #1: no second persistence
// path that the full restart would silently ignore).
func PersistConfig(cfg *GatewayConfig) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(stateDir(), 0755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(stateDir(), "k2r.yml"), data, 0600)
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
	// DNS redirect must be default-on to close the dnsmasq leak path. Without
	// it, locally-generated DNS (dnsmasq → ISP upstream) bypasses TPROXY,
	// since PREROUTING never sees router-originated packets. Opt-out is not
	// exposed at the YAML layer today; would require switching to *bool.
	cfg.DNSRedirect = true
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
		// DirectDialer stamps SO_MARK on direct sockets so the OUTPUT-chain
		// DNS REDIRECT exempts k2r's own DirectDNS queries before they
		// recurse through the DNS listener. See K2RFwmark and gateway/intercept_ipt.go
		// K2R_DNS_OUT chain.
		DirectDialer: &gatewayDirectDialer{},
	}
}

// dialTarget describes one wire-outbound dial destination derived from a
// route's Via URL. hintedIPs are populated from URL ip=/ip6= params; when
// non-empty the engine.resolveDialAddr path skips DNS, so the gateway must
// do the same to keep the TPROXY exclude set in sync with what wire actually
// connects to. host is used only as a DNS fallback when no IP hint is given.
type dialTarget struct {
	host      string
	hintedIPs []string
}

// extractDialTargets returns one dialTarget per wire-outbound route (k2v5://,
// k2p://). Delegates to wire.ParseURL / k2p.ParseURL — the same parsers the
// engine uses (engine/engine.go:1184, k2p/url.go:32) — so the gateway never
// diverges from the engine's view of "what address will I dial".
//
// k2subs:// is intentionally skipped: it is unresolved at this layer (the
// gateway resolves subscriptions before calling this, then passes the
// resolved k2v5 URLs in via Routes). "direct"/"reject" sentinels are also
// skipped. Returns nil when no wire route exists.
//
// A malformed wire URL is a hard error here — if wire can't dial it, gateway
// has nothing to exclude from TPROXY either.
func extractDialTargets(routes []config.RouteConfig) ([]dialTarget, error) {
	var targets []dialTarget
	for _, r := range routes {
		switch config.ViaScheme(r.Via) {
		case config.ViaSchemeK2v5:
			cfg, err := wire.ParseURL(r.Via)
			if err != nil {
				return nil, fmt.Errorf("gateway: parse k2v5 outbound: %w", err)
			}
			targets = append(targets, dialTargetFromWire(cfg))
		case config.ViaSchemeK2p:
			cfg, err := k2p.ParseURL(r.Via)
			if err != nil {
				return nil, fmt.Errorf("gateway: parse k2p outbound: %w", err)
			}
			targets = append(targets, dialTargetFromWire(cfg.WireCfg))
		}
	}
	return targets, nil
}

func dialTargetFromWire(cfg wire.Config) dialTarget {
	var ips []string
	if cfg.IPv4 != "" {
		ips = append(ips, cfg.IPv4)
	}
	if cfg.IPv6 != "" {
		ips = append(ips, cfg.IPv6)
	}
	return dialTarget{host: cfg.Host, hintedIPs: ips}
}

func resolveExcludeIPs(targets []dialTarget, directDNS []string) ([]string, error) {
	if len(targets) == 0 {
		return nil, fmt.Errorf("gateway: no wire-outbound routes to derive exclude IPs from")
	}
	exclude := make([]string, 0, len(targets)*2+len(directDNS)+8)
	for _, t := range targets {
		if len(t.hintedIPs) > 0 {
			// URL carries ip=/ip6= — engine.resolveDialAddr dials these
			// directly, no DNS round trip. Mirror that here so the exclude
			// set matches what wire actually connects to (and so the gateway
			// keeps working when the host's DNS is poisoned/blocked).
			exclude = append(exclude, t.hintedIPs...)
			continue
		}
		serverIPs, err := net.LookupHost(t.host)
		if err != nil {
			return nil, fmt.Errorf("resolve server %s: %w", t.host, err)
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

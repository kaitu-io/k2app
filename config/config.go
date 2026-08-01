package config

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/kaitu-io/k2/rule"
	"gopkg.in/yaml.v3"
)

// ClientConfig is the client-side configuration.
// Loaded by `k2 run -c` or parsed from `k2 up` URL, or unmarshalled from JSON.
type ClientConfig struct {
	Routes        []RouteConfig   `yaml:"routes"     json:"routes"`
	Mode          string          `yaml:"mode"       json:"mode"` // "tun" or "proxy", default "tun"
	Tun           TunConfig       `yaml:"tun"        json:"tun"`
	Proxy         ProxyConfig     `yaml:"proxy"      json:"proxy"`
	DNS           DNSConfig       `yaml:"dns"        json:"dns"`
	Log           LogConfig       `yaml:"log"        json:"log"`
	Listen        string          `yaml:"listen"     json:"listen"`                                 // daemon HTTP listen address, default "127.0.0.1:1777"
	PIDFile       string          `yaml:"pid_file"   json:"pid_file"`                               // path to PID file (empty = no PID file)
	BlockQUIC     bool            `yaml:"block_quic" json:"block_quic"`                             // block browser QUIC via ICMP reject (default: false = allow through DATAGRAM)
	CacheDir      string          `yaml:"cache_dir"  json:"cache_dir"`                              // cache directory for rule bundles ("" = platform default)
	RuleSources   []string        `yaml:"rule_sources,omitempty" json:"rule_sources,omitempty"`     // override default k2b download sources
	Telemetry     TelemetryConfig `yaml:"telemetry,omitempty" json:"telemetry,omitempty"`           // anonymous usage telemetry (Phase 1: rule miss)
	ClientVersion string          `yaml:"client_version,omitempty" json:"client_version,omitempty"` // client app version, forwarded to the node via the metadata stream for per-device attribution. Set by the webapp; "" = legacy/unknown.

	// Derived fields (not serialized to YAML/JSON).
	Server string `yaml:"-" json:"-"` // first k2v5:// URL from routes

}

// TelemetryConfig groups anonymous telemetry sub-features. Zero value means
// "all telemetry off" — the Phase 1 default.
type TelemetryConfig struct {
	RuleMiss RuleMissConfig `yaml:"rule_miss,omitempty" json:"rule_miss,omitempty"`
}

// RuleMissConfig controls rule-miss (domain-fallback) telemetry.
//
// Phase 1 is "dark instrumentation": when Enabled is false (the default),
// the engine does not construct a reporter, the rule engine holds a nil
// MissReporter pointer, and the match hot path short-circuits with zero
// allocations. Flipping Enabled=true wires the full ring buffer → hash →
// batcher → HTTP POST pipeline described in k2/engine/miss_reporter.go.
type RuleMissConfig struct {
	Enabled       bool   `yaml:"enabled" json:"enabled"`                                   // default false — MUST stay off by default
	Endpoint      string `yaml:"endpoint,omitempty" json:"endpoint,omitempty"`             // e.g. "https://center.kaitu.io/api/telemetry/rule_miss"
	Country       string `yaml:"country,omitempty" json:"country,omitempty"`               // ISO 3166-1 alpha-2, or "XX" when unknown
	ClientVersion string `yaml:"client_version,omitempty" json:"client_version,omitempty"` // e.g. "0.4.3"
}

// RouteConfig defines a single routing rule: traffic matching Match is sent via Via.
//
// Via is one of:
//   - ""           — direct (same as "direct")
//   - "direct"     — bypass the tunnel, dial target from the local interface
//   - "reject"     — drop the connection
//   - "k2v5://..." — k2v5 wire outbound URL
//   - "k2p://..."  — k2p peer-to-peer outbound URL
//   - "k2subs://..." — subscription URL (resolves to k2v5)
//
// Reserved (not yet implemented, validation rejects these):
//   - "device://<deviceId>"           — route via another device on the same user account
//     (reserved for k2p home / bring-your-own-relay feature)
//   - "auto://<pool>[/<country>]"     — route via a Center-selected node from a named pool
//     (reserved for server-driven exit selection)
//
// When implementing reserved schemes, add them to ViaSchemeReserved in the scheme
// constants below and wire up the outbound in engine.setupOutboundTransport.
type RouteConfig struct {
	Via   string           `yaml:"via"   json:"via"`
	Match rule.MatchConfig `yaml:"match" json:"match"`
}

// Via scheme constants. Use with ViaScheme() to classify a route's Via field.
const (
	// Sentinel values (no scheme).
	ViaDirect = "direct"
	ViaReject = "reject"

	// Implemented URL schemes (wire outbounds).
	ViaSchemeK2v5   = "k2v5"
	ViaSchemeK2p    = "k2p"
	ViaSchemeK2subs = "k2subs"

	// Reserved URL schemes — defined for forward-compat but not yet implemented.
	// ValidateClient rejects routes using these schemes; buildRouteEntries skips them
	// defensively (treats as direct + WARN log) in case validation is bypassed.
	ViaSchemeDevice = "device" // route via another device on the same user account (k2p home)
	ViaSchemeAuto   = "auto"   // route via a Center-selected node from a named pool
)

// ViaScheme returns the URL scheme portion of a Via string, or an empty string
// for sentinel values ("direct", "reject", "") and malformed inputs.
//
// Examples:
//
//	ViaScheme("k2v5://host:443")       → "k2v5"
//	ViaScheme("device://abc123")       → "device"
//	ViaScheme("direct")                → ""
//	ViaScheme("")                      → ""
func ViaScheme(via string) string {
	if i := strings.Index(via, "://"); i > 0 {
		return via[:i]
	}
	return ""
}

// IsReservedVia reports whether via uses a scheme that is reserved for a
// future feature and not yet implemented. Reserved schemes are rejected by
// ValidateClient so they never reach the engine assembly path.
func IsReservedVia(via string) bool {
	scheme := ViaScheme(via)
	return scheme == ViaSchemeDevice || scheme == ViaSchemeAuto
}

// IsAllProxy returns true if any route proxies all traffic through a wire outbound (k2v5, k2p, or k2subs).
func (c *ClientConfig) IsAllProxy() bool {
	for _, r := range c.Routes {
		if r.Match.All && IsOutboundURL(r.Via) {
			return true
		}
	}
	return false
}

// IsOutboundURL returns true if via is a wire outbound URL (k2v5, k2p, or k2subs), not "direct" or "reject".
// Reserved schemes (device://, auto://) return false — they are not wire outbounds.
func IsOutboundURL(via string) bool {
	scheme := ViaScheme(via)
	return scheme == ViaSchemeK2v5 || scheme == ViaSchemeK2p || scheme == ViaSchemeK2subs
}

// TunConfig contains TUN device configuration.
type TunConfig struct {
	IPv4 string `yaml:"ipv4" json:"ipv4"` // CIDR, e.g. "10.0.0.2/24"
	IPv6 string `yaml:"ipv6" json:"ipv6"` // CIDR, e.g. "fd00::2/128"
}

// Default TUN addresses (desktop: RFC 2544 test range).
// Mobile builds override these via init() in defaults_mobile.go.
var (
	DefaultTunIPv4 = "198.18.0.7/15"
	DefaultTunIPv6 = "fdfe:dcba:9876::7/64"
)

// ProxyConfig contains proxy mode configuration.
type ProxyConfig struct {
	Listen string `yaml:"listen" json:"listen"` // default "127.0.0.1:1080"
}

// ServerConfig is the server-side configuration.
// Loaded by `k2 run -s`.
type ServerConfig struct {
	Listen      string            `yaml:"listen"`
	Host        string            `yaml:"host"` // external hostname/IP for connect URL (auto-detected if empty)
	TLS         TLSConfig         `yaml:"tls"`
	ECH         ECHConfig         `yaml:"ech"`
	CertDir     string            `yaml:"cert_dir"`     // directory for auto-generated dual keypair (RSA+EC)
	LocalRoutes map[string]string `yaml:"local_routes"` // SNI -> target address for non-ECH TCP relay
	// ControlPlaneRoutes maps an ECH-decrypted INNER SNI to a Center origin
	// (host:port). When a k2v5 TLS connection's inner SNI matches, the node
	// HTTP-reverse-proxies it to the origin instead of treating it as a tunnel.
	// Used by the antiblock control-plane relay (Phase 1). Empty = built-in
	// default (relays the single permanent inner SNI k2.52j.me to the Center
	// address); set explicitly only to override that default.
	ControlPlaneRoutes map[string]string `yaml:"control_plane_routes"`
	PCC                string            `yaml:"pcc"`
	Rate               int64             `yaml:"rate"`
	Auth               AuthConfig        `yaml:"auth"`
	Log                LogConfig         `yaml:"log"`
	PIDFile            string            `yaml:"pid_file"`  // path to PID file (empty = no PID file)
	HopStart           int               `yaml:"hop_start"` // hop port range start (0 = no hop)
	HopEnd             int               `yaml:"hop_end"`   // hop port range end (0 = no hop)

	// UsageAPIListen is the loopback usage API addr (Plan 3-k2s, A3).
	// Default 127.0.0.1:9099; empty string disables the API.
	// Overridable at runtime via K2_USAGE_API_LISTEN.
	UsageAPIListen string `yaml:"usage_api_listen"`

	// Usage reporter (Plan 3-k2s Phase B, Option D). The reporter runs the
	// per-node traffic-cutoff loop in-process; it is gated on both
	// UsageReportURL and NodeSecret being non-empty (shared-pool nodes set
	// neither → reporter never starts → zero behavior change).
	UsageReportURL string `yaml:"usage_report_url"` // Center base URL for usage reporting; "" disables. env K2_USAGE_REPORT_URL
	NodeSecret     string `yaml:"-"`                // K2_NODE_SECRET (Basic auth pw). Secret — never serialized.
	NodeIPv4       string `yaml:"-"`                // node public IPv4 (Basic auth user); "" => ProbePublicIP fallback at runtime

	// SlackWebhookURL enables node-level auth-fault alerting (spec §6.4):
	// sustained `DIAG: auth-center-unreachable`, and node misconfiguration
	// that makes device auth impossible. Empty (the default) disables
	// alerting entirely — no goroutine, no HTTP, no behavior change.
	// Secret — never serialized, never logged. env K2_SLACK_WEBHOOK_URL.
	SlackWebhookURL string `yaml:"-"`
	// NodeName labels alerts so an operator can tell one node's incident from
	// a fleet-wide one. Falls back to the node's IPv4, then "unknown-node".
	// env K2_NODE_NAME.
	NodeName string `yaml:"-"`

	// NodeCutoffReport gates the LEGACY in-process node-level usage reporter
	// (server/usage_reporter.go). Default false: against the current
	// pure-recorder Center that endpoint returns no verdict, and the legacy
	// loop would read that as "stop" and also stomp NodeUsage quota fields.
	// Only enable against a Center that actually serves verdicts.
	// env K2_NODE_CUTOFF_REPORT ("1"/"true").
	NodeCutoffReport bool `yaml:"node_cutoff_report"`
}

// DNSConfig contains DNS resolver configuration.
type DNSConfig struct {
	Direct []string `yaml:"direct" json:"direct"`
	Proxy  []string `yaml:"proxy"  json:"proxy"`
}

// TLSConfig contains TLS certificate paths.
type TLSConfig struct {
	Cert string `yaml:"cert"`
	Key  string `yaml:"key"`
}

// ECHConfig contains Encrypted Client Hello configuration.
type ECHConfig struct {
	KeyPath    string `yaml:"key_path"`    // legacy: single file with key+config
	PublicName string `yaml:"public_name"` // legacy: explicit public name
	Provider   string `yaml:"provider"`    // "local" (default) — ECH template provider
}

// AuthConfig contains server auth configuration.
// Validators are checked in order: users_file → remote_url → accept-all.
type AuthConfig struct {
	UsersFile   string        `yaml:"users_file"`   // path to users file (udid:token per line)
	RemoteURL   string        `yaml:"remote_url"`   // Center API auth URL
	CacheTTL    time.Duration `yaml:"cache_ttl"`    // TTL for cached successful auths; 0 → defaults to 15m in SetDefaults (not "no cache")
	EnforceAuth bool          `yaml:"enforce_auth"` // reject unauthenticated proxy streams on BOTH transports (QUIC + TCP-WS). Default false (permissive) during rollout — flip to true only once new clients (which send metadata) have propagated, else legacy clients are cut off. env K2_ENFORCE_AUTH
}

// LogConfig contains logging configuration.
type LogConfig struct {
	Level       string    `yaml:"level"  json:"level"`  // debug, info, warn, error
	Output      string    `yaml:"output" json:"output"` // stderr, stdout, or file path
	ExtraWriter io.Writer `yaml:"-" json:"-"`           // optional: additional output (e.g. os.Stderr for debug dual-output)
}

// LoadClient loads a client config from a YAML file.
func LoadClient(path string) (*ClientConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg ClientConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	SetDefaults(&cfg)
	return &cfg, nil
}

// LoadServer loads a server config from a YAML file.
func LoadServer(path string) (*ServerConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg ServerConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	setServerDefaults(&cfg)
	return &cfg, nil
}

// ClientFromURL creates a ClientConfig from a k2v5:// wire URL.
func ClientFromURL(wireURL string) *ClientConfig {
	cfg := &ClientConfig{
		Routes: []RouteConfig{
			{Via: wireURL, Match: rule.MatchConfig{All: true}},
		},
	}
	SetDefaults(cfg)
	return cfg
}

// ClientFromSubsURL creates a ClientConfig from a k2subs:// subscription URL.
// The URL is stored as-is in routes; resolution happens at engine start time.
func ClientFromSubsURL(subsURL string) *ClientConfig {
	cfg := &ClientConfig{
		Routes: []RouteConfig{
			{Via: subsURL, Match: rule.MatchConfig{All: true}},
		},
	}
	SetDefaults(cfg)
	return cfg
}

// SetDefaults fills in default values for any zero-valued fields in a ClientConfig.
func SetDefaults(cfg *ClientConfig) {
	// Derive Server from first k2v5:// route.
	if cfg.Server == "" {
		for _, r := range cfg.Routes {
			if strings.HasPrefix(r.Via, "k2v5://") {
				cfg.Server = r.Via
				break
			}
		}
	}

	if cfg.Listen == "" {
		cfg.Listen = "127.0.0.1:1777"
	}
	if cfg.Mode == "" {
		cfg.Mode = "tun"
	}
	if cfg.Proxy.Listen == "" {
		cfg.Proxy.Listen = "127.0.0.1:1080"
	}
	if len(cfg.DNS.Direct) == 0 {
		cfg.DNS.Direct = []string{
			"114.114.114.114:53", "114.114.115.115:53",
			"223.5.5.5:53", "223.6.6.6:53",
			"119.29.29.29:53", "119.28.28.28:53",
		}
	}
	if len(cfg.DNS.Proxy) == 0 {
		cfg.DNS.Proxy = []string{
			"8.8.8.8:53", "8.8.4.4:53",
			"1.1.1.1:53", "1.0.0.1:53",
			"9.9.9.9:53", "149.112.112.112:53",
		}
	}
	if cfg.Log.Level == "" {
		cfg.Log.Level = "info"
	}
	// Log.Output intentionally left empty when unset — SetupLogging()
	// interprets empty as DefaultLogPath(), which writes to the correct
	// platform log file. Overriding to "stderr" broke daemon logging
	// under service supervisors (launchd, Windows SCM) that discard stderr.
	if cfg.Tun.IPv4 == "" {
		cfg.Tun.IPv4 = DefaultTunIPv4
	}
	if cfg.Tun.IPv6 == "" {
		cfg.Tun.IPv6 = DefaultTunIPv6
	}
}

// defaultServerDir returns the platform-appropriate default directory for
// server state (certificates, ECH keys, auth, connect URL).
func defaultServerDir() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("ProgramData"), "k2s")
	}
	return "/etc/k2s"
}

// DefaultClientDir returns the platform-appropriate default directory for
// client daemon state (saved config, reconnect state).
func DefaultClientDir() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("ProgramData"), "k2")
	}
	return "/etc/k2"
}

// DefaultServerConfig creates a ServerConfig with all defaults applied.
// Used when k2s starts with no config file (zero-config mode).
func DefaultServerConfig() *ServerConfig {
	cfg := &ServerConfig{}
	setServerDefaults(cfg)
	return cfg
}

func setServerDefaults(cfg *ServerConfig) {
	if cfg.Listen == "" {
		cfg.Listen = ":443"
	}
	if cfg.CertDir == "" && cfg.TLS.Cert == "" {
		cfg.CertDir = defaultServerDir()
	}
	if cfg.ECH.Provider == "" {
		cfg.ECH.Provider = "local"
	}
	if cfg.Auth.CacheTTL == 0 {
		cfg.Auth.CacheTTL = 15 * time.Minute
	}
	if cfg.Log.Level == "" {
		cfg.Log.Level = "info"
	}
	if cfg.Log.Output == "" {
		cfg.Log.Output = "stderr"
	}
	if cfg.UsageAPIListen == "" {
		cfg.UsageAPIListen = "127.0.0.1:9099"
	}
	// Runtime env override wins (operators can repoint or disable via "").
	if v, ok := os.LookupEnv("K2_USAGE_API_LISTEN"); ok {
		cfg.UsageAPIListen = v
	}
	// Usage reporter env (Plan 3-k2s Phase B, Option D). Secret/IPv4 are env-only
	// (yaml:"-") so they never land in a config file.
	if v, ok := os.LookupEnv("K2_USAGE_REPORT_URL"); ok {
		cfg.UsageReportURL = v
	}
	if v, ok := os.LookupEnv("K2_NODE_SECRET"); ok {
		cfg.NodeSecret = v
	}
	if v, ok := os.LookupEnv("K2_NODE_IPV4"); ok {
		cfg.NodeIPv4 = v
	}
	if v := os.Getenv("K2_NODE_CUTOFF_REPORT"); v == "1" || v == "true" {
		cfg.NodeCutoffReport = true
	}
	if v := os.Getenv("K2_ENFORCE_AUTH"); v == "1" || v == "true" {
		cfg.Auth.EnforceAuth = true
	}
	// Node-level alerting (spec §6.4). Unset => alerting off, which is the
	// documented default: a node with no webhook must behave exactly as it did
	// before alerting existed.
	if v, ok := os.LookupEnv("K2_SLACK_WEBHOOK_URL"); ok {
		cfg.SlackWebhookURL = v
	}
	if v, ok := os.LookupEnv("K2_NODE_NAME"); ok {
		cfg.NodeName = v
	}
}

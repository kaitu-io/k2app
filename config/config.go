package config

import (
	"os"
	"path/filepath"
	"runtime"
	"time"

	"gopkg.in/yaml.v3"
)

// ClientConfig is the client-side configuration.
// Loaded by `k2 run -c` or parsed from `k2 up` URL, or unmarshalled from JSON.
type ClientConfig struct {
	Listen  string      `yaml:"listen"   json:"listen"`   // daemon HTTP listen address, default "127.0.0.1:1777"
	Server  string      `yaml:"server"   json:"server"`   // k2v5:// URL
	Mode    string      `yaml:"mode"     json:"mode"`     // "tun" or "proxy", default "tun"
	Tun     TunConfig   `yaml:"tun"      json:"tun"`
	Proxy   ProxyConfig `yaml:"proxy"    json:"proxy"`
	DNS     DNSConfig   `yaml:"dns"      json:"dns"`
	Rule    RuleConfig  `yaml:"rule"     json:"rule"`
	Log     LogConfig   `yaml:"log"      json:"log"`
	PIDFile string      `yaml:"pid_file" json:"pid_file"` // path to PID file (empty = no PID file)
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
	Host        string            `yaml:"host"`         // external hostname/IP for connect URL (auto-detected if empty)
	TLS         TLSConfig         `yaml:"tls"`
	ECH         ECHConfig         `yaml:"ech"`
	CertDir     string            `yaml:"cert_dir"`     // directory for auto-generated dual keypair (RSA+EC)
	LocalRoutes map[string]string `yaml:"local_routes"` // SNI -> target address for non-ECH TCP relay
	PCC         string            `yaml:"pcc"`
	Rate        int64             `yaml:"rate"`
	Auth        AuthConfig        `yaml:"auth"`
	Log         LogConfig         `yaml:"log"`
	PIDFile     string            `yaml:"pid_file"` // path to PID file (empty = no PID file)
	HopStart    int               `yaml:"hop_start"` // hop port range start (0 = no hop)
	HopEnd      int               `yaml:"hop_end"`   // hop port range end (0 = no hop)
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
	KeyPath    string `yaml:"key_path"`     // legacy: single file with key+config
	PublicName string `yaml:"public_name"`  // legacy: explicit public name
	Provider   string `yaml:"provider"`     // "local" (default) — ECH template provider
}

// AuthConfig contains server auth configuration.
// Validators are checked in order: users_file → remote_url → accept-all.
type AuthConfig struct {
	UsersFile string        `yaml:"users_file"` // path to users file (udid:token per line)
	RemoteURL string        `yaml:"remote_url"` // Center API auth URL
	CacheTTL  time.Duration `yaml:"cache_ttl"`
}

// RuleConfig contains k2rule routing rule configuration.
type RuleConfig struct {
	RuleURL  string `yaml:"rule_url"  json:"rule_url"`  // remote rule file URL ("" = CDN default)
	GeoIPURL string `yaml:"geoip_url" json:"geoip_url"` // remote GeoIP .mmdb URL ("" = CDN default)
	Antiporn bool   `yaml:"antiporn"  json:"antiporn"`  // enable anti-porn detection (default: false)
	PornURL  string `yaml:"porn_url"  json:"porn_url"`  // remote porn list URL ("" = CDN default, only when antiporn=true)
	CacheDir string `yaml:"cache_dir" json:"cache_dir"` // cache directory ("" = paths.CacheDir()/k2rule)
	Global   bool   `yaml:"global"    json:"global"`    // true = global proxy mode (ignore rules)
}

// LogConfig contains logging configuration.
type LogConfig struct {
	Level  string `yaml:"level"  json:"level"`  // debug, info, warn, error
	Output string `yaml:"output" json:"output"` // stderr, stdout, or file path
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
	cfg := &ClientConfig{Server: wireURL}
	SetDefaults(cfg)
	return cfg
}

// SetDefaults fills in default values for any zero-valued fields in a ClientConfig.
func SetDefaults(cfg *ClientConfig) {
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
		cfg.DNS.Direct = []string{"114.114.114.114:53", "223.5.5.5:53"}
	}
	if len(cfg.DNS.Proxy) == 0 {
		cfg.DNS.Proxy = []string{"8.8.8.8:53", "1.1.1.1:53"}
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
		cfg.Auth.CacheTTL = 5 * time.Minute
	}
	if cfg.Log.Level == "" {
		cfg.Log.Level = "info"
	}
	if cfg.Log.Output == "" {
		cfg.Log.Output = "stderr"
	}
}

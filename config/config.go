package config

import (
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// ClientConfig is the client-side configuration.
// Loaded by `k2 run -c` or parsed from `k2 up` URL.
type ClientConfig struct {
	Server string       `yaml:"server"` // k2v5:// URL
	Mode   string       `yaml:"mode"`   // "tun" or "proxy", default "tun"
	Proxy  ProxyConfig  `yaml:"proxy"`
	DNS  DNSConfig  `yaml:"dns"`
	Rule RuleConfig `yaml:"rule"`
	Log    LogConfig    `yaml:"log"`
}

// ProxyConfig contains proxy mode configuration.
type ProxyConfig struct {
	Listen string `yaml:"listen"` // default "127.0.0.1:1080"
}

// ServerConfig is the server-side configuration.
// Loaded by `k2 run -s`.
type ServerConfig struct {
	Listen string    `yaml:"listen"`
	TLS    TLSConfig `yaml:"tls"`
	PCC    string    `yaml:"pcc"`
	Rate   int64     `yaml:"rate"`
	Auth   AuthConfig    `yaml:"auth"`
	Log    LogConfig     `yaml:"log"`
}

// DNSConfig contains DNS resolver configuration.
type DNSConfig struct {
	Direct []string `yaml:"direct"`
	Proxy  []string `yaml:"proxy"`
}

// TLSConfig contains TLS certificate paths.
type TLSConfig struct {
	Cert string `yaml:"cert"`
	Key  string `yaml:"key"`
}

// AuthConfig contains server auth configuration.
type AuthConfig struct {
	RemoteURL string        `yaml:"remote_url"`
	CacheTTL  time.Duration `yaml:"cache_ttl"`
}

// RuleConfig contains k2rule routing rule configuration.
type RuleConfig struct {
	RuleURL  string `yaml:"rule_url"`  // remote rule file URL ("" = CDN default)
	GeoIPURL string `yaml:"geoip_url"` // remote GeoIP .mmdb URL ("" = CDN default)
	PornURL  string `yaml:"porn_url"`  // remote porn list URL ("" = CDN default)
	CacheDir string `yaml:"cache_dir"` // cache directory ("" = ~/.cache/k2rule/)
	Global   bool   `yaml:"global"`    // true = global proxy mode (ignore rules)
}

// LogConfig contains logging configuration.
type LogConfig struct {
	Level  string `yaml:"level"`  // debug, info, warn, error
	Output string `yaml:"output"` // stderr, stdout, or file path
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

	setClientDefaults(&cfg)
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
	setClientDefaults(cfg)
	return cfg
}

func setClientDefaults(cfg *ClientConfig) {
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
	if cfg.Log.Output == "" {
		cfg.Log.Output = "stderr"
	}
}

func setServerDefaults(cfg *ServerConfig) {
	if cfg.Listen == "" {
		cfg.Listen = ":443"
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

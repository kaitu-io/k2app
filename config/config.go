package config

import (
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config represents the k2tunnel configuration
type Config struct {
	Log    LogConfig    `yaml:"log"`
	Wire   any          `yaml:"wire"`   // string (client URL) or ServerConfig map (server)
	Router RouterConfig `yaml:"router"`
}

// LogConfig contains logging configuration
type LogConfig struct {
	Level  string `yaml:"level"`  // debug, info, warn, error
	Output string `yaml:"output"` // stderr, stdout, or file path
}

// ServerWireConfig contains server-side wire configuration
type ServerWireConfig struct {
	Protocol string `yaml:"protocol"` // k2v5, http3
	Listen   string `yaml:"listen"`

	// Auth
	Auth AuthConfig `yaml:"auth"`

	// QUIC+PCC
	QUIC QUICConfig `yaml:"quic"`

	// VLESS Reality
	Reality RealityServerConfig `yaml:"reality"`
}

// AuthConfig contains server auth configuration
type AuthConfig struct {
	RemoteURL string        `yaml:"remote_url"`
	CacheTTL  time.Duration `yaml:"cache_ttl"`
}

// QUICConfig contains QUIC transport configuration
type QUICConfig struct {
	Cert    string `yaml:"cert"`
	Key     string `yaml:"key"`
	PCCMode string `yaml:"pcc"`
	MaxRate int64  `yaml:"rate"`
}

// RealityServerConfig contains Reality TLS server configuration
type RealityServerConfig struct {
	PrivateKey string   `yaml:"private_key"`
	ShortID    []string `yaml:"short_id"`
	Handshake  struct {
		Server string `yaml:"server"`
		Port   int    `yaml:"port"`
	} `yaml:"handshake"`
}

// RouterConfig contains routing configuration
type RouterConfig struct {
	Default string `yaml:"default"` // "direct", "proxy", or "reject"
}

// WireURL returns the wire URL string if config is client-mode
func (c *Config) WireURL() string {
	if s, ok := c.Wire.(string); ok {
		return s
	}
	return ""
}

// WireServerConfig returns the ServerWireConfig if config is server-mode
func (c *Config) WireServerConfig() *ServerWireConfig {
	m, ok := c.Wire.(map[string]any)
	if !ok {
		return nil
	}

	cfg := &ServerWireConfig{}
	if v, ok := m["protocol"].(string); ok {
		cfg.Protocol = v
	}
	if v, ok := m["listen"].(string); ok {
		cfg.Listen = v
	}

	// Auth
	if auth, ok := m["auth"].(map[string]any); ok {
		if v, ok := auth["remote_url"].(string); ok {
			cfg.Auth.RemoteURL = v
		}
		if v, ok := auth["cache_ttl"].(string); ok {
			if d, err := time.ParseDuration(v); err == nil {
				cfg.Auth.CacheTTL = d
			}
		}
	}

	// QUIC
	if quic, ok := m["quic"].(map[string]any); ok {
		if v, ok := quic["cert"].(string); ok {
			cfg.QUIC.Cert = v
		}
		if v, ok := quic["key"].(string); ok {
			cfg.QUIC.Key = v
		}
		if v, ok := quic["pcc"].(string); ok {
			cfg.QUIC.PCCMode = v
		}
		if v, ok := quic["rate"].(int); ok {
			cfg.QUIC.MaxRate = int64(v)
		}
	}

	// Reality
	if reality, ok := m["reality"].(map[string]any); ok {
		if v, ok := reality["private_key"].(string); ok {
			cfg.Reality.PrivateKey = v
		}
		if v, ok := reality["short_id"].([]any); ok {
			for _, s := range v {
				if str, ok := s.(string); ok {
					cfg.Reality.ShortID = append(cfg.Reality.ShortID, str)
				}
			}
		}
		if hs, ok := reality["handshake"].(map[string]any); ok {
			if v, ok := hs["server"].(string); ok {
				cfg.Reality.Handshake.Server = v
			}
			if v, ok := hs["port"].(int); ok {
				cfg.Reality.Handshake.Port = v
			}
		}
	}

	return cfg
}

// Load loads configuration from a YAML file
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	// Set defaults
	if cfg.Log.Level == "" {
		cfg.Log.Level = "info"
	}
	if cfg.Log.Output == "" {
		cfg.Log.Output = "stderr"
	}
	if cfg.Router.Default == "" {
		cfg.Router.Default = "proxy"
	}

	return &cfg, nil
}

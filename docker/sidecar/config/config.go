package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/creasty/defaults"
	"github.com/kaitu-io/k2-sidecar/sidecar"
	"gopkg.in/yaml.v2"
)

// K2CenterConfig holds K2 Center configuration
type K2CenterConfig struct {
	Enabled bool   `yaml:"enabled" default:"true"`
	BaseURL string `yaml:"base_url" default:"https://k2.52j.me"`
	Timeout string `yaml:"timeout" default:"10s"`
	Secret  string `yaml:"secret"` // Node authentication secret (required)

	// Metrics reporting (used by sidecar)
	ReportInterval   string `yaml:"report_interval" default:"120s"`
	BillingStartDate string `yaml:"billing_start_date"`
	TrafficLimitGB   int64  `yaml:"traffic_limit_gb"`
}

// TunnelSectionConfig holds tunnel-specific configuration
type TunnelSectionConfig struct {
	Enabled      bool   `yaml:"enabled" default:"true"`
	Domain       string `yaml:"domain"`
	Port         int    `yaml:"port" default:"443"`
	HopPortStart int    `yaml:"hop_port_start" default:"0"` // Port hopping start (0 = disabled)
	HopPortEnd   int    `yaml:"hop_port_end" default:"0"`   // Port hopping end (0 = disabled)
	CertFile     string `yaml:"cert_file"`                  // Custom certificate file path (optional)
	KeyFile      string `yaml:"key_file"`                   // Custom key file path (optional)
}

// NodeSectionConfig holds node identity configuration
type NodeSectionConfig struct {
	IPv4    string `yaml:"ipv4"`                 // Node IPv4 address (optional, auto-detected)
	IPv6    string `yaml:"ipv6"`                 // Node IPv6 address (optional, auto-detected)
	Name    string `yaml:"name"`                 // Node display name (optional, defaults to IPv4)
	Country string `yaml:"country" default:"US"` // Country code (optional, auto-detected)
	Region  string `yaml:"region"`               // Server region/datacenter (optional, defaults to country)
}

// RelaySectionConfig holds relay-specific configuration
type RelaySectionConfig struct {
	Enabled bool `yaml:"enabled" default:"false"`
}

// OCTunnelConfig holds OpenConnect tunnel settings (for sidecar registration)
type OCTunnelConfig struct {
	Enabled      bool   `yaml:"enabled" default:"false"`
	Domain       string `yaml:"domain"`
	Port         int    `yaml:"port" default:"443"`        // Registration port (for SNI routing, always 443)
	ListenPort   int    `yaml:"listen_port" default:"443"` // Container listening port (exposed via port mapping)
	RadiusServer string `yaml:"radius_server" default:"k2-sidecar"`
}

// ECHSectionConfig holds ECH (Encrypted Client Hello) configuration
type ECHSectionConfig struct {
	Enabled  bool   `yaml:"enabled" default:"false"`
	KeysFile string `yaml:"keys_file" default:"/etc/kaitu/ech_keys.yaml"` // ECH keys file (managed by sidecar)
}

// ECHKeysFile represents the structure of the ECH keys YAML file
// This file is written by sidecar and read by k2-slave
type ECHKeysFile struct {
	Keys []ECHKeyConfig `yaml:"keys"`
}

// ECHKeyConfig represents a single ECH key configuration in the keys file
type ECHKeyConfig struct {
	ConfigID   uint8  `yaml:"config_id"`   // ECH Config ID (1-255)
	PrivateKey string `yaml:"private_key"` // Base64-encoded X25519 private key
	PublicKey  string `yaml:"public_key"`  // Base64-encoded X25519 public key
	KEMId      uint16 `yaml:"kem_id"`      // KEM algorithm ID (0x0020 = X25519)
	KDFId      uint16 `yaml:"kdf_id"`      // KDF algorithm ID (0x0001 = HKDF-SHA256)
	AEADId     uint16 `yaml:"aead_id"`     // AEAD algorithm ID (0x0001 = AES-128-GCM)
	Status     string `yaml:"status"`      // Key status: "active", "grace_period"
}

// Config holds the global configuration
type Config struct {
	Users    map[string]string `yaml:"users"`     // Local users username:password
	K2Center K2CenterConfig    `yaml:"k2_center"` // K2 Center configuration

	// Structured capability sections
	Tunnel TunnelSectionConfig `yaml:"tunnel"`
	Relay  RelaySectionConfig  `yaml:"relay"`
	Node   NodeSectionConfig   `yaml:"node"`

	// OpenConnect tunnel (sidecar registers, k2-slave routes via SNI)
	OC OCTunnelConfig `yaml:"oc"`

	// ECH (Encrypted Client Hello) configuration
	// Keys file is managed by sidecar, k2-slave only reads from it
	ECH ECHSectionConfig `yaml:"ech"`

	// SNI local routing rules (higher priority than Center lookup)
	// Format: domain -> target (e.g., "*.oc.example.com" -> "k2oc:443")
	// Use "LOCAL" as target to indicate local handling
	LocalRoutes map[string]string `yaml:"local_routes"`

	// Test node flag (used by sidecar for tunnel registration)
	TestNode bool `yaml:"test_node"`

	// Config directory (sidecar writes certs here, k2-slave reads from here)
	ConfigDir string `yaml:"config_dir" default:"/etc/kaitu"`

	// K2V4Port is the port k2v4 server listens on (default: "8443")
	// Used in k2v5-config.yaml local_routes
	K2V4Port string `yaml:"k2v4_port" default:"8443"`
}

var (
	config         *Config
	configOnce     sync.Once
	configFilePath string // Custom config file path (set via -c flag)
	// Node singleton instance
	nodeInstance *sidecar.Node
	nodeOnce     sync.Once
)

// SetConfigFile sets the config file path (call before GetConfig)
func SetConfigFile(path string) {
	configFilePath = path
}

// ResetConfig resets the config singleton for testing purposes
// This should ONLY be used in tests to ensure a fresh config is loaded
func ResetConfig() {
	config = nil
	configOnce = sync.Once{}
	configFilePath = "" // Reset custom config path for test isolation
	nodeInstance = nil
	nodeOnce = sync.Once{}
}

// GetConfig returns the global configuration (lazy-loaded, auto-initialized)
func GetConfig() Config {
	configOnce.Do(func() {
		config = loadConfig()
	})
	return *config
}

// loadConfig loads configuration (internal function)
func loadConfig() *Config {
	cfg := &Config{}

	// 1. Set default values
	if err := defaults.Set(cfg); err != nil {
		slog.Warn("Failed to set defaults", "component", "config", "err", err)
	}

	// 2. Find config file (prefer -c specified path)
	var cfgFile string
	if configFilePath != "" {
		// Use the path specified via -c flag
		if fileExists(configFilePath) {
			cfgFile = configFilePath
		} else {
			slog.Error("Specified config file does not exist", "component", "config", "path", configFilePath)
			os.Exit(1)
		}
	} else {
		// Fall back to auto-discovery (same directory as executable)
		cfgFile = findConfigFile()
	}

	if cfgFile != "" {
		slog.Info("Loading config file", "component", "config", "path", cfgFile)
		if err := loadConfigFile(cfg, cfgFile); err != nil {
			slog.Warn("Failed to load config file", "component", "config", "err", err)
		}
	} else {
		slog.Info("No config file found, using defaults", "component", "config")
	}

	// 3. Override K2V4Port from environment if set
	if envPort := os.Getenv("K2V4_PORT"); envPort != "" {
		cfg.K2V4Port = envPort
		slog.Info("Read K2V4_PORT from env", "component", "config", "port", cfg.K2V4Port)
	}

	// 4. If K2 enabled, must get IPv4 address (from config or auto-detect)
	if cfg.K2Center.Enabled {
		// Read jump port configuration from environment variables
		if envHopPortStart := os.Getenv("K2_JUMP_PORT_MIN"); envHopPortStart != "" {
			if port := parseIntSafe(envHopPortStart, 0); port > 0 {
				cfg.Tunnel.HopPortStart = port
				slog.Info("Read hop_port_start from env", "component", "config", "port", cfg.Tunnel.HopPortStart)
			}
		}
		if envHopPortEnd := os.Getenv("K2_JUMP_PORT_MAX"); envHopPortEnd != "" {
			if port := parseIntSafe(envHopPortEnd, 0); port > 0 {
				cfg.Tunnel.HopPortEnd = port
				slog.Info("Read hop_port_end from env", "component", "config", "port", cfg.Tunnel.HopPortEnd)
			}
		}

		var ipData sidecar.IPData
		var needIPDetection = cfg.Node.IPv4 == "" || cfg.Node.Country == "" || cfg.Node.Region == ""

		if needIPDetection {
			slog.Info("Detecting missing network info...", "component", "config")
			var err error
			ipData, err = sidecar.GetExternalIP("ipv4")
			if err != nil {
				if cfg.Node.IPv4 == "" {
					slog.Error("Failed to get IPv4 address (K2 mode requires IPv4)", "component", "config", "err", err)
					os.Exit(1)
				}
				slog.Warn("Failed to get location info, using defaults", "component", "config", "err", err)
			}
		}

		// Set IPv4
		if cfg.Node.IPv4 == "" {
			if ipData.IP == "" {
				slog.Error("IPv4 address is empty, cannot start (K2 mode requires IPv4)", "component", "config")
				os.Exit(1)
			}
			cfg.Node.IPv4 = ipData.IP
			slog.Info("Auto-detected IPv4", "component", "config", "ipv4", cfg.Node.IPv4)
		} else {
			slog.Info("Using configured IPv4", "component", "config", "ipv4", cfg.Node.IPv4)
		}

		// Set country code
		if cfg.Node.Country == "" && ipData.CountryCode != "" {
			cfg.Node.Country = ipData.CountryCode
			slog.Info("Auto-detected country", "component", "config", "country", cfg.Node.Country)
		}

		// Set Region (default: Country-Location)
		if cfg.Node.Region == "" {
			if ipData.CountryCode != "" && ipData.Location != "" {
				cfg.Node.Region = slugify(ipData.CountryCode + "-" + ipData.Location)
				slog.Info("Auto-generated region", "component", "config", "region", cfg.Node.Region)
			} else if cfg.Node.Country != "" {
				cfg.Node.Region = slugify(cfg.Node.Country)
				slog.Info("Using country as region", "component", "config", "region", cfg.Node.Region)
			}
		} else {
			slog.Info("Using configured region", "component", "config", "region", cfg.Node.Region)
		}

		// Verify IPv4 is set
		if cfg.Node.IPv4 == "" {
			slog.Error("IPv4 address is empty, cannot start (K2 mode requires IPv4)", "component", "config")
			os.Exit(1)
		}
	}

	// Auto-generate Tunnel.Domain using sslip.io if not configured
	if cfg.Tunnel.Domain == "" && cfg.Node.IPv4 != "" {
		cfg.Tunnel.Domain = strings.ReplaceAll(cfg.Node.IPv4, ".", "-") + ".sslip.io"
		slog.Info("Auto-generated tunnel domain", "component", "config", "domain", cfg.Tunnel.Domain)
	}

	// Validate configuration and log warnings
	validateConfig(cfg)

	slog.Info("Config loaded",
		"component", "config",
		"ipv4", cfg.Node.IPv4,
		"country", cfg.Node.Country,
		"region", cfg.Node.Region,
		"tunnelPort", cfg.Tunnel.Port,
		"k2Enabled", cfg.K2Center.Enabled,
		"tunnelDomain", cfg.Tunnel.Domain)

	return cfg
}

// findConfigFile finds config file (same directory as executable)
func findConfigFile() string {
	exePath, err := os.Executable()
	if err != nil {
		slog.Warn("Failed to get executable path", "component", "config", "err", err)
		return ""
	}

	exeDir := filepath.Dir(exePath)

	configNames := []string{
		"config.yml",
		"config.yaml",
		"kaitu-slave.yml",
		"kaitu-slave.yaml",
	}

	for _, name := range configNames {
		configPath := filepath.Join(exeDir, name)
		if fileExists(configPath) {
			return configPath
		}
	}

	return ""
}

// loadConfigFile loads configuration from file
func loadConfigFile(cfg *Config, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("failed to parse config file: %w", err)
	}

	return nil
}

// fileExists checks if a file exists
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// slugify converts a string to slug format (lowercase, spaces replaced with hyphens)
func slugify(s string) string {
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ToLower(s)
	return s
}

// parseIntSafe safely parses a string to int, returning defaultValue on failure
func parseIntSafe(s string, defaultValue int) int {
	var result int
	if _, err := fmt.Sscanf(s, "%d", &result); err != nil {
		return defaultValue
	}
	return result
}

// validateConfig validates configuration and logs warnings
func validateConfig(cfg *Config) {
	// Warn if both tunnel and relay are disabled
	if !cfg.Tunnel.Enabled && !cfg.Relay.Enabled {
		slog.Warn("Both tunnel and relay are disabled - node will not serve any traffic", "component", "config")
	}

	// Warn if tunnel is enabled but no domain
	if cfg.Tunnel.Enabled && cfg.Tunnel.Domain == "" && cfg.K2Center.Enabled {
		slog.Info("Tunnel domain not set, will auto-generate using sslip.io", "component", "config")
	}
}

// GetNode returns the shared Node instance (lazy-loaded, singleton pattern)
func GetNode() (*sidecar.Node, error) {
	cfg := GetConfig()

	if !cfg.K2Center.Enabled {
		return nil, fmt.Errorf("K2 is not enabled")
	}

	var initErr error
	nodeOnce.Do(func() {
		slog.Info("Initializing shared Node instance...", "component", "node")
		nodeInstance, initErr = sidecar.NewNode(cfg.K2Center.BaseURL, cfg.K2Center.Secret)
		if initErr != nil {
			slog.Error("Failed to create Node instance", "component", "node", "err", initErr)
		} else {
			slog.Info("Shared Node instance initialized successfully", "component", "node")
		}
	})

	if initErr != nil {
		return nil, initErr
	}

	return nodeInstance, nil
}

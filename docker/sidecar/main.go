package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"text/template"
	"time"

	"github.com/kaitu-io/k2-sidecar/config"
	"github.com/kaitu-io/k2-sidecar/sidecar"
)

// Sidecar manages node registration, config generation, and metrics
type Sidecar struct {
	config       *config.Config
	nodeInstance *sidecar.Node
	collector    *sidecar.Collector
	shutdownChan chan os.Signal
}

var configFile string

func init() {
	flag.StringVar(&configFile, "c", "", "Path to config file (required)")
}

func main() {
	flag.Parse()
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))
	slog.Info("Starting k2-sidecar v5.0 (unified config, batch registration)", "component", "sidecar")

	if configFile == "" {
		slog.Error("Config file path is required. Usage: ./k2-sidecar -c /path/to/config.yaml", "component", "sidecar")
		os.Exit(1)
	}

	// Use config package's config loading
	config.SetConfigFile(configFile)
	cfg := config.GetConfig()

	s, err := NewSidecar(&cfg)
	if err != nil {
		slog.Error("Failed to initialize", "component", "sidecar", "err", err)
		os.Exit(1)
	}

	if err := s.Start(); err != nil {
		slog.Error("Failed to start", "component", "sidecar", "err", err)
		os.Exit(1)
	}
}

// NewSidecar creates a new sidecar instance
func NewSidecar(cfg *config.Config) (*Sidecar, error) {
	n, err := sidecar.NewNode(cfg.K2Center.BaseURL, cfg.K2Center.Secret)
	if err != nil {
		return nil, fmt.Errorf("failed to create node: %w", err)
	}

	if cfg.Node.Name != "" {
		n.Name = cfg.Node.Name
	}
	if cfg.Node.Region != "" {
		n.Region = cfg.Node.Region
	}
	// Private-node activation: echo the one-time claim token back to Center on
	// registration. Empty for shared-pool nodes (omitempty → no wire change).
	n.PrivateClaim = cfg.K2Center.PrivateClaim

	// Auto-generate Tunnel.Domain using sslip.io if not configured
	if cfg.Tunnel.Domain == "" && n.GetIPv4() != "" {
		cfg.Tunnel.Domain = strings.ReplaceAll(n.GetIPv4(), ".", "-") + ".sslip.io"
		slog.Info("Auto-generated K2 domain", "component", "sidecar", "domain", cfg.Tunnel.Domain)
	}

	return &Sidecar{
		config:       cfg,
		nodeInstance: n,
		shutdownChan: make(chan os.Signal, 1),
	}, nil
}

// Start starts the sidecar service
func (s *Sidecar) Start() error {
	slog.Info("Center URL", "component", "sidecar", "url", s.config.K2Center.BaseURL)
	slog.Info("Config Dir", "component", "sidecar", "dir", s.config.ConfigDir)

	// Step 1: Build tunnel configurations
	tunnels := s.buildTunnelConfigs()
	slog.Info("Tunnels to register", "component", "sidecar", "count", len(tunnels))

	if len(tunnels) == 0 {
		return fmt.Errorf("no tunnels configured (K2_DOMAIN is empty)")
	}

	// Step 2: Register node with all tunnels
	result, err := s.nodeInstance.Register(tunnels)
	if err != nil {
		return fmt.Errorf("failed to register node: %w", err)
	}
	slog.Info("Node registered", "component", "sidecar", "ipv4", result.IPv4, "tunnels", len(result.Tunnels))

	// Step 3: Save certificates and generate configs
	if err := s.saveCertificates(result); err != nil {
		return fmt.Errorf("failed to save certificates: %w", err)
	}

	if err := s.generateConfigs(result); err != nil {
		return fmt.Errorf("failed to generate configs: %w", err)
	}

	// Step 3a: Fetch ECH keys from Center
	if s.config.ECH.Enabled {
		echKeysFile := s.config.ECH.KeysFile
		if echKeysFile == "" {
			echKeysFile = fmt.Sprintf("%s/ech_keys.yaml", s.config.ConfigDir)
		}
		count, err := s.nodeInstance.FetchECHKeys(echKeysFile)
		if err != nil {
			slog.Warn("Failed to fetch ECH keys", "component", "sidecar", "err", err)
			// Non-fatal: k2-slave will start without ECH
		} else {
			slog.Info("Fetched ECH keys", "component", "sidecar", "count", count, "file", echKeysFile)
		}
	}

	// Step 3.5: Create ready flag to signal other containers
	readyFile := fmt.Sprintf("%s/.ready", s.config.ConfigDir)
	if err := os.WriteFile(readyFile, []byte(fmt.Sprintf("%d", time.Now().Unix())), 0644); err != nil {
		return fmt.Errorf("failed to create ready flag: %w", err)
	}
	slog.Info("Created ready flag", "component", "sidecar", "file", readyFile)

	// Step 3.6: Start k2v5 connect-url polling (if K2 tunnel is enabled)
	// k2v5 writes connect-url.txt after startup, which is after sidecar's
	// initial registration. This goroutine waits for the file and re-registers.
	if s.config.Tunnel.Enabled && s.config.Tunnel.Domain != "" {
		go s.pollAndRegisterK2V5ConnectURL()
	}

	// Step 4: Initialize and start metrics collector
	reportInterval := parseReportInterval(s.config.K2Center.ReportInterval)
	s.collector = sidecar.NewCollector(
		s.nodeInstance,
		reportInterval,
		s.config.K2Center.BillingStartDate,
		s.config.K2Center.TrafficLimitGB,
	)

	// Start metrics collection in background
	go func() {
		if err := s.collector.Run(); err != nil {
			slog.Error("Metrics collector error", "component", "sidecar", "err", err)
		}
	}()

	// Step 4.5: Private nodes self-meter HOST-NIC usage to Center's quota
	// ledger (Part 2). The sidecar is the single usage reporter — k2s's
	// in-process reporter is retired. Online enforcement is Center-side
	// (device-auth + tunnel-hide gates read the CloudInstance.TrafficUsedBytes
	// this reporter feeds). Gate on the private claim so shared-pool nodes never
	// call /slave/usage (byte-identical to before).
	if s.nodeInstance.PrivateClaim != "" {
		meter := sidecar.NewHostNICMeter() // single meter shared by reporter + enforcer
		reporter := sidecar.NewUsageReporter(
			meter,
			s.config.K2Center.BaseURL,
			s.nodeInstance.IPv4,
			s.nodeInstance.Secret,
		)

		// Node-side cutoff: the enforcer's 5s local loop pauses data-plane
		// containers when usage reaches 100% of quota. If the docker client can't
		// be created, keep reporting but skip node-side cutoff (degraded, logged).
		if enf, err := sidecar.NewEnforcer(meter); err != nil {
			slog.Error("Cutoff enforcer init failed; reporting continues without node-side cutoff",
				"component", "sidecar", "err", err)
		} else {
			reporter.SetSink(enf)
			go enf.Run(context.Background())
			slog.Info("Private-node traffic cutoff enforcer started", "component", "sidecar")
		}

		go reporter.Run(context.Background())
		slog.Info("Private-node usage reporter started", "component", "sidecar", "ipv4", s.nodeInstance.IPv4)
	} else {
		slog.Info("Usage reporter disabled (not a private node)", "component", "sidecar")
	}

	// Setup signal handling
	signal.Notify(s.shutdownChan, syscall.SIGINT, syscall.SIGTERM)

	slog.Info("Service started successfully, waiting for shutdown signal...", "component", "sidecar")

	// Wait for shutdown signal
	sig := <-s.shutdownChan
	slog.Info("Received signal, shutting down...", "component", "sidecar", "signal", sig)

	return s.shutdown()
}

// parseReportInterval parses duration string with fallback to default
func parseReportInterval(interval string) time.Duration {
	if interval == "" {
		return 120 * time.Second
	}
	d, err := time.ParseDuration(interval)
	if err != nil {
		slog.Warn("Invalid report_interval, using default 120s", "component", "sidecar", "interval", interval)
		return 120 * time.Second
	}
	return d
}

// buildTunnelConfigs builds tunnel configurations from config
func (s *Sidecar) buildTunnelConfigs() []sidecar.TunnelConfig {
	var tunnels []sidecar.TunnelConfig

	// K2 tunnel — use k2v5 protocol for new deployments
	if s.config.Tunnel.Enabled && s.config.Tunnel.Domain != "" {
		// Read k2v5 connect URL and build server URL
		var serverURL string
		connectURLPath := "/etc/k2v5/connect-url.txt"
		if data, err := os.ReadFile(connectURLPath); err == nil {
			serverURL = sidecar.BuildServerURL(strings.TrimSpace(string(data)),
				s.config.Tunnel.Domain, s.config.Tunnel.Port,
				s.nodeInstance.IPv4, s.nodeInstance.IPv6)
			if serverURL != "" {
				slog.Info("Built k2v5 server URL", "component", "sidecar", "url", serverURL)
			}
		} else if !os.IsNotExist(err) {
			slog.Warn("Failed to read connect-url.txt", "component", "sidecar", "err", err)
		}

		tunnels = append(tunnels, sidecar.TunnelConfig{
			Domain:       s.config.Tunnel.Domain,
			Protocol:     "k2v5", // K2 protocol version 5
			Port:         s.config.Tunnel.Port,
			HopPortStart: s.config.Tunnel.HopPortStart,
			HopPortEnd:   s.config.Tunnel.HopPortEnd,
			IsTest:       s.config.TestNode,
			HasRelay:     s.config.Relay.Enabled,
			HasTunnel:    s.config.Tunnel.Enabled,
			ServerURL:    serverURL,
		})
		testSuffix := ""
		if s.config.TestNode {
			testSuffix = " (test node)"
		}
		slog.Info("K2 tunnel configured",
			"component", "sidecar",
			"domain", s.config.Tunnel.Domain,
			"port", s.config.Tunnel.Port,
			"protocol", "k2v5",
			"hopPortStart", s.config.Tunnel.HopPortStart,
			"hopPortEnd", s.config.Tunnel.HopPortEnd,
			"suffix", testSuffix)
	}

	return tunnels
}

// readConnectURL reads connect-url.txt from the given directory and builds a
// clean server URL using BuildServerURL. Returns empty string if the file
// doesn't exist or doesn't contain usable parameters.
func readConnectURL(dir, domain string, port int, ipv4, ipv6 string) string {
	data, err := os.ReadFile(filepath.Join(dir, "connect-url.txt"))
	if err != nil {
		return ""
	}
	return sidecar.BuildServerURL(strings.TrimSpace(string(data)), domain, port, ipv4, ipv6)
}

// pollAndRegisterK2V5ConnectURL polls for /etc/k2v5/connect-url.txt and
// re-registers with Center once the file appears. k2v5 writes this file
// after startup, which happens after sidecar's initial registration.
func (s *Sidecar) pollAndRegisterK2V5ConnectURL() {
	const connectURLDir = "/etc/k2v5"
	const connectURLFile = "connect-url.txt"
	const pollInterval = 5 * time.Second
	const logEvery = 6 // log "waiting" every 6 polls (30s)

	// Only accept connect-url.txt written AFTER this timestamp.
	// The k2v5-data volume persists across restarts, so a stale file
	// from a previous k2v5 run would be found immediately and cause
	// registration with outdated data (e.g. missing hop params).
	pollStart := time.Now()

	iteration := 0
	for {
		if info, err := os.Stat(filepath.Join(connectURLDir, connectURLFile)); err == nil && info.ModTime().After(pollStart) {
			serverURL := readConnectURL(connectURLDir,
				s.config.Tunnel.Domain, s.config.Tunnel.Port,
				s.nodeInstance.IPv4, s.nodeInstance.IPv6)

			if serverURL != "" {
				tunnels := s.buildTunnelConfigs()
				for i := range tunnels {
					if tunnels[i].Domain == s.config.Tunnel.Domain {
						tunnels[i].ServerURL = serverURL
					}
				}

				if _, err := s.nodeInstance.Register(tunnels); err != nil {
					slog.Error("Failed to re-register with k2v5 serverURL", "component", "sidecar", "err", err)
				} else {
					slog.Info("Updated k2v5 serverURL", "component", "sidecar", "url", serverURL)
				}
				return
			}
		}

		iteration++
		if iteration%logEvery == 0 {
			slog.Info("Waiting for k2v5 connect-url.txt...", "component", "sidecar")
		}
		time.Sleep(pollInterval)
	}
}

// saveCertificates saves tunnel certificates to required directories
// Saves domain-specific certs and creates symlinks to fixed filenames for k2-slave
func (s *Sidecar) saveCertificates(result *sidecar.RegisterResult) error {
	// Primary cert directory for K2
	kaituCertDir := fmt.Sprintf("%s/certs", s.config.ConfigDir)
	if err := os.MkdirAll(kaituCertDir, 0755); err != nil {
		return fmt.Errorf("failed to create kaitu certs dir: %w", err)
	}

	// Save certificates for each tunnel to appropriate directories
	for domain, cert := range result.Tunnels {
		// K2 tunnel cert -> /etc/kaitu/certs/
		if domain == s.config.Tunnel.Domain {
			// Save domain-specific certificate files (for history/debugging)
			domainCertFile := fmt.Sprintf("%s-cert.pem", domain)
			domainKeyFile := fmt.Sprintf("%s-key.pem", domain)

			if err := cert.SaveToFiles(kaituCertDir, domainCertFile, domainKeyFile); err != nil {
				return fmt.Errorf("failed to save K2 cert to %s: %w", kaituCertDir, err)
			}
			slog.Info("Saved K2 certificate", "component", "sidecar", "dir", kaituCertDir, "file", domainCertFile)

			// Create symlinks or copy to fixed filenames for k2-slave
			if err := s.linkOrCopyCertificate(kaituCertDir, domainCertFile, "server-cert.pem"); err != nil {
				return fmt.Errorf("failed to link K2 cert: %w", err)
			}
			if err := s.linkOrCopyCertificate(kaituCertDir, domainKeyFile, "server-key.pem"); err != nil {
				return fmt.Errorf("failed to link K2 key: %w", err)
			}
			slog.Info("Linked K2 certificate to server-cert.pem", "component", "sidecar")
		}

	}

	return nil
}

// linkOrCopyCertificate creates symlink or copies file to target name
// Tries symlink first, falls back to copy if symlink fails (cross-device, permissions, etc.)
func (s *Sidecar) linkOrCopyCertificate(dir, sourceFile, targetFile string) error {
	sourcePath := filepath.Join(dir, sourceFile)
	targetPath := filepath.Join(dir, targetFile)

	// Remove existing target (could be old symlink or file)
	os.Remove(targetPath)

	// Try to create symlink first
	if err := os.Symlink(sourceFile, targetPath); err != nil {
		// Fallback: copy file if symlink fails
		slog.Warn("Symlink failed, copying instead", "component", "sidecar", "err", err)
		data, readErr := os.ReadFile(sourcePath)
		if readErr != nil {
			return fmt.Errorf("failed to read source file: %w", readErr)
		}
		// Preserve permissions: 0644 for certs, 0600 for keys
		perm := os.FileMode(0644)
		if strings.Contains(sourceFile, "key") {
			perm = 0600
		}
		if writeErr := os.WriteFile(targetPath, data, perm); writeErr != nil {
			return fmt.Errorf("failed to write target file: %w", writeErr)
		}
	}

	return nil
}

// generateConfigs generates configuration files for tunnels
func (s *Sidecar) generateConfigs(result *sidecar.RegisterResult) error {
	// Generate k2v5-config.yaml for k2s server
	if s.config.Tunnel.Enabled && s.config.Tunnel.Domain != "" {
		if err := s.generateK2V5Config(); err != nil {
			return fmt.Errorf("failed to generate k2v5 config: %w", err)
		}
	}

	// Generate k2v4-config.yaml (simplified — no ECH, no local_routes)
	if s.config.Tunnel.Enabled && s.config.Tunnel.Domain != "" {
		if err := s.generateK2V4Config(); err != nil {
			return fmt.Errorf("failed to generate k2v4 config: %w", err)
		}
	}

	return nil
}

// K2V5ConfigData holds template data for k2v5 configuration
type K2V5ConfigData struct {
	CertDir   string
	CertPath  string
	KeyPath   string
	K2Domain  string
	K2V4Host  string
	K2V4Port  string
	CenterURL string
	LogLevel  string
	UsersFile string
	HopStart  int
	HopEnd    int
}

const k2v5ConfigTemplate = `listen: ":443"
cert_dir: "{{.CertDir}}"
# tls — dormant: k2v5 generates its own self-signed certs in cert_dir.
# These entries are kept for documentation; k2v5 ignores them.
tls:
  cert: "{{.CertPath}}"
  key: "{{.KeyPath}}"
auth:
  users_file: "{{.UsersFile}}"
  remote_url: "{{.CenterURL}}/slave/device-check-auth"
  cache_ttl: 5m
local_routes:
  "{{.K2Domain}}": "{{.K2V4Host}}:{{.K2V4Port}}"
log:
  level: "{{.LogLevel}}"
{{- if and .HopStart .HopEnd}}
hop_start: {{.HopStart}}
hop_end: {{.HopEnd}}
{{- end}}
`

// generateK2V5Config generates k2v5-config.yaml for k2s server
func (s *Sidecar) generateK2V5Config() error {
	configDir := s.config.ConfigDir
	outputPath := fmt.Sprintf("%s/k2v5-config.yaml", configDir)

	logLevel := os.Getenv("K2_LOG_LEVEL")
	if logLevel == "" {
		logLevel = "info"
	}

	k2v4Host := os.Getenv("K2V4_HOST")
	if k2v4Host == "" {
		k2v4Host = "127.0.0.1"
	}

	k2v5DataDir := "/etc/k2v5"

	data := K2V5ConfigData{
		CertDir:   k2v5DataDir,
		CertPath:  fmt.Sprintf("%s/certs/server-cert.pem", configDir),
		KeyPath:   fmt.Sprintf("%s/certs/server-key.pem", configDir),
		K2Domain:  s.config.Tunnel.Domain,
		K2V4Host:  k2v4Host,
		K2V4Port:  s.config.K2V4Port,
		CenterURL: s.config.K2Center.BaseURL,
		LogLevel:  logLevel,
		UsersFile: k2v5DataDir + "/users",
		HopStart:  s.config.Tunnel.HopPortStart,
		HopEnd:    s.config.Tunnel.HopPortEnd,
	}

	return s.generateConfigFromTemplate("k2v5-config.yaml", k2v5ConfigTemplate, outputPath, data)
}

// K2V4ConfigData holds template data for old-style config.yaml
// The k2v4-slave binary (k2-slave) reads this format for cert paths and Center auth.
type K2V4ConfigData struct {
	CenterURL    string
	CenterSecret string
	Domain       string
	ConfigDir    string
}

// Old-style config.yaml that k2-slave binary expects.
// No local_routes or hop_port (k2v5 handles SNI routing and DNAT).
const k2v4ConfigTemplate = `k2_center:
  enabled: true
  base_url: "{{.CenterURL}}"
  timeout: "10s"
  secret: "{{.CenterSecret}}"
tunnel:
  enabled: true
  domain: "{{.Domain}}"
  port: 443
config_dir: "{{.ConfigDir}}"
`

// generateK2V4Config generates config.yaml in old k2-slave format
func (s *Sidecar) generateK2V4Config() error {
	configDir := s.config.ConfigDir
	outputPath := fmt.Sprintf("%s/config.yaml", configDir)

	data := K2V4ConfigData{
		CenterURL:    s.config.K2Center.BaseURL,
		CenterSecret: s.config.K2Center.Secret,
		Domain:       s.config.Tunnel.Domain,
		ConfigDir:    configDir,
	}

	return s.generateConfigFromTemplate("config.yaml", k2v4ConfigTemplate, outputPath, data)
}

func (s *Sidecar) generateConfigFromTemplate(name, tmplStr, outputPath string, data interface{}) error {
	tmpl, err := template.New(name).Parse(tmplStr)
	if err != nil {
		return fmt.Errorf("failed to parse template: %w", err)
	}

	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	if err := tmpl.Execute(file, data); err != nil {
		return fmt.Errorf("failed to execute template: %w", err)
	}

	slog.Info("Generated config file", "component", "sidecar", "path", outputPath)
	return nil
}

// shutdown gracefully shuts down the sidecar
func (s *Sidecar) shutdown() error {
	slog.Info("Shutting down...", "component", "sidecar")

	if err := s.nodeInstance.Unregister(); err != nil {
		slog.Warn("Failed to unregister node", "component", "sidecar", "err", err)
	} else {
		slog.Info("Node unregistered successfully", "component", "sidecar")
	}

	slog.Info("Shutdown complete", "component", "sidecar")
	return nil
}

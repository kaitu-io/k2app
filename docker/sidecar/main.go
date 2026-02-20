package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"text/template"
	"time"

	"github.com/kaitu-io/k2-sidecar/config"
	"github.com/kaitu-io/k2-sidecar/sidecar"
	"layeh.com/radius"
	"layeh.com/radius/rfc2865"
)

// Sidecar manages node registration, config generation, RADIUS proxy, and metrics
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
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Printf("[Sidecar] Starting k2-slave-sidecar v5.0 (unified config, batch registration)")

	if configFile == "" {
		log.Fatalf("[Sidecar] Config file path is required. Usage: ./k2-sidecar -c /path/to/config.yaml")
	}

	// Use config package's config loading
	config.SetConfigFile(configFile)
	cfg := config.GetConfig()

	s, err := NewSidecar(&cfg)
	if err != nil {
		log.Fatalf("[Sidecar] Failed to initialize: %v", err)
	}

	if err := s.Start(); err != nil {
		log.Fatalf("[Sidecar] Failed to start: %v", err)
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

	// Auto-generate Tunnel.Domain using sslip.io if not configured
	if cfg.Tunnel.Domain == "" && n.GetIPv4() != "" {
		cfg.Tunnel.Domain = strings.ReplaceAll(n.GetIPv4(), ".", "-") + ".sslip.io"
		log.Printf("[Sidecar] Auto-generated K2 domain: %s", cfg.Tunnel.Domain)
	}

	// Note: OC domain is NOT auto-generated - must be explicitly configured
	// (unlike K2 tunnel which auto-generates using sslip.io)

	return &Sidecar{
		config:       cfg,
		nodeInstance: n,
		shutdownChan: make(chan os.Signal, 1),
	}, nil
}

// Start starts the sidecar service
func (s *Sidecar) Start() error {
	log.Printf("[Sidecar] Center URL: %s", s.config.K2Center.BaseURL)
	log.Printf("[Sidecar] Config Dir: %s", s.config.ConfigDir)

	// Step 1: Build tunnel configurations
	tunnels := s.buildTunnelConfigs()
	log.Printf("[Sidecar] Tunnels to register: %d", len(tunnels))

	// Step 2: Register node with all tunnels
	result, err := s.nodeInstance.Register(tunnels)
	if err != nil {
		return fmt.Errorf("failed to register node: %w", err)
	}
	log.Printf("[Sidecar] Node registered: IPv4=%s, Tunnels=%d", result.IPv4, len(result.Tunnels))

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
			log.Printf("[Sidecar] Warning: Failed to fetch ECH keys: %v", err)
			// Non-fatal: k2-slave will start without ECH
		} else {
			log.Printf("[Sidecar] Fetched %d ECH keys to %s", count, echKeysFile)
		}
	}

	// Step 3.5: Create ready flag to signal other containers
	readyFile := fmt.Sprintf("%s/.ready", s.config.ConfigDir)
	if err := os.WriteFile(readyFile, []byte(fmt.Sprintf("%d", time.Now().Unix())), 0644); err != nil {
		return fmt.Errorf("failed to create ready flag: %w", err)
	}
	log.Printf("[Sidecar] Created ready flag: %s", readyFile)

	// Step 4: Start RADIUS proxy (if OC tunnel is configured)
	if s.config.OC.Domain != "" {
		if err := s.startRadiusProxy(); err != nil {
			return fmt.Errorf("failed to start RADIUS proxy: %w", err)
		}
	}

	// Step 5: Initialize and start metrics collector
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
			log.Printf("[Sidecar] Metrics collector error: %v", err)
		}
	}()

	// Setup signal handling
	signal.Notify(s.shutdownChan, syscall.SIGINT, syscall.SIGTERM)

	log.Printf("[Sidecar] Service started successfully, waiting for shutdown signal...")

	// Wait for shutdown signal
	sig := <-s.shutdownChan
	log.Printf("[Sidecar] Received signal: %v, shutting down...", sig)

	return s.shutdown()
}

// parseReportInterval parses duration string with fallback to default
func parseReportInterval(interval string) time.Duration {
	if interval == "" {
		return 120 * time.Second
	}
	d, err := time.ParseDuration(interval)
	if err != nil {
		log.Printf("[Sidecar] Invalid report_interval %q, using default 120s", interval)
		return 120 * time.Second
	}
	return d
}

// buildTunnelConfigs builds tunnel configurations from config
func (s *Sidecar) buildTunnelConfigs() []sidecar.TunnelConfig {
	var tunnels []sidecar.TunnelConfig

	// K2 tunnel — use k2v5 protocol for new deployments
	if s.config.Tunnel.Enabled && s.config.Tunnel.Domain != "" {
		// Read k2v5 connect URL for cert pin and ECH config
		var k2v5CertPin, k2v5ECHConfig string
		connectURLPath := fmt.Sprintf("%s/connect-url.txt", s.config.ConfigDir)
		if data, err := os.ReadFile(connectURLPath); err == nil {
			k2v5CertPin, k2v5ECHConfig = sidecar.ParseConnectURL(strings.TrimSpace(string(data)))
			if k2v5CertPin != "" {
				pinPreview := k2v5CertPin
				if len(pinPreview) > 16 {
					pinPreview = pinPreview[:16]
				}
				log.Printf("[Sidecar] Parsed k2v5 connect URL: pin=%s... ech=%d bytes",
					pinPreview, len(k2v5ECHConfig))
			}
		} else if !os.IsNotExist(err) {
			log.Printf("[Sidecar] Warning: failed to read connect-url.txt: %v", err)
		}

		tunnels = append(tunnels, sidecar.TunnelConfig{
			Domain:        s.config.Tunnel.Domain,
			Protocol:      "k2v5", // K2 protocol version 5
			Port:          s.config.Tunnel.Port,
			HopPortStart:  s.config.Tunnel.HopPortStart,
			HopPortEnd:    s.config.Tunnel.HopPortEnd,
			IsTest:        s.config.TestNode,
			HasRelay:      s.config.Relay.Enabled,
			HasTunnel:     s.config.Tunnel.Enabled,
			CertPin:       k2v5CertPin,
			ECHConfigList: k2v5ECHConfig,
		})
		testSuffix := ""
		if s.config.TestNode {
			testSuffix = " (test node)"
		}
		log.Printf("[Sidecar] K2 tunnel configured: %s:%d, protocol=k2v5, hopPorts=%d-%d%s",
			s.config.Tunnel.Domain, s.config.Tunnel.Port,
			s.config.Tunnel.HopPortStart, s.config.Tunnel.HopPortEnd, testSuffix)
	}

	// OC tunnel (register if domain is configured, regardless of oc.enabled flag)
	// oc.enabled controls RADIUS proxy, not registration
	if s.config.OC.Domain != "" {
		tunnels = append(tunnels, sidecar.TunnelConfig{
			Domain:   s.config.OC.Domain,
			Protocol: "k2oc",
			Port:     s.config.OC.Port,
			IsTest:   s.config.TestNode,
		})
		testSuffix := ""
		if s.config.TestNode {
			testSuffix = " (test node)"
		}
		log.Printf("[Sidecar] OC tunnel configured: %s:%d%s", s.config.OC.Domain, s.config.OC.Port, testSuffix)
	}

	return tunnels
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
			log.Printf("[Sidecar] Saved K2 certificate: %s/%s", kaituCertDir, domainCertFile)

			// Create symlinks or copy to fixed filenames for k2-slave
			if err := s.linkOrCopyCertificate(kaituCertDir, domainCertFile, "server-cert.pem"); err != nil {
				return fmt.Errorf("failed to link K2 cert: %w", err)
			}
			if err := s.linkOrCopyCertificate(kaituCertDir, domainKeyFile, "server-key.pem"); err != nil {
				return fmt.Errorf("failed to link K2 key: %w", err)
			}
			log.Printf("[Sidecar] Linked K2 certificate to server-cert.pem")
		}

		// OC tunnel cert -> /etc/ocserv/
		if domain == s.config.OC.Domain {
			ocservDir := "/etc/ocserv"
			domainCertFile := fmt.Sprintf("%s-cert.pem", domain)
			domainKeyFile := fmt.Sprintf("%s-key.pem", domain)

			if err := cert.SaveToFiles(ocservDir, domainCertFile, domainKeyFile); err != nil {
				return fmt.Errorf("failed to save OC cert to %s: %w", ocservDir, err)
			}
			log.Printf("[Sidecar] Saved OC certificate: %s/%s", ocservDir, domainCertFile)

			// Create symlinks or copy to fixed filenames
			if err := s.linkOrCopyCertificate(ocservDir, domainCertFile, "server-cert.pem"); err != nil {
				return fmt.Errorf("failed to link OC cert: %w", err)
			}
			if err := s.linkOrCopyCertificate(ocservDir, domainKeyFile, "server-key.pem"); err != nil {
				return fmt.Errorf("failed to link OC key: %w", err)
			}
			log.Printf("[Sidecar] Linked OC certificate to server-cert.pem")
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
		log.Printf("[Sidecar] Symlink failed, copying instead: %v", err)
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

	// Generate OC config if OC tunnel is configured
	if s.config.OC.Domain != "" {
		if err := s.generateOcservConfig(); err != nil {
			return fmt.Errorf("failed to generate ocserv config: %w", err)
		}
	}

	return nil
}

// K2V5ConfigData holds template data for k2v5 configuration
type K2V5ConfigData struct {
	CertPath     string
	KeyPath      string
	K2Domain     string
	K2V4Port     string
	K2OCDomain   string
	K2OCPort     string
	CenterURL    string
	LogLevel     string
	HasOCDomain  bool
}

const k2v5ConfigTemplate = `listen: ":443"
tls:
  cert: "{{.CertPath}}"
  key: "{{.KeyPath}}"
auth:
  remote_url: "{{.CenterURL}}/slave/device-check-auth"
  cache_ttl: 5m
local_routes:
  "{{.K2Domain}}": "127.0.0.1:{{.K2V4Port}}"
{{- if .HasOCDomain}}
  "{{.K2OCDomain}}": "127.0.0.1:{{.K2OCPort}}"
{{- end}}
log:
  level: "{{.LogLevel}}"
`

// generateK2V5Config generates k2v5-config.yaml for k2s server
func (s *Sidecar) generateK2V5Config() error {
	configDir := s.config.ConfigDir
	outputPath := fmt.Sprintf("%s/k2v5-config.yaml", configDir)

	logLevel := os.Getenv("K2_LOG_LEVEL")
	if logLevel == "" {
		logLevel = "info"
	}

	k2ocPort := os.Getenv("K2OC_PORT")
	if k2ocPort == "" {
		k2ocPort = "10001"
	}

	data := K2V5ConfigData{
		CertPath:    fmt.Sprintf("%s/certs/server-cert.pem", configDir),
		KeyPath:     fmt.Sprintf("%s/certs/server-key.pem", configDir),
		K2Domain:    s.config.Tunnel.Domain,
		K2V4Port:    s.config.K2V4Port,
		K2OCDomain:  s.config.OC.Domain,
		K2OCPort:    k2ocPort,
		CenterURL:   s.config.K2Center.BaseURL,
		LogLevel:    logLevel,
		HasOCDomain: s.config.OC.Domain != "",
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

// OcservConfigData holds template data for ocserv configuration
type OcservConfigData struct {
	ConfigDir       string
	OcservConfigDir string
	TunnelDomain    string
	ListenPort      int    // Actual port ocserv listens on (host network mode)
	RadiusServer    string // RADIUS server address (127.0.0.1 for host network mode)
	RadiusSecret    string
}

const ocservConfTemplate = `auth = "radius [config={{.OcservConfigDir}}/radius-client.conf,groupconfig=true]"

default-domain={{.TunnelDomain}}

tcp-port = {{.ListenPort}}
udp-port = {{.ListenPort}}

cert-user-oid = 0.9.2342.19200300.100.1.1

run-as-user = nobody
run-as-group = daemon

socket-file = /var/run/ocserv-socket

server-cert = {{.OcservConfigDir}}/server-cert.pem
server-key = {{.OcservConfigDir}}/server-key.pem
ca-cert = {{.OcservConfigDir}}/root-ca-cert.pem

isolate-workers = false
max-clients = 2000
max-same-clients = 1

server-stats-reset-time = 604800
keepalive = 32400
dpd = 90
mobile-dpd = 1800
switch-to-tcp-timeout = 25
try-mtu-discovery = true

compression = true
tls-priorities = "NORMAL:%SERVER_PRECEDENCE:%COMPAT:-VERS-SSL3.0"
auth-timeout = 240
idle-timeout = 1200
mobile-idle-timeout = 2400
min-reauth-time = 1
max-ban-score = 50
ban-reset-time = 300
cookie-timeout = 172800
persistent-cookies = true
deny-roaming = false
rekey-time = 172800
rekey-method = ssl

use-occtl = true
pid-file = /var/run/ocserv.pid
net-priority = 5
device = vpns
predictable-ips = true
ipv4-network = 10.3.0.0
ipv4-netmask = 255.255.0.0
tunnel-all-dns = true
dns = 8.8.8.8
dns = 8.8.4.4

ping-leases = false
mtu = 1420

cisco-client-compat = true
stats-report-time = 60

no-route = 192.168.0.0/255.255.0.0
no-route = 172.16.0.0/255.240.0.0
no-route = 10.0.0.0/255.0.0.0
route = default
`

const radiusClientConfTemplate = `nas-identifier {{.TunnelDomain}}:ocserv-slave

authserver 	{{.RadiusServer}}:1812

servers		{{.OcservConfigDir}}/radius-servers

dictionary 	/usr/share/radcli/dictionary

default_realm
radius_timeout	10
radius_retries	3
bindaddr	*
`

const radiusServersTemplate = `{{.RadiusServer}} {{.RadiusSecret}}
`

// generateOcservConfig generates ocserv configuration files
func (s *Sidecar) generateOcservConfig() error {
	ocservDir := "/etc/ocserv"
	if err := os.MkdirAll(ocservDir, 0755); err != nil {
		return fmt.Errorf("failed to create ocserv config dir: %w", err)
	}

	data := OcservConfigData{
		ConfigDir:       s.config.ConfigDir,
		OcservConfigDir: ocservDir,
		TunnelDomain:    s.config.OC.Domain,
		ListenPort:      s.config.OC.ListenPort,
		RadiusServer:    s.config.OC.RadiusServer,
		RadiusSecret:    "localhost-radius",
	}

	// Download CA certificate
	if err := s.downloadCACert(ocservDir + "/root-ca-cert.pem"); err != nil {
		return fmt.Errorf("failed to download CA cert: %w", err)
	}

	// Generate config files
	configs := map[string]string{
		"ocserv.conf":        ocservConfTemplate,
		"radius-client.conf": radiusClientConfTemplate,
		"radius-servers":     radiusServersTemplate,
	}

	for filename, tmplStr := range configs {
		if err := s.generateConfigFromTemplate(filename, tmplStr, ocservDir+"/"+filename, data); err != nil {
			return fmt.Errorf("failed to generate %s: %w", filename, err)
		}
	}

	log.Printf("[Sidecar] Generated ocserv configuration files in %s", ocservDir)
	return nil
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

	log.Printf("[Sidecar] Generated config file: %s", outputPath)
	return nil
}

func (s *Sidecar) downloadCACert(outputPath string) error {
	caCertURL := s.config.K2Center.BaseURL + "/api/ca"

	// CA certificate is public - no authentication needed
	resp, err := getHTTPClient().Get(caCertURL)
	if err != nil {
		return fmt.Errorf("failed to download CA cert: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("failed to download CA cert: status %d", resp.StatusCode)
	}

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read CA cert response: %w", err)
	}

	// Validate it's a PEM certificate (not a JSON error response)
	if !strings.HasPrefix(string(body), "-----BEGIN") {
		preview := string(body)
		if len(preview) > 100 {
			preview = preview[:100]
		}
		return fmt.Errorf("invalid CA cert response: expected PEM certificate, got: %s", preview)
	}

	if err := os.WriteFile(outputPath, body, 0644); err != nil {
		return fmt.Errorf("failed to write CA cert file: %w", err)
	}

	log.Printf("[Sidecar] Downloaded CA certificate to: %s", outputPath)
	return nil
}

// startRadiusProxy starts the RADIUS authentication proxy
func (s *Sidecar) startRadiusProxy() error {
	handler := radius.HandlerFunc(s.handleRadiusRequest)
	server := radius.PacketServer{
		Handler:      handler,
		SecretSource: radius.StaticSecretSource([]byte("localhost-radius")),
		Addr:         ":1812",
	}

	log.Printf("[RADIUS] Starting RADIUS proxy on :1812")

	go func() {
		if err := server.ListenAndServe(); err != nil {
			log.Printf("[RADIUS] Server error: %v", err)
		}
	}()

	return nil
}

// handleRadiusRequest processes RADIUS authentication requests
// RADIUS protocol: UserName = UDID, UserPassword = Password (MD5)
// Supports UDID format: "udid" or "udid@user_id" (auto-extracts part before @)
func (s *Sidecar) handleRadiusRequest(w radius.ResponseWriter, r *radius.Request) {
	rawUDID := rfc2865.UserName_GetString(r.Packet)
	password := rfc2865.UserPassword_GetString(r.Packet)

	if rawUDID == "" || password == "" {
		log.Printf("[RADIUS] Request missing username (udid) or password")
		w.Write(r.Response(radius.CodeAccessReject))
		return
	}

	// Parse UDID: extract the part before "@" if format is "udid@user_id"
	// This handles RADIUS clients that append realm/user_id to the username
	udid := rawUDID
	if strings.Contains(rawUDID, "@") {
		parts := strings.SplitN(rawUDID, "@", 2)
		udid = parts[0]
		log.Printf("[RADIUS] Parsed UDID format: %s -> %s (extracted from @-format)", rawUDID, udid)
	}

	udidPreview := udid
	if len(udidPreview) > 16 {
		udidPreview = udidPreview[:16]
	}
	log.Printf("[RADIUS] Auth request for udid: %s...", udidPreview)

	// Use node.CheckAuth with internal caching
	success := s.nodeInstance.CheckAuth(udid, password)

	if success {
		log.Printf("[RADIUS] Authentication successful")
		w.Write(r.Response(radius.CodeAccessAccept))
	} else {
		log.Printf("[RADIUS] Authentication failed")
		w.Write(r.Response(radius.CodeAccessReject))
	}
}

// shutdown gracefully shuts down the sidecar
func (s *Sidecar) shutdown() error {
	log.Printf("[Sidecar] Shutting down...")

	// Remove K2 tunnel
	if s.config.Tunnel.Enabled && s.config.Tunnel.Domain != "" {
		if err := s.nodeInstance.RemoveTunnel(s.config.Tunnel.Domain); err != nil {
			log.Printf("[Sidecar] Warning: Failed to remove K2 tunnel: %v", err)
		}
	}
	// Remove OC tunnel
	if s.config.OC.Domain != "" {
		if err := s.nodeInstance.RemoveTunnel(s.config.OC.Domain); err != nil {
			log.Printf("[Sidecar] Warning: Failed to remove OC tunnel: %v", err)
		}
	}

	// Mark node as offline to prevent clients from connecting to a dead node
	if err := s.nodeInstance.MarkOffline(); err != nil {
		log.Printf("[Sidecar] Warning: Failed to mark node offline: %v", err)
	} else {
		log.Printf("[Sidecar] Node marked offline successfully")
	}

	log.Printf("[Sidecar] Shutdown complete")
	return nil
}

// Helper functions

func getHTTPClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Second}
}

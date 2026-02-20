package sidecar

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"gopkg.in/yaml.v2"
)

// IPData IP information
type IPData struct {
	IP          string `json:"ip"`
	Location    string `json:"location"`
	CountryCode string `json:"country_code"`
}

// IPInfo ipinfo.io response
type IPInfo struct {
	IP      string `json:"ip"`
	Country string `json:"country"`
	City    string `json:"city"`
	Region  string `json:"region"`
}

// IPWhois ipwhois.app response
type IPWhois struct {
	IP          string `json:"ip"`
	Success     bool   `json:"success"`
	CountryCode string `json:"country_code"`
	Country     string `json:"country"`
	Region      string `json:"region"`
	City        string `json:"city"`
}

// CenterResponse Center API unified response format
type CenterResponse[T any] struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    *T     `json:"data"`
}

// TunnelConfig tunnel configuration (for batch registration)
type TunnelConfig struct {
	Domain        string `json:"domain"`
	Protocol      string `json:"protocol,omitempty"`      // Protocol: k2v5, k2v4, k2wss, k2oc
	Port          int    `json:"port"`
	HopPortStart  int    `json:"hopPortStart,omitempty"`  // Port hopping start (0 = disabled)
	HopPortEnd    int    `json:"hopPortEnd,omitempty"`    // Port hopping end
	IsTest        bool   `json:"isTest,omitempty"`        // Whether this is a test node
	HasRelay      bool   `json:"hasRelay,omitempty"`      // Whether this tunnel provides relay/forwarding capability
	HasTunnel     bool   `json:"hasTunnel,omitempty"`     // Whether this tunnel provides direct tunnel capability
	CertPin       string `json:"certPin,omitempty"`       // k2v5 cert pin (from connect-url.txt)
	ECHConfigList string `json:"echConfigList,omitempty"` // k2v5 ECH config (from connect-url.txt)
}

// TunnelResult tunnel registration result (with certificate)
type TunnelResult struct {
	Domain       string `json:"domain"`
	Protocol     string `json:"protocol"`     // Protocol: k2v5, k2v4, k2wss, k2oc
	Port         int    `json:"port"`
	HopPortStart int    `json:"hopPortStart"` // Port hopping start
	HopPortEnd   int    `json:"hopPortEnd"`   // Port hopping end
	SSLCert      string `json:"sslCert"`
	SSLKey       string `json:"sslKey"`
	Created      bool   `json:"created"`
	HasRelay     bool   `json:"hasRelay"`  // Whether this tunnel provides relay/forwarding capability
	HasTunnel    bool   `json:"hasTunnel"` // Whether this tunnel provides direct tunnel capability
}

// NodeUpsertRequest node registration/update request (supports batch tunnels)
type NodeUpsertRequest struct {
	Country     string         `json:"country"`
	Region      string         `json:"region,omitempty"`
	Name        string         `json:"name"`
	IPv6        string         `json:"ipv6,omitempty"`
	SecretToken string         `json:"secretToken,omitempty"`
	IsAlive     *bool          `json:"isAlive,omitempty"`    // Node online status (optional, defaults to true)
	Tunnels     []TunnelConfig `json:"tunnels,omitempty"`    // Batch tunnel configuration
}

// NodeUpsertResponse node registration/update response (with tunnel certificates)
type NodeUpsertResponse struct {
	IPv4        string         `json:"ipv4"`
	SecretToken string         `json:"secretToken"`
	Created     bool           `json:"created"`
	Tunnels     []TunnelResult `json:"tunnels,omitempty"` // Tunnel registration results
}

// RegisterResult registration result (simplified interface)
type RegisterResult struct {
	IPv4        string                        // Node IPv4
	NodeCreated bool                          // Whether node was newly created
	Tunnels     map[string]*TunnelCertificate // Tunnel certificates (key = domain)
}

// TunnelUpsertRequest tunnel registration/update request (single tunnel, backward compatible)
type TunnelUpsertRequest struct {
	Name         string `json:"name"`
	Protocol     string `json:"protocol,omitempty"`
	Port         int    `json:"port"`
	Version      int    `json:"version"`       // K2 protocol version
	HopPortStart int    `json:"hopPortStart"`  // Port hopping start
	HopPortEnd   int    `json:"hopPortEnd"`    // Port hopping end
}

// TunnelUpsertResponse tunnel registration/update response (single tunnel, backward compatible)
type TunnelUpsertResponse struct {
	TunnelID uint64 `json:"tunnelId"`
	Domain   string `json:"domain"`
	SSLCert  string `json:"sslCert"`
	SSLKey   string `json:"sslKey"`
	Created  bool   `json:"created"`
}

// TunnelCertificate encapsulates tunnel certificate information
type TunnelCertificate struct {
	SSLCert string // PEM format SSL certificate
	SSLKey  string // PEM format SSL private key
}

// BuildTLSConfig builds tls.Config from PEM certificate
func (tc *TunnelCertificate) BuildTLSConfig() (*tls.Config, error) {
	cert, err := tls.X509KeyPair([]byte(tc.SSLCert), []byte(tc.SSLKey))
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate: %w", err)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		// Use GetCertificate callback to fix Go's TLS bug where it validates
		// certificate against ECH public_name instead of decrypted inner SNI.
		// RFC 9458 requires certificate validation against inner SNI (real domain).
		GetCertificate: func(chi *tls.ClientHelloInfo) (*tls.Certificate, error) {
			// Go's TLS automatically decrypts ECH before calling this callback.
			// chi.ServerName contains the decrypted inner SNI (real domain: *.sslip.io).
			// Return the certificate that matches the real domain, not the public_name.
			log.Printf("[TLS] GetCertificate: inner_sni=%s (expected: *.sslip.io or *.kaitu.io)", chi.ServerName)
			return &cert, nil
		},
		MinVersion: tls.VersionTLS12,
	}, nil
}

// SaveToFiles saves certificate to the specified directory
// certDir: certificate directory path
// certFileName: certificate file name (default: server-cert.pem)
// keyFileName: private key file name (default: server-key.pem)
func (tc *TunnelCertificate) SaveToFiles(certDir, certFileName, keyFileName string) error {
	// Create directory
	if err := os.MkdirAll(certDir, 0755); err != nil {
		return fmt.Errorf("failed to create cert directory: %w", err)
	}

	// Default file names
	if certFileName == "" {
		certFileName = "server-cert.pem"
	}
	if keyFileName == "" {
		keyFileName = "server-key.pem"
	}

	// Save certificate file
	certPath := filepath.Join(certDir, certFileName)
	if err := os.WriteFile(certPath, []byte(tc.SSLCert), 0644); err != nil {
		return fmt.Errorf("failed to write certificate: %w", err)
	}
	log.Printf("[Certificate] Saved certificate to: %s", certPath)

	// Save private key file
	keyPath := filepath.Join(certDir, keyFileName)
	if err := os.WriteFile(keyPath, []byte(tc.SSLKey), 0600); err != nil {
		return fmt.Errorf("failed to write private key: %w", err)
	}
	log.Printf("[Certificate] Saved private key to: %s", keyPath)

	return nil
}

// Node encapsulates node and tunnel registration management logic
type Node struct {
	CenterURL string // Center service URL
	Secret    string // Node secret
	IPv4      string // Node IPv4 address (auto-detected)
	IPv6      string // Node IPv6 address (optional)
	Country   string // Country code (auto-detected)
	Region    string // Region (optional, defaults to Country)
	Name      string // Node name (optional, defaults to IPv4)
}

// Global auth cache (30 minute validity)
var (
	globalAuthCache   *AuthCache
	authCacheDuration = 30 * time.Minute
	initAuthCacheOnce sync.Once
	authCacheEnabled  = os.Getenv("K2_AUTH_CACHE_ENABLED") == "true" // Default off, set to "true" to enable
)

// getAuthCache returns the global auth cache (lazy initialization)
func getAuthCache() *AuthCache {
	initAuthCacheOnce.Do(func() {
		globalAuthCache = NewAuthCache()
		if authCacheEnabled {
			log.Printf("[Auth] Auth cache ENABLED with %v TTL (K2_AUTH_CACHE_ENABLED=true)", authCacheDuration)
		} else {
			log.Printf("[Auth] Auth cache DISABLED (set K2_AUTH_CACHE_ENABLED=true to enable)")
		}
	})
	return globalAuthCache
}

// NewNode creates a new Node instance
// centerURL: Center service URL (e.g., https://k2.52j.me)
// secret: node secret (K2_NODE_SECRET)
func NewNode(centerURL, secret string) (*Node, error) {
	if centerURL == "" {
		return nil, fmt.Errorf("centerURL is required")
	}
	if secret == "" {
		return nil, fmt.Errorf("secret is required")
	}

	node := &Node{
		CenterURL: centerURL,
		Secret:    secret,
	}

	// Auto-detect IP address and geo information
	if err := node.DetectIP(); err != nil {
		return nil, fmt.Errorf("failed to detect IP: %w", err)
	}

	return node, nil
}

// DetectIP auto-detects the node's IP address and geo information
func (n *Node) DetectIP() error {
	// Detect IPv4 - using exported function from ip.go
	ipv4, ipData, err := GetExternalIPWithData("ipv4")
	if err != nil {
		return fmt.Errorf("failed to get IPv4: %w", err)
	}
	n.IPv4 = ipv4
	n.Country = ipData.CountryCode

	// Validate required fields: Country cannot be empty
	if n.Country == "" {
		return fmt.Errorf("failed to detect country code for IPv4: %s", n.IPv4)
	}

	// Auto-generate Region (Country-Location)
	if ipData.Location != "" {
		n.Region = slugify(ipData.CountryCode + "-" + ipData.Location)
	} else {
		n.Region = slugify(ipData.CountryCode)
	}

	log.Printf("[Node] Detected IPv4: %s, Country: %s, Region: %s", n.IPv4, n.Country, n.Region)

	// Try to detect IPv6 (optional) - using exported function from ip.go
	if ipv6, _, err := GetExternalIPWithData("ipv6"); err == nil {
		n.IPv6 = ipv6
		log.Printf("[Node] Detected IPv6: %s", n.IPv6)
	}

	// Default to using IPv4 as node name
	if n.Name == "" {
		n.Name = n.IPv4
	}

	return nil
}

// RegisterNode registers the physical node with Center
// Returns the node's SecretToken
func (n *Node) RegisterNode() (string, error) {
	if n.IPv4 == "" {
		return "", fmt.Errorf("IPv4 is required, call DetectIP() first")
	}
	if n.Country == "" {
		return "", fmt.Errorf("Country is required, call DetectIP() first")
	}
	if n.Name == "" {
		return "", fmt.Errorf("Name is required")
	}

	log.Printf("[Node] Registering node: IPv4=%s, Country=%s, Name=%s", n.IPv4, n.Country, n.Name)

	nodeReq := NodeUpsertRequest{
		Country:     n.Country,
		Region:      n.Region,
		Name:        n.Name,
		IPv6:        n.IPv6,
		SecretToken: n.Secret,
	}

	nodePath := fmt.Sprintf("/slave/nodes/%s", n.IPv4)
	nodeBody, err := n.requestWithAuth("PUT", nodePath, nodeReq)
	if err != nil {
		return "", fmt.Errorf("failed to register node: %w", err)
	}

	var nodeResp CenterResponse[NodeUpsertResponse]
	if err := json.Unmarshal(nodeBody, &nodeResp); err != nil {
		return "", fmt.Errorf("failed to parse node response: %w", err)
	}

	if nodeResp.Code != 0 || nodeResp.Data == nil {
		return "", fmt.Errorf("node registration failed: code=%d, message=%s", nodeResp.Code, nodeResp.Message)
	}

	log.Printf("[Node] Node registered successfully: IPv4=%s, Created=%v", nodeResp.Data.IPv4, nodeResp.Data.Created)
	return nodeResp.Data.SecretToken, nil
}

// Register registers node and all tunnels in one call (recommended)
// tunnels: tunnel configuration list
// Returns: RegisterResult (contains node info and all tunnel certificates)
func (n *Node) Register(tunnels []TunnelConfig) (*RegisterResult, error) {
	if n.IPv4 == "" {
		return nil, fmt.Errorf("IPv4 is required, call DetectIP() first")
	}
	if n.Country == "" {
		return nil, fmt.Errorf("Country is required, call DetectIP() first")
	}
	if n.Name == "" {
		n.Name = n.IPv4 // Default to IPv4 as name
	}

	log.Printf("[Node] Registering node with %d tunnels: IPv4=%s, Country=%s", len(tunnels), n.IPv4, n.Country)

	// Build request
	nodeReq := NodeUpsertRequest{
		Country:     n.Country,
		Region:      n.Region,
		Name:        n.Name,
		IPv6:        n.IPv6,
		SecretToken: n.Secret,
		Tunnels:     tunnels,
	}

	nodePath := fmt.Sprintf("/slave/nodes/%s", n.IPv4)
	nodeBody, err := n.requestWithAuth("PUT", nodePath, nodeReq)
	if err != nil {
		return nil, fmt.Errorf("failed to register node: %w", err)
	}

	var nodeResp CenterResponse[NodeUpsertResponse]
	if err := json.Unmarshal(nodeBody, &nodeResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if nodeResp.Code != 0 || nodeResp.Data == nil {
		return nil, fmt.Errorf("registration failed: code=%d, message=%s", nodeResp.Code, nodeResp.Message)
	}

	// Build result
	result := &RegisterResult{
		IPv4:        nodeResp.Data.IPv4,
		NodeCreated: nodeResp.Data.Created,
		Tunnels:     make(map[string]*TunnelCertificate),
	}

	// Convert tunnel certificates
	for _, t := range nodeResp.Data.Tunnels {
		result.Tunnels[t.Domain] = &TunnelCertificate{
			SSLCert: t.SSLCert,
			SSLKey:  t.SSLKey,
		}
		log.Printf("[Node] Tunnel registered: Domain=%s, Protocol=%s, Created=%v", t.Domain, t.Protocol, t.Created)
	}

	log.Printf("[Node] Registration completed: IPv4=%s, NodeCreated=%v, Tunnels=%d",
		result.IPv4, result.NodeCreated, len(result.Tunnels))

	return result, nil
}

// AddTunnel adds a tunnel to the current node
// domain: tunnel domain
// port: tunnel port
// protocol: tunnel protocol (k2wss, k2s, etc.)
// version: K2 protocol version
// hopPortStart: port hopping start port
// hopPortEnd: port hopping end port
// Tunnel name is auto-generated as: Country + random[0000,10000)
// Returns: TunnelCertificate (contains certificate info and convenience methods)
func (n *Node) AddTunnel(domain string, port int, protocol string, version int, hopPortStart, hopPortEnd int) (*TunnelCertificate, error) {
	if n.IPv4 == "" {
		return nil, fmt.Errorf("IPv4 is required, call DetectIP() or RegisterNode() first")
	}
	if domain == "" {
		return nil, fmt.Errorf("domain is required")
	}

	// Auto-generate name: Country + random[0000,10000)
	name := generateTunnelName(n.Country)

	log.Printf("[Node] Adding tunnel: Domain=%s, Port=%d, Protocol=%s, Version=%d, HopPorts=%d-%d, Name=%s",
		domain, port, protocol, version, hopPortStart, hopPortEnd, name)

	tunnelReq := TunnelUpsertRequest{
		Name:         name,
		Protocol:     protocol,
		Port:         port,
		Version:      version,
		HopPortStart: hopPortStart,
		HopPortEnd:   hopPortEnd,
	}

	tunnelPath := fmt.Sprintf("/slave/nodes/%s/tunnels/%s", n.IPv4, domain)
	tunnelBody, err := n.requestWithAuth("PUT", tunnelPath, tunnelReq)
	if err != nil {
		return nil, fmt.Errorf("failed to add tunnel: %w", err)
	}

	var tunnelResp CenterResponse[TunnelUpsertResponse]
	if err := json.Unmarshal(tunnelBody, &tunnelResp); err != nil {
		return nil, fmt.Errorf("failed to parse tunnel response: %w", err)
	}

	if tunnelResp.Code != 0 || tunnelResp.Data == nil {
		return nil, fmt.Errorf("add tunnel failed: code=%d, message=%s", tunnelResp.Code, tunnelResp.Message)
	}

	log.Printf("[Node] Tunnel added successfully: Domain=%s, Protocol=%s, TunnelID=%d, Created=%v",
		tunnelResp.Data.Domain, protocol, tunnelResp.Data.TunnelID, tunnelResp.Data.Created)

	// Return certificate wrapper object
	return &TunnelCertificate{
		SSLCert: tunnelResp.Data.SSLCert,
		SSLKey:  tunnelResp.Data.SSLKey,
	}, nil
}

// RemoveTunnel removes a tunnel from the current node
// domain: tunnel domain
func (n *Node) RemoveTunnel(domain string) error {
	if n.IPv4 == "" {
		return fmt.Errorf("IPv4 is required, call DetectIP() first")
	}
	if domain == "" {
		return fmt.Errorf("domain is required")
	}

	log.Printf("[Node] Removing tunnel: Domain=%s", domain)

	tunnelPath := fmt.Sprintf("/slave/nodes/%s/tunnels/%s", n.IPv4, domain)
	_, err := n.requestWithAuth("DELETE", tunnelPath, nil)
	if err != nil {
		return fmt.Errorf("failed to remove tunnel: %w", err)
	}

	log.Printf("[Node] Tunnel removed successfully: Domain=%s", domain)
	return nil
}

// MarkOffline marks the node as offline
// Used during graceful shutdown to notify Center the node is offline,
// preventing clients from connecting to a dead node
func (n *Node) MarkOffline() error {
	if n.IPv4 == "" {
		return fmt.Errorf("IPv4 is required, call DetectIP() first")
	}
	if n.Country == "" {
		return fmt.Errorf("Country is required, call DetectIP() first")
	}
	if n.Name == "" {
		n.Name = n.IPv4 // Default to IPv4 as name
	}

	log.Printf("[Node] Marking node offline: IPv4=%s", n.IPv4)

	isAlive := false
	nodeReq := NodeUpsertRequest{
		Country:     n.Country,
		Region:      n.Region,
		Name:        n.Name,
		IPv6:        n.IPv6,
		SecretToken: n.Secret,
		IsAlive:     &isAlive, // Mark as offline
	}

	nodePath := fmt.Sprintf("/slave/nodes/%s", n.IPv4)
	_, err := n.requestWithAuth("PUT", nodePath, nodeReq)
	if err != nil {
		return fmt.Errorf("failed to mark node offline: %w", err)
	}

	log.Printf("[Node] Node marked offline successfully: IPv4=%s", n.IPv4)
	return nil
}

// requestWithAuth sends an HTTP request with Basic Auth
func (n *Node) requestWithAuth(method, path string, body interface{}) ([]byte, error) {
	url := n.CenterURL + path

	var req *http.Request
	var err error
	if body != nil {
		jsonBody, _ := json.Marshal(body)
		req, err = http.NewRequestWithContext(context.Background(), method, url, bytes.NewBuffer(jsonBody))
	} else {
		req, err = http.NewRequestWithContext(context.Background(), method, url, nil)
	}
	if err != nil {
		return nil, err
	}

	// Set request headers
	req.Header.Set("Content-Type", "application/json")
	auth := base64.StdEncoding.EncodeToString([]byte(n.IPv4 + ":" + n.Secret))
	req.Header.Set("Authorization", "Basic "+auth)

	// Send request
	client := &http.Client{Timeout: 10 * time.Second}
	startTime := time.Now()
	resp, err := client.Do(req)
	elapsed := time.Since(startTime)
	if err != nil {
		log.Printf("[Node] Center request failed: %s %s elapsed=%v error=%v", method, path, elapsed, err)
		return nil, err
	}
	defer resp.Body.Close()

	// Read response
	var respBody bytes.Buffer
	respBody.ReadFrom(resp.Body)

	if resp.StatusCode >= 300 {
		log.Printf("[Node] Center request failed: %s %s status=%d elapsed=%v body=%s", method, path, resp.StatusCode, elapsed, respBody.String())
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, respBody.String())
	}

	log.Printf("[Node] Center request ok: %s %s status=%d elapsed=%v", method, path, resp.StatusCode, elapsed)
	return respBody.Bytes(), nil
}

// GetIPv4 returns the node's IPv4 address
func (n *Node) GetIPv4() string {
	return n.IPv4
}

// ReportStatus reports node health status to Center
// health: health metrics data
// Corresponds to Center API: POST /slave/report/status
func (n *Node) ReportStatus(health Health) error {
	if n.IPv4 == "" {
		return fmt.Errorf("IPv4 is required, call DetectIP() first")
	}

	log.Printf("[Node] Reporting: CPU=%.1f%% Mem=%.1f%% Disk=%.1f%% Conn=%d Speed=%.2f/%.2f/%.2fMbps Latency=%.2fms Loss=%.2f%% Traffic=%d/%d",
		health.CPUUsage, health.MemoryUsage, health.DiskUsage, health.Connections,
		health.NetworkSpeedMbps, health.BandwidthUpMbps, health.BandwidthDownMbps,
		health.NetworkLatencyMs, health.PacketLossPercent, health.NetworkIn, health.NetworkOut)

	req := ReportRequest{
		UpdatedAt: time.Now().Unix(),
		Health:    health,
	}

	respBody, err := n.requestWithAuth("POST", "/slave/report/status", req)
	if err != nil {
		log.Printf("[Node] Failed to report status: %v", err)
		return fmt.Errorf("failed to report status: %w", err)
	}

	log.Printf("[Node] Status reported successfully, response: %s", string(respBody))
	return nil
}

// AuthErrorCode authentication error code (aligned with Center)
type AuthErrorCode int

const (
	AuthErrorNone              AuthErrorCode = 0   // Authentication successful
	AuthErrorInvalidToken      AuthErrorCode = 401 // Token invalid or expired
	AuthErrorMembershipExpired AuthErrorCode = 402 // Membership expired
	AuthErrorUnknown           AuthErrorCode = 500 // Unknown error
)

// AuthCache authentication cache (keyed by UDID)
type AuthCache struct {
	mu    sync.RWMutex
	items map[string]*AuthCacheItem
}

// AuthCacheItem cache item (supports positive and negative caching)
type AuthCacheItem struct {
	token     string
	expiredAt time.Time
	isValid   bool          // true=auth successful, false=auth failed
	errorCode AuthErrorCode // error code when failed
}

// AuthCacheResult cache lookup result
type AuthCacheResult struct {
	Found     bool          // Whether cache was hit
	IsValid   bool          // Whether auth is valid
	ErrorCode AuthErrorCode // Error code (only meaningful when IsValid=false)
}

// NewAuthCache creates an auth cache
func NewAuthCache() *AuthCache {
	cache := &AuthCache{
		items: make(map[string]*AuthCacheItem),
	}

	// Start cleanup goroutine
	go cache.cleanup()

	return cache
}

// SetSuccess sets a successful cache entry (keyed by UDID)
func (c *AuthCache) SetSuccess(udid, token string, duration time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items[udid] = &AuthCacheItem{
		token:     token,
		expiredAt: time.Now().Add(duration),
		isValid:   true,
		errorCode: AuthErrorNone,
	}
}

// SetFailure sets a failure cache entry (negative cache, shorter duration)
func (c *AuthCache) SetFailure(udid, token string, duration time.Duration, errorCode AuthErrorCode) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items[udid] = &AuthCacheItem{
		token:     token,
		expiredAt: time.Now().Add(duration),
		isValid:   false,
		errorCode: errorCode,
	}
}

// Set sets a cache entry (backward compatible interface, defaults to success cache)
func (c *AuthCache) Set(udid, token string, duration time.Duration) {
	c.SetSuccess(udid, token, duration)
}

// GetResult gets cache result (returns detailed info)
func (c *AuthCache) GetResult(udid, token string) AuthCacheResult {
	c.mu.RLock()
	defer c.mu.RUnlock()

	item, exists := c.items[udid]
	if !exists {
		return AuthCacheResult{Found: false}
	}

	if time.Now().After(item.expiredAt) {
		return AuthCacheResult{Found: false}
	}

	if item.token != token {
		return AuthCacheResult{Found: false}
	}

	return AuthCacheResult{
		Found:     true,
		IsValid:   item.isValid,
		ErrorCode: item.errorCode,
	}
}

// Get gets cache (backward compatible interface, returns only successful caches)
func (c *AuthCache) Get(udid, token string) bool {
	result := c.GetResult(udid, token)
	return result.Found && result.IsValid
}

// cleanup periodically cleans up expired cache entries
func (c *AuthCache) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		c.mu.Lock()
		now := time.Now()
		for udid, item := range c.items {
			if now.After(item.expiredAt) {
				delete(c.items, udid)
			}
		}
		c.mu.Unlock()
	}
}

// Health health metrics
type Health struct {
	CPUUsage    float64 `json:"cpuUsage"`
	MemoryUsage float64 `json:"memoryUsage"`
	DiskUsage   float64 `json:"diskUsage"`
	NetworkIn   int64   `json:"networkIn"`
	NetworkOut  int64   `json:"networkOut"`
	Connections int     `json:"connections"`

	// Network performance metrics
	NetworkSpeedMbps  float64 `json:"networkSpeedMbps"`
	BandwidthUpMbps   float64 `json:"bandwidthUpMbps"`
	BandwidthDownMbps float64 `json:"bandwidthDownMbps"`
	NetworkLatencyMs  float64 `json:"networkLatencyMs"`
	PacketLossPercent float64 `json:"packetLossPercent"`

	// Monthly traffic tracking (for billing and load calculation)
	BillingCycleEndAt        int64 `json:"billingCycleEndAt"`        // Billing cycle end timestamp (Unix seconds)
	MonthlyTrafficLimitBytes int64 `json:"monthlyTrafficLimitBytes"` // Monthly traffic limit (bytes), 0 = unlimited
	UsedTrafficBytes         int64 `json:"usedTrafficBytes"`         // Traffic used in current cycle (bytes)
}

// ReportRequest report request
type ReportRequest struct {
	UpdatedAt int64  `json:"updatedAt"`
	Health    Health `json:"health"`
}

// DeviceCheckAuthRequest device auth request
type DeviceCheckAuthRequest struct {
	UDID  string `json:"udid,omitempty"` // Optional, extracted from JWT when token auth is used
	Token string `json:"token"`
}

// TokenAuthRequest pure token auth request
type TokenAuthRequest struct {
	Token string `json:"token"`
}

// DeviceCheckAuthResponse device auth response
type DeviceCheckAuthResponse struct {
	UserID           uint64 `json:"userID"`
	UDID             string `json:"udid"`
	TokenExpiredAt   int64  `json:"tokenExpiredAt"`
	ServiceExpiredAt int64  `json:"serviceExpiredAt"`
}

// AuthResult authentication result
type AuthResult struct {
	Success   bool          // Whether authentication was successful
	ErrorCode AuthErrorCode // Error code (0=success, 401=token invalid, 402=membership expired)
	Message   string        // Error message
}

// CheckDeviceAuth checks device authentication (for SOCKS5 and similar scenarios)
//
// Auth layer description:
// 1. Basic Auth (node auth): added via requestWithAuth, proves caller is a legitimate slave node
//   - Uses IPv4:Secret for authentication
//   - Ensures only registered nodes can call Center API
//
// 2. Device Auth (device auth): this method's business logic, validates user device UDID and Token
//   - Used to verify user devices are valid and not expired
//   - Used for SOCKS5 proxy and similar user auth scenarios
//
// Parameters:
// - udid: device UDID
// - token: device token
//
// Returns:
// - AuthResult: contains success status, error code and error message
//
// Error codes:
// - 0: auth successful
// - 401: token invalid or expired (need to re-login)
// - 402: membership expired (need to renew)
// - 500: unknown error (network issues, etc.)
func (n *Node) CheckDeviceAuth(udid, token string) AuthResult {
	startTime := time.Now()

	req := DeviceCheckAuthRequest{
		UDID:  udid,
		Token: token,
	}

	// Use requestWithAuth to send request (requires Basic Auth node authentication)
	respBody, err := n.requestWithAuth("POST", "/slave/device-check-auth", req)
	elapsed := time.Since(startTime)

	if err != nil {
		log.Printf("[Node] Auth failed: UDID=%s elapsed=%v error=%v", udid, elapsed, err)
		return AuthResult{
			Success:   false,
			ErrorCode: AuthErrorUnknown,
			Message:   fmt.Sprintf("API request failed: %v", err),
		}
	}

	var apiResp CenterResponse[DeviceCheckAuthResponse]
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		log.Printf("[Node] Auth failed: UDID=%s elapsed=%v error=parse_failed body=%s", udid, elapsed, string(respBody))
		return AuthResult{
			Success:   false,
			ErrorCode: AuthErrorUnknown,
			Message:   fmt.Sprintf("failed to parse response: %v", err),
		}
	}

	// Check Center returned error code
	if apiResp.Code != 0 {
		errorCode := AuthErrorCode(apiResp.Code)
		// Only cache clear business error codes (401, 402), treat others as unknown errors
		if errorCode != AuthErrorInvalidToken && errorCode != AuthErrorMembershipExpired {
			errorCode = AuthErrorUnknown
		}
		log.Printf("[Node] Auth failed: UDID=%s elapsed=%v code=%d message=%s", udid, elapsed, apiResp.Code, apiResp.Message)
		return AuthResult{
			Success:   false,
			ErrorCode: errorCode,
			Message:   apiResp.Message,
		}
	}

	if apiResp.Data == nil {
		log.Printf("[Node] Auth failed: UDID=%s elapsed=%v error=nil_data", udid, elapsed)
		return AuthResult{
			Success:   false,
			ErrorCode: AuthErrorUnknown,
			Message:   "nil response data",
		}
	}

	// Check UDID match
	if apiResp.Data.UDID != udid {
		log.Printf("[Node] Auth failed: UDID=%s elapsed=%v error=UDID_mismatch expected=%s got=%s", udid, elapsed, udid, apiResp.Data.UDID)
		return AuthResult{
			Success:   false,
			ErrorCode: AuthErrorInvalidToken,
			Message:   fmt.Sprintf("UDID mismatch: expected=%s, got=%s", udid, apiResp.Data.UDID),
		}
	}

	log.Printf("[Node] Auth success: UDID=%s UserID=%d elapsed=%v", udid, apiResp.Data.UserID, elapsed)
	return AuthResult{
		Success:   true,
		ErrorCode: AuthErrorNone,
		Message:   "",
	}
}

// ValidateCredentialFormat defensive auth filter: validates credential format
// If format doesn't match rules, directly rejects the auth request to avoid invalid API calls
func ValidateCredentialFormat(udid, token string) bool {
	if udid == "" || token == "" {
		log.Printf("[Auth] Format validation failed: UDID=%s Token=%s error=empty_credentials", udid, token)
		return false
	}

	return true
}

// negativeCacheDuration negative cache duration (shorter than positive cache, gives users chance to refresh token)
const negativeCacheDuration = 5 * time.Minute

// CheckAuth checks device auth with caching
// udid: device UDID
// token: device token
// Returns: whether auth was successful
//
// Uses global cache with 30 minute validity, includes defensive format validation
// Supports negative caching: only caches 401 errors (token invalid, 5 minutes)
// Does not cache 402 errors (membership expired), because users may renew at any time
func (n *Node) CheckAuth(udid, token string) bool {
	result := n.CheckAuthWithResult(udid, token)
	return result.Success
}

// CheckAuthWithResult checks device auth with caching (returns detailed result)
// udid: device UDID
// token: device token
// Returns: AuthResult containing success status, error code and error message
//
// Error code description:
// - 0: auth successful
// - 401: token invalid or expired (need to re-login) - negatively cached for 5 minutes
// - 402: membership expired (need to renew) - not cached, user may renew at any time
// - 500: unknown error (network issues, etc.) - not cached, will retry next request
//
// Environment variable: K2_AUTH_CACHE_ENABLED=true enables caching (default: off)
func (n *Node) CheckAuthWithResult(udid, token string) AuthResult {
	// 1. Defensive filter: validate format first
	if !ValidateCredentialFormat(udid, token) {
		return AuthResult{
			Success:   false,
			ErrorCode: AuthErrorInvalidToken,
			Message:   "invalid credential format",
		}
	}

	// 2. Check cache (includes negative cache) - only use when enabled
	if authCacheEnabled {
		cache := getAuthCache()
		cacheResult := cache.GetResult(udid, token)
		if cacheResult.Found {
			if cacheResult.IsValid {
				return AuthResult{Success: true, ErrorCode: AuthErrorNone}
			}
			// Hit negative cache
			log.Printf("[Auth] Cached auth failure for UDID=%s code=%d", udid, cacheResult.ErrorCode)
			return AuthResult{
				Success:   false,
				ErrorCode: cacheResult.ErrorCode,
				Message:   "cached auth failure",
			}
		}
	}

	// 3. Call Center API to validate
	result := n.CheckDeviceAuth(udid, token)

	// 4. Cache result - only when enabled
	if authCacheEnabled {
		cache := getAuthCache()
		if result.Success {
			// Success cache (30 minutes)
			cache.SetSuccess(udid, token, authCacheDuration)
		} else if result.ErrorCode == AuthErrorInvalidToken {
			// Negative cache (5 minutes) - only cache 401 errors (token invalid)
			// Don't cache 402 (membership expired), because user may renew at any time
			cache.SetFailure(udid, token, negativeCacheDuration, result.ErrorCode)
			log.Printf("[Auth] Negative cache set for UDID=%s code=%d duration=%v", udid, result.ErrorCode, negativeCacheDuration)
		}
	}
	// Note: AuthErrorMembershipExpired (402) and AuthErrorUnknown (500) are not cached

	return result
}

// Helper functions for IP detection

// isIPVersionMatch checks if an IP address matches the specified version
func isIPVersionMatch(ip string, version string) bool {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return false
	}

	if version == "ipv4" {
		return parsedIP.To4() != nil
	} else if version == "ipv6" {
		return parsedIP.To4() == nil && parsedIP.To16() != nil
	}
	return false
}

// firstNonEmpty returns the first non-empty string
func firstNonEmpty(fields ...string) string {
	for _, f := range fields {
		if f != "" {
			return f
		}
	}
	return ""
}

// slugify converts a string to slug format (lowercase, spaces replaced with hyphens)
func slugify(s string) string {
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ToLower(s)
	return s
}

// generateTunnelName generates a tunnel name: Country + random[0000,10000)
func generateTunnelName(country string) string {
	if country == "" {
		country = "XX"
	}
	// Generate a random number 0-9999, formatted as 4-digit number
	randomNum := rand.Intn(10000)
	return fmt.Sprintf("%s %04d", country, randomNum)
}

// ============================================================================
// ECH Key Management
// ============================================================================

// ECHKeyConfig represents a single ECH key configuration
type ECHKeyConfig struct {
	ConfigID   uint8  `yaml:"config_id" json:"configId"`
	PrivateKey string `yaml:"private_key" json:"privateKey"`
	PublicKey  string `yaml:"public_key" json:"publicKey"`
	KEMId      uint16 `yaml:"kem_id" json:"kemId"`
	KDFId      uint16 `yaml:"kdf_id" json:"kdfId"`
	AEADId     uint16 `yaml:"aead_id" json:"aeadId"`
	Status     string `yaml:"status" json:"status"`
	ExpiresAt  int64  `yaml:"expires_at,omitempty" json:"expiresAt,omitempty"`
}

// ECHKeysFile represents the structure of the ECH keys YAML file
type ECHKeysFile struct {
	Keys []ECHKeyConfig `yaml:"keys"`
}

// echKeysListData is the response data from Center API
type echKeysListData struct {
	Items []ECHKeyConfig `json:"items"`
}

// FetchECHKeys fetches ECH keys from Center and writes to a YAML file.
// outputPath: path to write the ECH keys file (e.g., /etc/kaitu/ech_keys.yaml)
// Returns the number of keys fetched, or error if failed.
func (n *Node) FetchECHKeys(outputPath string) (int, error) {
	if n.IPv4 == "" {
		return 0, fmt.Errorf("IPv4 is required, call DetectIP() first")
	}

	log.Printf("[Node] Fetching ECH keys from Center...")

	// Request ECH keys from Center
	respBody, err := n.requestWithAuth("GET", "/slave/ech/keys", nil)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch ECH keys: %w", err)
	}

	var echResp CenterResponse[echKeysListData]
	if err := json.Unmarshal(respBody, &echResp); err != nil {
		return 0, fmt.Errorf("failed to parse ECH keys response: %w", err)
	}

	if echResp.Code != 0 {
		return 0, fmt.Errorf("ECH keys fetch failed: code=%d, message=%s", echResp.Code, echResp.Message)
	}

	if echResp.Data == nil || len(echResp.Data.Items) == 0 {
		log.Printf("[Node] No ECH keys returned from Center")
		return 0, nil
	}

	// Convert to YAML file format
	keysFile := ECHKeysFile{
		Keys: echResp.Data.Items,
	}

	data, err := yaml.Marshal(keysFile)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal ECH keys: %w", err)
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return 0, fmt.Errorf("failed to create directory: %w", err)
	}

	// Write to file with restrictive permissions (private keys!)
	if err := os.WriteFile(outputPath, data, 0600); err != nil {
		return 0, fmt.Errorf("failed to write ECH keys file: %w", err)
	}

	log.Printf("[Node] ECH keys written to %s (%d keys)", outputPath, len(keysFile.Keys))
	return len(keysFile.Keys), nil
}

// FetchECHKeysAndNotify fetches ECH keys and sends SIGHUP to k2-slave process.
// outputPath: path to write the ECH keys file
// pidFile: path to k2-slave PID file (e.g., /var/run/k2-slave.pid)
// Returns the number of keys fetched, or error if failed.
func (n *Node) FetchECHKeysAndNotify(outputPath, pidFile string) (int, error) {
	count, err := n.FetchECHKeys(outputPath)
	if err != nil {
		return 0, err
	}

	if count > 0 && pidFile != "" {
		if err := SendSIGHUP(pidFile); err != nil {
			log.Printf("[Node] Warning: failed to send SIGHUP to k2-slave: %v", err)
			// Don't return error - keys were written successfully
		}
	}

	return count, nil
}

// SendSIGHUP sends SIGHUP signal to a process identified by its PID file.
// pidFile: path to the PID file (e.g., /var/run/k2-slave.pid)
func SendSIGHUP(pidFile string) error {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return fmt.Errorf("read pid file: %w", err)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return fmt.Errorf("parse pid: %w", err)
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find process: %w", err)
	}

	if err := process.Signal(syscall.SIGHUP); err != nil {
		return fmt.Errorf("send SIGHUP: %w", err)
	}

	log.Printf("[Node] Sent SIGHUP to k2-slave (pid=%d)", pid)
	return nil
}

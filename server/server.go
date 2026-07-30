package server

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/apernet/quic-go"
	"github.com/coder/websocket"
	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/safego"
	"github.com/kaitu-io/k2/wire"
	"github.com/kaitu-io/k2/wire/tlspad"
	smux "github.com/xtaci/smux/v2"
)

// Server is the k2s server daemon.
// It accepts QUIC and TCP-WS connections and proxies traffic.
type Server struct {
	cfg *config.ServerConfig

	quicSrv  *wire.QUICServer
	tcpwsSrv *wire.TCPWSServer
	handler  *ProxyHandler

	// TLSConfig for incoming TCP connections.
	// Set by New() from config, or directly for testing.
	TLSConfig *tls.Config

	// echConfig is the ECH config list bytes (for distribution to clients).
	echConfig []byte

	// camouflageName is this node's cover domain — the `public_name` of its own
	// ECH config. It is the ONLY outer SNI learnOuterSNI will trust; see there.
	camouflageName string

	// trustedSNIs holds outer SNIs learned from successful ECH connections.
	// Key: outer SNI domain, Value: last seen time.
	// Entries expire after trustedSNITTL. Max trustedSNIMax entries.
	trustedSNIs  map[string]time.Time
	trustedSNIMu sync.RWMutex

	// proxySem limits concurrent reverse proxy connections.
	proxySem chan struct{}

	// controlPlaneTransport is the HTTPS round-tripper used to relay
	// control-plane requests to Center (system CA validates Center's cert).
	controlPlaneTransport http.RoundTripper

	done chan struct{}
}

const (
	trustedSNITTL = 1 * time.Hour // outer SNI expiry
	trustedSNIMax = 32            // max cached outer SNIs
)

// New creates a server from config.
func New(cfg *config.ServerConfig) (*Server, error) {
	if len(cfg.ControlPlaneRoutes) == 0 {
		// Center's address is permanent; bake in the single relay route so a node
		// needs no per-node config. An explicit config overrides this default.
		cfg.ControlPlaneRoutes = defaultControlPlaneRoutes()
	}
	if err := validateControlPlaneRoutes(cfg.ControlPlaneRoutes); err != nil {
		return nil, fmt.Errorf("control_plane_routes: %w", err)
	}
	s := &Server{
		cfg:         cfg,
		quicSrv:     wire.NewQUICServer(),
		tcpwsSrv:    wire.NewTCPWSServer(),
		handler:     NewProxyHandler(),
		trustedSNIs: make(map[string]time.Time),
		proxySem:    make(chan struct{}, 128),
		done:        make(chan struct{}),
	}
	s.controlPlaneTransport = &http.Transport{
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          32,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	}

	// Auto-provisioning: unified cert + ECH + auth setup when cert_dir is set.
	if cfg.CertDir != "" {
		if err := s.setupAutoProvision(cfg); err != nil {
			return nil, err
		}
	} else if cfg.TLS.Cert != "" && cfg.TLS.Key != "" {
		// Legacy mode: static certificate from file.
		cert, err := tls.LoadX509KeyPair(cfg.TLS.Cert, cfg.TLS.Key)
		if err != nil {
			return nil, fmt.Errorf("load TLS: %w", err)
		}
		s.TLSConfig = &tls.Config{
			Certificates: []tls.Certificate{cert},
			NextProtos:   []string{"h3", "h2", "http/1.1"},
			MinVersion:   tls.VersionTLS13,
		}

		// Legacy ECH: single combined key+config file.
		s.camouflageName = cfg.ECH.PublicName
		if cfg.ECH.KeyPath != "" {
			echKeys, echConfig, err := wire.EnsureECHKey(cfg.ECH.KeyPath, cfg.ECH.PublicName)
			if err != nil {
				return nil, fmt.Errorf("ECH key: %w", err)
			}
			s.TLSConfig.EncryptedClientHelloKeys = echKeys
			s.echConfig = echConfig
		}
	}

	// Auth: build validator chain (users_file → remote_url → accept-all).
	var validators []wire.Validator
	if cfg.Auth.UsersFile != "" {
		slog.Info("server: auth chain", "mode", "users_file", "path", cfg.Auth.UsersFile)
		validators = append(validators, wire.NewUsersFileValidator(cfg.Auth.UsersFile))
	}
	if cfg.Auth.RemoteURL != "" {
		slog.Info("server: auth chain", "mode", "remote_url", "url", cfg.Auth.RemoteURL)
		// Lazy node-IPv4 resolver for the remote validator's Basic auth. Nodes
		// often don't set K2_NODE_IPV4 and must ProbePublicIP, which can't run at
		// construction time (may block / network not up yet). Resolve once, on
		// first auth — by then a client is connected so the network is live.
		var ipOnce sync.Once
		var nodeIPv4 string
		ipv4Fn := func() string {
			ipOnce.Do(func() {
				nodeIPv4 = cfg.NodeIPv4
				if nodeIPv4 == "" {
					nodeIPv4 = wire.ProbePublicIP("https://api4.ipify.org")
				}
				slog.Info("server: remote auth node identity resolved", "ipv4", nodeIPv4)
			})
			return nodeIPv4
		}
		validators = append(validators, wire.NewRemoteValidator(cfg.Auth.RemoteURL, ipv4Fn, cfg.NodeSecret))
	}

	var validator wire.Validator
	if len(validators) > 0 {
		validator = wire.ChainValidators(validators...)
		// Cache successful auths: opening the QUIC metadata path to all normal
		// clients would otherwise fire one Center request per new connection.
		validator = wire.CachingValidator(validator, cfg.Auth.CacheTTL)
	} else {
		// The accept-all fallback admits any two non-empty strings. Combined with
		// enforce that is the worst possible state: the node logs that it enforces
		// device auth while admitting everyone. Refuse to start rather than report
		// an enforcement the node does not perform.
		if cfg.Auth.EnforceAuth {
			return nil, fmt.Errorf("auth: enforce_auth is on but no validator is configured — " +
				"set auth.users_file or auth.remote_url, else the accept-all fallback admits any non-empty udid/token")
		}
		slog.Info("server: auth chain", "mode", "accept-all")
		validator = func(token, udid, mode string) wire.AuthVerdict {
			if token != "" && udid != "" {
				return wire.AuthVerdict{OK: true}
			}
			return wire.AuthVerdict{OK: false, Code: 401}
		}
	}
	// Enforcement must cover BOTH transports with the same validator: QUIC is
	// primary, TCP-WS is the fallback used exactly on the UDP-blocked/censored
	// networks that matter most. Gating only QUIC would let an attacker force
	// TCP-WS to bypass device auth entirely.
	s.quicSrv.SetValidator(validator)
	s.quicSrv.SetEnforce(cfg.Auth.EnforceAuth)
	s.tcpwsSrv.SetValidator(validator)
	s.tcpwsSrv.SetEnforce(cfg.Auth.EnforceAuth)
	slog.Info("server: device-auth enforcement", "enforce", cfg.Auth.EnforceAuth)

	return s, nil
}

// setupAutoProvision performs unified auto-provisioning of certs, ECH, and auth.
func (s *Server) setupAutoProvision(cfg *config.ServerConfig) error {
	// 1. Ensure cert_dir exists.
	if err := os.MkdirAll(cfg.CertDir, 0755); err != nil {
		return fmt.Errorf("create cert dir: %w", err)
	}

	// 2. Ensure dual cert pair (RSA + EC).
	pair, err := wire.EnsureCertPair(cfg.CertDir)
	if err != nil {
		return fmt.Errorf("cert pair: %w", err)
	}

	// 3. Ensure ECH private key.
	keyPath := filepath.Join(cfg.CertDir, "ech.key")
	privKey, err := wire.EnsureECHPrivateKey(keyPath)
	if err != nil {
		return fmt.Errorf("ECH private key: %w", err)
	}

	// 4. Fetch ECH template (Cloudflare DNS -> cache -> defaults).
	var provider wire.ECHProvider = &wire.LocalECHProvider{}
	cachePath := filepath.Join(cfg.CertDir, "ech_template.json")
	tmpl, err := wire.FetchTemplateWithFallback(provider, cachePath)
	if err != nil {
		return fmt.Errorf("ECH template: %w", err)
	}

	// 5. Generate + save new ECH config.
	echKey, _, err := wire.GenerateECHKeyFromTemplate(tmpl, privKey)
	if err != nil {
		return fmt.Errorf("ECH config generate: %w", err)
	}
	configsDir := filepath.Join(cfg.CertDir, "ech-configs")
	if err := wire.SaveECHConfig(configsDir, echKey.Config); err != nil {
		return fmt.Errorf("ECH config save: %w", err)
	}

	// 6. Load all ECH configs (supports rotation).
	allConfigs, err := wire.LoadAllECHConfigs(configsDir)
	if err != nil {
		return fmt.Errorf("ECH configs load: %w", err)
	}

	// 7. Probe target for padding (use template.PublicName, NOT cfg.ECH.PublicName).
	publicName := tmpl.PublicName
	if cfg.ECH.PublicName != "" {
		publicName = cfg.ECH.PublicName // explicit override
	}
	// Same name is the camouflage identity: the cover site we impersonate, the
	// only outer SNI real clients send, and so the only one learnOuterSNI trusts.
	s.camouflageName = publicName
	var profile *wire.TargetProfile
	if publicName != "" {
		profile, err = wire.ProbeTarget(publicName)
		if err != nil {
			slog.Error("server: probe target failed (padding disabled)", "target", publicName, "err", err)
			// Non-fatal: certs still work, just not padded.
		}
	}

	// 8. Apply padding to certs if we got a profile.
	if profile != nil && profile.CertChainLen > 0 {
		targetLen := profile.CertChainLen

		if tlspad.MeasureCertDERLen(&pair.RSA) < targetLen {
			padded, padErr := tlspad.GeneratePaddedCert(pair.RSA.PrivateKey, targetLen)
			if padErr != nil {
				slog.Error("server: RSA cert padding failed", "err", padErr)
			} else {
				pair.RSA = padded
			}
		}

		if tlspad.MeasureCertDERLen(&pair.EC) < targetLen {
			padded, padErr := tlspad.GeneratePaddedCert(pair.EC.PrivateKey, targetLen)
			if padErr != nil {
				slog.Error("server: EC cert padding failed", "err", padErr)
			} else {
				pair.EC = padded
			}
		}

		slog.Info("server: certs padded", "size", targetLen, "target", publicName, "algo", profile.KeyAlgo)
	}

	// 9. Initialize TLS config.
	s.TLSConfig = &tls.Config{
		NextProtos: []string{"h3", "h2", "http/1.1"},
		MinVersion: tls.VersionTLS13,
	}
	s.SetCertPair(pair, profile)
	s.TLSConfig.EncryptedClientHelloKeys = wire.BuildECHKeys(privKey, allConfigs)

	// Latest config for client URL distribution.
	if len(allConfigs) > 0 {
		s.echConfig = wire.BuildECHConfigList(allConfigs[:1])
	}

	// 10. Generate auth token if needed.
	authPath := filepath.Join(cfg.CertDir, "auth.json")
	auth, err := wire.EnsureAuth(authPath)
	if err != nil {
		return fmt.Errorf("auth: %w", err)
	}

	// 11. Log connection info.
	slog.Info("server: cert pins", "pins", pair.FormatPins())
	if len(allConfigs) > 0 {
		slog.Info("server: ECH", "configs", len(allConfigs), "config_id", allConfigs[0][4])
	}
	logConnectionURL(cfg, pair, s.echConfig, auth)

	return nil
}

// logConnectionURL prints and saves the k2v5:// connect URL for clients.
func logConnectionURL(cfg *config.ServerConfig, pair *wire.CertPair, echConfigList []byte, auth *wire.AuthInfo) {
	host := cfg.Host
	if host == "" {
		host = detectHost()
	}

	port := "443"
	if cfg.Listen != "" && cfg.Listen != ":443" {
		// Extract port from listen address (e.g. ":8443" or "0.0.0.0:8443")
		if idx := strings.LastIndex(cfg.Listen, ":"); idx >= 0 {
			port = cfg.Listen[idx+1:]
		}
	}

	echB64 := base64.URLEncoding.EncodeToString(echConfigList)
	pins := pair.FormatPins()

	// Route the credential through url.UserPassword rather than interpolating
	// auth.Token directly into fmt.Sprintf — matches cmd/k2s/user.go's
	// buildUserURL and keeps the raw token out of any Sprintf/Errorf arg list
	// that wire/credential_log_guard_test.go statically scans (spec §9).
	userinfo := url.UserPassword(auth.UDID, auth.Token).String()
	connectURL := fmt.Sprintf("k2v5://%s@%s:%s?ech=%s&pin=%s&insecure=1",
		userinfo, host, port, echB64, pins)

	// Add ip= raw IPv4 for dialing (separate from Host which is TLS SNI domain).
	if rawIP4 := wire.ProbePublicIP("https://api4.ipify.org"); rawIP4 != "" {
		connectURL += "&ip=" + rawIP4
		slog.Info("server: IPv4 detected", "ip", rawIP4)
	}

	// IPv6 omitted from connect URL — client doesn't handle ip6= reliably yet.
	// Re-enable when client k2v5 URL parser handles IPv6 brackets correctly.

	if cfg.HopStart > 0 && cfg.HopEnd > 0 {
		connectURL += fmt.Sprintf("&hop=%d-%d", cfg.HopStart, cfg.HopEnd)
	}

	// Do NOT slog.Info the raw connectURL: it embeds auth.Token, and
	// config.SetupLogging routes slog's default output into a rotated,
	// world-readable (0644) log file meant for support/log-upload tooling —
	// a materially wider exposure than the 0600 file below. cmd/k2s already
	// owns operator-facing display: it reads connect-url.txt back and prints
	// it directly (readConnectURL/waitForConnectURL in cmd/k2s/setup.go),
	// so this slog call was a redundant credential leak, not the only path
	// to visibility. Caught by wire/credential_log_guard_test.go (spec §9).
	urlPath := filepath.Join(cfg.CertDir, "connect-url.txt")
	if err := os.WriteFile(urlPath, []byte(connectURL+"\n"), 0600); err != nil {
		slog.Error("server: failed to save connect URL", "err", err)
	}
}

// detectHost probes for the server's public IPv4 and returns it as a sslip.io domain.
// Falls back to "YOUR_SERVER_IP.sslip.io" if probe fails.
func detectHost() string {
	if ip := wire.ProbePublicIP("https://api4.ipify.org"); ip != "" {
		return wire.FormatSslipHost(ip)
	}
	if ip := wire.ProbePublicIP("https://api6.ipify.org"); ip != "" {
		return wire.FormatSslipHost(ip)
	}
	return "YOUR_SERVER_IP.sslip.io"
}

// SetCertPair configures the server to use a dual cert pair with dynamic cert selection.
// This replaces the static Certificates in TLSConfig with a GetCertificate callback
// that selects RSA or EC cert based on the target profile's key algorithm.
func (s *Server) SetCertPair(pair *wire.CertPair, defaultProfile *wire.TargetProfile) {
	s.TLSConfig.Certificates = nil // clear static certs
	s.TLSConfig.GetCertificate = func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
		if defaultProfile != nil && defaultProfile.KeyAlgo == "rsa" {
			return &pair.RSA, nil
		}
		return &pair.EC, nil
	}
}

// Run starts the server and blocks until ctx is cancelled.
func (s *Server) Run(ctx context.Context) error {
	if s.TLSConfig == nil {
		return fmt.Errorf("TLS config required")
	}

	slog.Info("k2s server starting", "listen", s.cfg.Listen)

	// TCP listener.
	tcpLn, err := net.Listen("tcp", s.cfg.Listen)
	if err != nil {
		return fmt.Errorf("TCP listen: %w", err)
	}
	defer tcpLn.Close()

	// QUIC listener on same port (UDP).
	udpAddr, err := net.ResolveUDPAddr("udp", s.cfg.Listen)
	if err != nil {
		return fmt.Errorf("resolve UDP: %w", err)
	}
	udpConn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return fmt.Errorf("UDP listen: %w", err)
	}
	defer udpConn.Close()

	quicTransport := &quic.Transport{Conn: udpConn}
	defer quicTransport.Close()

	quicLn, err := quicTransport.Listen(s.TLSConfig.Clone(), wire.ServerQUICConfig())
	if err != nil {
		return fmt.Errorf("QUIC listen: %w", err)
	}
	defer quicLn.Close()

	// Feed QUIC listener into wire server.
	s.quicSrv.Listen(quicLn)

	var wg sync.WaitGroup

	// QUIC TCP accept loop.
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.acceptTCPLoop(ctx, s.quicSrv)
	}()

	// QUIC UDP handler.
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.quicSrv.ServeUDP(ctx, wire.UDPHandlerFunc(s.handler.HandleUDP))
	}()

	// TCP-WS TCP accept loop.
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.acceptTCPLoop(ctx, s.tcpwsSrv)
	}()

	// TCP-WS UDP handler.
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.tcpwsSrv.ServeUDP(ctx, wire.UDPHandlerFunc(s.handler.HandleUDP))
	}()

	// Raw TCP listener (TLS + routing).
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.tcpListenLoop(ctx, tcpLn)
	}()

	slog.Info("k2s server ready", "listen", s.cfg.Listen)

	// Loopback usage API for the sidecar (Plan 3-k2s, A3). Disabled when empty.
	var usageSrv *http.Server
	if s.cfg.UsageAPIListen != "" {
		usageSrv = newUsageAPI(s.handler, s.cfg.UsageAPIListen)
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := usageSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				slog.Warn("usage API server exited", "listen", s.cfg.UsageAPIListen, "err", err)
			}
		}()
		slog.Info("k2s usage API listening", "listen", s.cfg.UsageAPIListen)
	}

	// Usage reporting (Plan 3-k2s Phase B, Option D). Gated: only private nodes
	// inject both UsageReportURL and NodeSecret. Shared-pool nodes set neither →
	// no reporter starts → zero behavior change.
	if s.cfg.UsageReportURL != "" && s.cfg.NodeSecret != "" {
		ipv4 := s.cfg.NodeIPv4
		if ipv4 == "" {
			ipv4 = wire.ProbePublicIP("https://api4.ipify.org")
		}
		if ipv4 != "" {
			// Device-level accounting reporter: always on when creds exist.
			drep := newDeviceTrafficReporter(s.handler, s.cfg.UsageReportURL, ipv4, s.cfg.NodeSecret)
			safego.Go(func() { drep.Run(ctx) })
			slog.Info("k2s device traffic reporter started", "center", s.cfg.UsageReportURL)

			// LEGACY node-level cutoff reporter: explicit opt-in only — the
			// current Center /slave/usage serves no verdict (see config field doc).
			if s.cfg.NodeCutoffReport {
				rep := newUsageReporter(s.handler, s.cfg.UsageReportURL, ipv4, s.cfg.NodeSecret)
				safego.Go(func() { rep.Run(ctx) })
				slog.Info("k2s usage reporter started", "center", s.cfg.UsageReportURL)
			}
		} else {
			slog.Warn("usage reporter disabled: node IPv4 undetermined")
		}
	}

	<-ctx.Done()
	slog.Info("k2s server shutting down")

	if usageSrv != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = usageSrv.Shutdown(shutdownCtx)
		cancel()
	}

	close(s.done)
	s.handler.Close() // force-close all active proxy connections
	s.quicSrv.Close()
	s.tcpwsSrv.Close()

	wg.Wait()
	return ctx.Err()
}

// acceptTCPLoop accepts TCP proxy streams from a wire.Listener.
func (s *Server) acceptTCPLoop(ctx context.Context, ln wire.Listener) {
	for {
		conn, addr, err := ln.AcceptTCP(ctx)
		if err != nil {
			select {
			case <-ctx.Done():
				return
			case <-s.done:
				return
			default:
				slog.Error("server: AcceptTCP", "err", err)
				return
			}
		}
		slog.Debug("server: AcceptTCP got stream", "addr", addr, "remote", conn.RemoteAddr())
		// Accepting gate (Plan 3-k2s, A2): refuse NEW streams on a Center
		// stop verdict. Established connections are not force-killed.
		if !s.handler.Accepting() {
			conn.Close()
			continue
		}
		go func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("server: panic in HandleTCP", "panic", r, "addr", addr)
				}
			}()
			s.handler.HandleTCP(conn, addr)
		}()
	}
}

// tcpListenLoop accepts raw TCP connections and routes them.
func (s *Server) tcpListenLoop(ctx context.Context, ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			case <-s.done:
				return
			default:
				slog.Error("server: TCP accept", "err", err)
				return
			}
		}
		// Accepting gate (Plan 3-k2s, A2): refuse NEW raw TCP connections on
		// a Center stop verdict. Established connections are not force-killed.
		if !s.handler.Accepting() {
			conn.Close()
			continue
		}
		go func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("server: panic in handleRawTCP", "panic", r, "remote", conn.RemoteAddr())
				}
			}()
			s.handleRawTCP(conn)
		}()
	}
}

// handleRawTCP peeks the TLS ClientHello for ECH-based routing.
// ECH connections -> TLS handshake with ECH decryption -> k2v5.
// Non-ECH connections -> reverse proxy to the outer SNI website (camouflage).
func (s *Server) handleRawTCP(conn net.Conn) {
	br := bufio.NewReaderSize(conn, 4096)
	peeked, err := br.Peek(5) // TLS record header
	if err != nil {
		conn.Close()
		return
	}
	if peeked[0] != 0x16 { // Not TLS handshake
		conn.Close()
		return
	}

	recordLen := int(peeked[3])<<8 | int(peeked[4])
	hello, err := br.Peek(5 + recordLen)
	if err != nil {
		conn.Close()
		return
	}

	info, err := wire.PeekClientHello(hello)
	if err != nil {
		conn.Close()
		return
	}

	if !info.HasECH {
		// Check local_routes: forward known SNI to local services.
		if target, ok := s.matchLocalRoute(info.SNI); ok {
			select {
			case s.proxySem <- struct{}{}:
				defer func() { <-s.proxySem }()
			default:
				slog.Warn("server: reverse proxy at capacity", "sni", info.SNI)
				conn.Close()
				return
			}
			allData, _ := br.Peek(br.Buffered())
			wire.ReverseProxyWithTimeout(conn, allData, target, 5*time.Minute, 24*time.Hour)
			return
		}
		// Dynamic trusted SNI whitelist (learned from successful ECH connections).
		if s.isTrustedSNI(info.SNI) {
			select {
			case s.proxySem <- struct{}{}:
				defer func() { <-s.proxySem }()
			default:
				slog.Warn("server: reverse proxy at capacity", "sni", info.SNI)
				conn.Close()
				return
			}
			allData, _ := br.Peek(br.Buffered())
			wire.ReverseProxyWithTimeout(conn, allData, info.SNI+":443", 30*time.Second, 5*time.Minute)
			return
		}
		// Unknown SNI: reject (not in whitelist).
		slog.Debug("server: rejected unknown SNI", "sni", info.SNI, "remote", conn.RemoteAddr())
		conn.Close()
		return
	}

	// ECH -> TLS handshake with ECH decryption -> k2v5.
	wrapped := &bufferedConn{Conn: conn, br: br}
	s.handleK2V5(wrapped, info.SNI)
}

// learnOuterSNI records an outer SNI from a successful ECH connection.
// The SNI is added to trustedSNIs with the current timestamp.
// Expired entries are evicted, and the map is capped at trustedSNIMax.
//
// Only this node's own camouflage domain is admitted. The outer SNI is chosen
// by the client, and a trusted SNI makes handleRawTCP relay raw TCP to
// <SNI>:443 — a path that never passes the device-auth admission gate and,
// unlike the proxy handler, applies no isReservedIP check. Without this pin,
// anyone holding the node's ECH config (it ships inside the k2v5:// URL, so
// every current and former user has one, revoked devices included) could learn
// an arbitrary string and turn the node into an open TCP relay, reaching even
// its own private network. Pinning costs legitimate traffic nothing: uTLS
// forces a real client's outer SNI to the ECH config's public_name (spec §3.1).
func (s *Server) learnOuterSNI(sni string) {
	if sni == "" {
		return
	}
	if s.camouflageName == "" || !strings.EqualFold(sni, s.camouflageName) {
		slog.Warn("server: refusing to trust unexpected outer SNI",
			"sni", sni, "expected", s.camouflageName)
		return
	}
	s.trustedSNIMu.Lock()
	defer s.trustedSNIMu.Unlock()

	// Refresh if already present.
	if _, ok := s.trustedSNIs[sni]; ok {
		s.trustedSNIs[sni] = time.Now()
		return
	}

	// Evict expired entries.
	now := time.Now()
	for k, t := range s.trustedSNIs {
		if now.Sub(t) > trustedSNITTL {
			delete(s.trustedSNIs, k)
		}
	}

	// Enforce max size: evict oldest.
	if len(s.trustedSNIs) >= trustedSNIMax {
		var oldestKey string
		var oldestTime time.Time
		for k, t := range s.trustedSNIs {
			if oldestKey == "" || t.Before(oldestTime) {
				oldestKey = k
				oldestTime = t
			}
		}
		delete(s.trustedSNIs, oldestKey)
	}

	s.trustedSNIs[sni] = now
	slog.Info("server: learned trusted SNI", "sni", sni, "total", len(s.trustedSNIs))
}

// isTrustedSNI checks if an outer SNI is in the trusted map and not expired.
func (s *Server) isTrustedSNI(sni string) bool {
	s.trustedSNIMu.RLock()
	defer s.trustedSNIMu.RUnlock()
	t, ok := s.trustedSNIs[sni]
	if !ok {
		return false
	}
	return time.Since(t) <= trustedSNITTL
}

// matchLocalRoute checks if the SNI matches any configured local_routes.
// Supports exact match and wildcard (*.) prefix matching.
func (s *Server) matchLocalRoute(sni string) (string, bool) {
	if len(s.cfg.LocalRoutes) == 0 {
		return "", false
	}
	// Exact match.
	if target, ok := s.cfg.LocalRoutes[sni]; ok {
		return target, true
	}
	// Wildcard match: *.example.com matches foo.example.com.
	for pattern, target := range s.cfg.LocalRoutes {
		if strings.HasPrefix(pattern, "*.") {
			suffix := pattern[1:] // ".example.com"
			if strings.HasSuffix(sni, suffix) {
				return target, true
			}
		}
	}
	return "", false
}

// handleK2V5 completes TLS and routes to auth (HTTP), WebSocket tunnel, or raw smux.
// outerSNI is the plaintext SNI from the ClientHello (before ECH decryption).
func (s *Server) handleK2V5(conn net.Conn, outerSNI string) {
	tlsConn := tls.Server(conn, s.TLSConfig)
	if err := tlsConn.Handshake(); err != nil {
		slog.Error("server: TLS handshake", "err", err)
		conn.Close()
		return
	}

	// Learn outer SNI from successful ECH connections for camouflage whitelist.
	if tlsConn.ConnectionState().ECHAccepted {
		s.learnOuterSNI(outerSNI)
	}

	// Control-plane relay (antiblock Phase 1): if the ECH-decrypted inner SNI
	// matches a configured control_plane_route, HTTP-reverse-proxy this
	// connection to Center instead of treating it as a k2v5 tunnel.
	// handleK2V5 is only reached for HasECH connections (handleRawTCP:539,574-576),
	// so a plaintext-SNI probe can never hit this branch — ConnectionState().ServerName
	// here is always the ECH-decrypted inner SNI.
	if origin, ok := s.matchControlPlaneRoute(tlsConn.ConnectionState().ServerName); ok {
		select {
		case s.proxySem <- struct{}{}:
			defer func() { <-s.proxySem }()
		default:
			slog.Warn("server: control-plane proxy at capacity", "sni", tlsConn.ConnectionState().ServerName)
			tlsConn.Close()
			return
		}
		s.reverseProxyHTTP(tlsConn, origin)
		return
	}

	// Peek first bytes to distinguish HTTP (auth/WS) from smux (tunnel).
	br := bufio.NewReaderSize(tlsConn, 512)
	first, err := br.Peek(4)
	if err != nil {
		tlsConn.Close()
		return
	}

	buffConn := &bufferedConn{Conn: tlsConn, br: br}

	if isHTTPMethod(first) {
		// Only GET is a valid k2v5 client verb (WebSocket upgrade to
		// /k2v5/tunnel). The ticket-auth POST /k2v5/auth endpoint was removed —
		// device auth now rides the metadata stream on the smux/QUIC path, so any
		// other HTTP method is not a k2v5 client.
		if string(first[:3]) == "GET" {
			s.handleHTTP(buffConn)
		} else {
			buffConn.Close()
		}
		return
	}

	// Raw smux frames (backward compat, non-WS clients).
	s.tcpwsSrv.HandleConn(buffConn)
}

// handleHTTP serves a single HTTP request on a raw connection.
// Routes WebSocket upgrades to /k2v5/tunnel.
func (s *Server) handleHTTP(conn net.Conn) {
	ln := newSingleConnListener(conn)
	mux := http.NewServeMux()
	mux.HandleFunc("/k2v5/tunnel", func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true,
		})
		if err != nil {
			slog.Error("server: WS accept", "err", err)
			return
		}
		// Use a context that lives until the connection is done (not the HTTP request context).
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		wsConn := websocket.NetConn(ctx, ws, websocket.MessageBinary)
		// HandleConn creates the smux session and starts a goroutine to accept streams.
		// We must block here to keep the HTTP handler alive (and the underlying
		// hijacked connection open). Wait until the smux session ends.
		sess, err := smux.Server(wsConn, wire.DefaultSmuxConfig())
		if err != nil {
			return
		}
		s.tcpwsSrv.HandleSession(sess)
	})
	httpSrv := &http.Server{Handler: mux}
	httpSrv.Serve(ln)
}

// isHTTPMethod checks if the first bytes look like an HTTP method.
func isHTTPMethod(b []byte) bool {
	if len(b) < 3 {
		return false
	}
	switch string(b[:3]) {
	case "GET", "POS", "PUT", "DEL", "HEA", "OPT", "PAT":
		return true
	}
	return false
}

// bufferedConn wraps a net.Conn with a bufio.Reader for peeked data.
type bufferedConn struct {
	net.Conn
	br *bufio.Reader
}

func (c *bufferedConn) Read(b []byte) (int, error) {
	return c.br.Read(b)
}

// singleConnListener serves exactly one connection through http.Server.Serve.
// After the connection is returned once, Accept blocks until Close is called.
type singleConnListener struct {
	conn net.Conn
	once sync.Once
	done chan struct{}
}

func newSingleConnListener(conn net.Conn) *singleConnListener {
	return &singleConnListener{conn: conn, done: make(chan struct{})}
}

func (l *singleConnListener) Accept() (net.Conn, error) {
	var accepted bool
	l.once.Do(func() { accepted = true })
	if accepted {
		return &singleConnListenerConn{Conn: l.conn, ln: l}, nil
	}
	<-l.done
	return nil, net.ErrClosed
}

// singleConnListenerConn wraps the connection to signal the listener when closed.
// Without this, the second Accept() call blocks forever (goroutine leak).
type singleConnListenerConn struct {
	net.Conn
	ln   *singleConnListener
	once sync.Once
}

func (c *singleConnListenerConn) Close() error {
	c.once.Do(func() { c.ln.Close() })
	return c.Conn.Close()
}

func (l *singleConnListener) Close() error {
	select {
	case <-l.done:
	default:
		close(l.done)
	}
	return nil
}

func (l *singleConnListener) Addr() net.Addr {
	return l.conn.LocalAddr()
}

package server

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/apernet/quic-go"
	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/wire"
	"github.com/kaitu-io/k2/wire/tlspad"
	smux "github.com/xtaci/smux/v2"
	"github.com/coder/websocket"
)

// Server is the k2s server daemon.
// It accepts QUIC and TCP-WS connections and proxies traffic.
type Server struct {
	cfg   *config.ServerConfig
	store *wire.TicketStore

	quicSrv  *wire.QUICServer
	tcpwsSrv *wire.TCPWSServer
	handler  *ProxyHandler

	authHandler http.Handler

	// TLSConfig for incoming TCP connections.
	// Set by New() from config, or directly for testing.
	TLSConfig *tls.Config

	// echConfig is the ECH config list bytes (for distribution to clients).
	echConfig []byte

	done chan struct{}
}

// New creates a server from config.
func New(cfg *config.ServerConfig) (*Server, error) {
	store := wire.NewTicketStore()

	s := &Server{
		cfg:      cfg,
		store:    store,
		quicSrv:  wire.NewQUICServer(store),
		tcpwsSrv: wire.NewTCPWSServer(store),
		handler:  NewProxyHandler(),
		done:     make(chan struct{}),
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
		if cfg.ECH.KeyPath != "" {
			echKeys, echConfig, err := wire.EnsureECHKey(cfg.ECH.KeyPath, cfg.ECH.PublicName)
			if err != nil {
				return nil, fmt.Errorf("ECH key: %w", err)
			}
			s.TLSConfig.EncryptedClientHelloKeys = echKeys
			s.echConfig = echConfig
		}
	}

	// Auth handler — accept all non-empty credentials for now.
	s.authHandler = wire.NewAuthHandler(store, func(jwt, udid string) bool {
		return jwt != "" && udid != ""
	})

	return s, nil
}

// setupCertPair initializes the server with dual RSA+EC certs and optional padding.
// Chain: EnsureCertPair → ProbeTarget → GeneratePaddedCert → SetCertPair
func (s *Server) setupCertPair(cfg *config.ServerConfig) error {
	pair, err := wire.EnsureCertPair(cfg.CertDir)
	if err != nil {
		return fmt.Errorf("cert pair: %w", err)
	}

	// Probe target site to get cert characteristics for padding.
	var profile *wire.TargetProfile
	if cfg.ECH.PublicName != "" {
		profile, err = wire.ProbeTarget(cfg.ECH.PublicName)
		if err != nil {
			slog.Error("server: probe target failed (padding disabled)", "target", cfg.ECH.PublicName, "err", err)
			// Continue without padding — certs still work, just not padded.
		}
	}

	// Apply padding if we have a probe result.
	if profile != nil && profile.CertChainLen > 0 {
		targetLen := profile.CertChainLen

		// Pad RSA cert.
		if tlspad.MeasureCertDERLen(&pair.RSA) < targetLen {
			padded, err := tlspad.GeneratePaddedCert(pair.RSA.PrivateKey, targetLen)
			if err != nil {
				slog.Error("server: RSA cert padding failed", "err", err)
			} else {
				pair.RSA = padded
			}
		}

		// Pad EC cert.
		if tlspad.MeasureCertDERLen(&pair.EC) < targetLen {
			padded, err := tlspad.GeneratePaddedCert(pair.EC.PrivateKey, targetLen)
			if err != nil {
				slog.Error("server: EC cert padding failed", "err", err)
			} else {
				pair.EC = padded
			}
		}

		slog.Info("server: certs padded", "size", targetLen, "target", cfg.ECH.PublicName, "algo", profile.KeyAlgo)
	}

	// Initialize TLS config with GetCertificate callback.
	s.TLSConfig = &tls.Config{
		NextProtos: []string{"h3", "h2", "http/1.1"},
		MinVersion: tls.VersionTLS13,
	}
	s.SetCertPair(pair, profile)

	slog.Info("server: cert pins", "pins", pair.FormatPins())
	return nil
}

// setupECHRotation initializes ECH with split key/config storage and rotation support.
// Flow: EnsureECHPrivateKey -> FetchTemplateWithFallback -> GenerateECHKeyFromTemplate
//       -> SaveECHConfig -> LoadAllECHConfigs -> BuildECHKeys + BuildECHConfigList
func (s *Server) setupECHRotation(cfg *config.ServerConfig) error {
	// 1. Ensure persistent HPKE private key.
	keyPath := filepath.Join(cfg.CertDir, "ech.key")
	privKey, err := wire.EnsureECHPrivateKey(keyPath)
	if err != nil {
		return fmt.Errorf("ECH private key: %w", err)
	}

	// 2. Fetch ECH template from provider with fallback.
	var provider wire.ECHProvider = &wire.LocalECHProvider{}
	cachePath := filepath.Join(cfg.CertDir, "ech_template.json")
	tmpl, err := wire.FetchTemplateWithFallback(provider, cachePath)
	if err != nil {
		return fmt.Errorf("ECH template: %w", err)
	}

	// 3. Generate new config from template using our private key.
	echKey, _, err := wire.GenerateECHKeyFromTemplate(tmpl, privKey)
	if err != nil {
		return fmt.Errorf("ECH config generate: %w", err)
	}

	// 4. Save the new config to ech-configs/.
	configsDir := filepath.Join(cfg.CertDir, "ech-configs")
	if err := wire.SaveECHConfig(configsDir, echKey.Config); err != nil {
		return fmt.Errorf("ECH config save: %w", err)
	}

	// 5. Load all configs (supports rotation — multiple versions).
	allConfigs, err := wire.LoadAllECHConfigs(configsDir)
	if err != nil {
		return fmt.Errorf("ECH configs load: %w", err)
	}

	// 6. Wire into TLS config.
	if s.TLSConfig != nil {
		s.TLSConfig.EncryptedClientHelloKeys = wire.BuildECHKeys(privKey, allConfigs)
	}

	// Latest config for client URL distribution.
	if len(allConfigs) > 0 {
		s.echConfig = wire.BuildECHConfigList(allConfigs[:1])
	}

	slog.Info("server: ECH rotation", "configs", len(allConfigs), "config_id", allConfigs[0][4])

	return nil
}

// setupAutoProvision performs unified auto-provisioning of certs, ECH, and auth.
// This replaces the separate setupCertPair + setupECHRotation calls, because
// in zero-config mode the ECH template's PublicName is needed for cert padding
// probing, and there's no explicit cfg.ECH.PublicName.
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

	connectURL := fmt.Sprintf("k2v5://%s:%s@%s:%s?ech=%s&pin=%s&insecure=1",
		auth.UDID, auth.Token, host, port, echB64, pins)

	// Add ip= raw IPv4 for dialing (separate from Host which is TLS SNI domain).
	if rawIP4 := wire.ProbePublicIP("https://api4.ipify.org"); rawIP4 != "" {
		connectURL += "&ip=" + rawIP4
		slog.Info("server: IPv4 detected", "ip", rawIP4)
	}

	// Add ip6= raw IPv6 for dialing (preferred over IPv4 when available).
	if rawIP6 := wire.ProbePublicIP("https://api6.ipify.org"); rawIP6 != "" {
		connectURL += "&ip6=" + rawIP6
		slog.Info("server: IPv6 detected", "ip6", rawIP6)
	}

	slog.Info("Connect URL", "url", connectURL)

	// Save connect URL to file for k2s run smart mode to read.
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

	// Start background ticket cleanup.
	s.store.StartCleanup(5*time.Minute, s.done)

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

	quicLn, err := quicTransport.Listen(s.TLSConfig.Clone(), wire.DefaultQUICConfig())
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

	<-ctx.Done()
	slog.Info("k2s server shutting down")

	close(s.done)
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
		go s.handler.HandleTCP(conn, addr)
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
		go s.handleRawTCP(conn)
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
			allData, _ := br.Peek(br.Buffered())
			wire.ReverseProxy(conn, allData, target)
			return
		}
		// Unknown SNI: reverse proxy to the real website (camouflage).
		allData, _ := br.Peek(br.Buffered())
		wire.ReverseProxy(conn, allData, info.SNI+":443")
		return
	}

	// ECH -> TLS handshake with ECH decryption -> k2v5.
	wrapped := &bufferedConn{Conn: conn, br: br}
	s.handleK2V5(wrapped)
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
func (s *Server) handleK2V5(conn net.Conn) {
	tlsConn := tls.Server(conn, s.TLSConfig)
	if err := tlsConn.Handshake(); err != nil {
		slog.Error("server: TLS handshake", "err", err)
		conn.Close()
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
		if string(first[:3]) == "GET" {
			// Potential WebSocket upgrade or other GET — serve via HTTP handler.
			s.handleHTTP(buffConn)
			return
		}
		// POST/PUT/etc — auth handler.
		s.handleAuth(buffConn)
		return
	}

	// Raw smux frames (backward compat, non-WS clients).
	s.tcpwsSrv.HandleConn(buffConn)
}

// handleHTTP serves a single HTTP request on a raw connection.
// Routes WebSocket upgrades to /k2v5/tunnel and auth to /k2v5/auth.
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
	mux.HandleFunc("/k2v5/auth", func(w http.ResponseWriter, r *http.Request) {
		s.authHandler.ServeHTTP(w, r)
	})
	httpSrv := &http.Server{Handler: mux}
	httpSrv.Serve(ln)
}

// handleAuth serves the /k2v5/auth HTTP endpoint on a single connection.
func (s *Server) handleAuth(conn net.Conn) {
	defer conn.Close()

	br := bufio.NewReader(conn)
	req, err := http.ReadRequest(br)
	if err != nil {
		return
	}

	rec := &responseRecorder{code: 200, header: make(http.Header)}
	s.authHandler.ServeHTTP(rec, req)

	resp := &http.Response{
		StatusCode:    rec.code,
		Header:        rec.header,
		Body:          io.NopCloser(&rec.body),
		ContentLength: int64(rec.body.Len()),
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
	}
	resp.Write(conn)
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

// responseRecorder captures HTTP response for writing to a raw connection.
type responseRecorder struct {
	code   int
	header http.Header
	body   bytes.Buffer
}

func (r *responseRecorder) Header() http.Header         { return r.header }
func (r *responseRecorder) WriteHeader(code int)        { r.code = code }
func (r *responseRecorder) Write(b []byte) (int, error) { return r.body.Write(b) }

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
		return l.conn, nil
	}
	<-l.done
	return nil, net.ErrClosed
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

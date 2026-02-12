package server

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/apernet/quic-go"
	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/wire"
	smux "github.com/xtaci/smux/v2"
	"nhooyr.io/websocket"
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

	// Load TLS certificate.
	if cfg.TLS.Cert != "" && cfg.TLS.Key != "" {
		cert, err := tls.LoadX509KeyPair(cfg.TLS.Cert, cfg.TLS.Key)
		if err != nil {
			return nil, fmt.Errorf("load TLS: %w", err)
		}
		s.TLSConfig = &tls.Config{
			Certificates: []tls.Certificate{cert},
			NextProtos:   []string{"h3"},
			MinVersion:   tls.VersionTLS13,
		}
	}

	// Load ECH keys if configured.
	if cfg.ECH.KeyPath != "" {
		echKeys, echConfig, err := wire.EnsureECHKey(cfg.ECH.KeyPath, cfg.ECH.PublicName)
		if err != nil {
			return nil, fmt.Errorf("ECH key: %w", err)
		}
		if s.TLSConfig != nil {
			s.TLSConfig.EncryptedClientHelloKeys = echKeys
		}
		s.echConfig = echConfig
	}

	// Auth handler — accept all non-empty credentials for now.
	s.authHandler = wire.NewAuthHandler(store, func(jwt, udid string) bool {
		return jwt != "" && udid != ""
	})

	return s, nil
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

	log.Printf("k2s server starting on %s", s.cfg.Listen)

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

	log.Printf("k2s server ready (TCP+QUIC on %s)", s.cfg.Listen)

	<-ctx.Done()
	log.Println("k2s server shutting down...")

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
				log.Printf("server: AcceptTCP: %v", err)
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
				log.Printf("server: TCP accept: %v", err)
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
		// Non-ECH -> reverse proxy to the real website (camouflage).
		allData, _ := br.Peek(br.Buffered())
		wire.ReverseProxy(conn, allData, info.SNI+":443")
		return
	}

	// ECH -> TLS handshake with ECH decryption -> k2v5.
	wrapped := &bufferedConn{Conn: conn, br: br}
	s.handleK2V5(wrapped)
}

// handleK2V5 completes TLS and routes to auth (HTTP), WebSocket tunnel, or raw smux.
func (s *Server) handleK2V5(conn net.Conn) {
	tlsConn := tls.Server(conn, s.TLSConfig)
	if err := tlsConn.Handshake(); err != nil {
		log.Printf("server: TLS handshake: %v", err)
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
			log.Printf("server: WS accept: %v", err)
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

func (r *responseRecorder) Header() http.Header   { return r.header }
func (r *responseRecorder) WriteHeader(code int)   { r.code = code }
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

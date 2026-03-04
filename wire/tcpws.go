package wire

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	utls "github.com/metacubex/utls"
	smux "github.com/xtaci/smux/v2"
	"github.com/coder/websocket"
)

// Compile-time interface checks.
var (
	_ Dialer   = (*TCPWSClient)(nil)
	_ Listener = (*TCPWSServer)(nil)
)

// DefaultSmuxConfig returns the smux configuration for k2v5 TCP-WS transport.
func DefaultSmuxConfig() *smux.Config {
	return &smux.Config{
		KeepAliveInterval: 10 * time.Second,
		KeepAliveTimeout:  30 * time.Second,
		MaxFrameSize:      32768,  // 32 KB, matches WebSocket message limit
		MaxReceiveBuffer:  262144, // 256 KB
		MaxStreamBuffer:   65536,  // 64 KB per stream
	}
}

// smuxStreamConn wraps a smux.Stream with a buffered reader for post-header reads.
type smuxStreamConn struct {
	*smux.Stream
	br *bufio.Reader
}

// Read uses the buffered reader if present.
func (c *smuxStreamConn) Read(b []byte) (int, error) {
	if c.br != nil {
		return c.br.Read(b)
	}
	return c.Stream.Read(b)
}

// ============================================================================
// TCPWSClient — implements wire.Dialer
// ============================================================================

// TCPWSClient is the TCP-WS + smux client transport.
//
// Connection flow (production mode):
//   1. TCP dial to server:port
//   2. uTLS handshake with fingerprint (chrome/firefox/safari/random)
//   3. WebSocket upgrade to /k2v5/tunnel over TLS connection
//   4. smux session multiplexing over WebSocket binary frames
//
// The uTLS library allows fingerprint customization to evade DPI detection.
// ECH (Encrypted Client Hello) config is passed through to hide the real SNI.
//
// Test mode: SetTestConn bypasses TCP+TLS+WS and uses a direct net.Conn for smux.
type TCPWSClient struct {
	cfg          Config
	store        *TicketStore
	serverAddr   string
	tlsConfig    *tls.Config // override for testing
	testConn     net.Conn    // override for testing (bypasses TCP+TLS+WS)
	directDialer DirectDialer

	mu     sync.Mutex
	sess   *smux.Session
	udpMux *smuxUDPMux
	alloc  *SessionIDAllocator
	closed bool
}

// WireStats returns a snapshot of TCP-WS transport metrics.
// TCP-WS has no congestion control stats — only transport name is returned.
func (c *TCPWSClient) WireStats() WireStats {
	return WireStats{Transport: "tcpws"}
}

var _ StatsProvider = (*TCPWSClient)(nil)

// NewTCPWSClient creates a new TCP-WS client transport.
func NewTCPWSClient(cfg Config, store *TicketStore) *TCPWSClient {
	dialHost := cfg.Host
	if cfg.DialAddr != "" {
		dialHost = cfg.DialAddr
	}
	return &TCPWSClient{
		cfg:        cfg,
		store:      store,
		serverAddr: net.JoinHostPort(dialHost, strconv.Itoa(cfg.Port)),
	}
}

// SetTestConn sets a test connection that bypasses TCP+TLS dial.
func (c *TCPWSClient) SetTestConn(conn net.Conn) {
	c.testConn = conn
}

// SetDirectDialer sets a physical-interface-bound dialer for bypassing TUN.
func (c *TCPWSClient) SetDirectDialer(d DirectDialer) {
	c.directDialer = d
}

// connect establishes a smux session if not already connected.
func (c *TCPWSClient) connect(ctx context.Context) (*smux.Session, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil, ErrNotConnected
	}
	if c.sess != nil {
		if !c.sess.IsClosed() {
			return c.sess, nil
		}
		// Session dead (keepalive timeout, underlying conn closed) — clear and reconnect.
		slog.Debug("TCPWSClient: clearing dead smux session")
		if c.udpMux != nil {
			c.udpMux.close()
			c.udpMux = nil
		}
		c.sess = nil
		c.alloc = nil
	}

	var conn net.Conn
	if c.testConn != nil {
		// Test mode: bypass TCP+TLS+WS, direct smux
		conn = c.testConn
	} else {
		// Production mode: TCP → uTLS → WebSocket → smux
		var err error
		conn, err = c.connectTLSWebSocket(ctx)
		if err != nil {
			return nil, err
		}
	}

	sess, err := smux.Client(conn, DefaultSmuxConfig())
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("wire: smux client: %w", err)
	}

	c.sess = sess
	c.alloc = NewSessionIDAllocator()
	return sess, nil
}

// connectTLSWebSocket establishes TCP → uTLS → WebSocket connection.
// Returns a net.Conn-compatible WebSocket connection ready for smux.
func (c *TCPWSClient) connectTLSWebSocket(ctx context.Context) (net.Conn, error) {
	slog.Debug("TCPWSClient: connectTLSWebSocket start", "serverAddr", c.serverAddr, "hasDirectDialer", c.directDialer != nil)
	// 1. TCP dial (use DirectDialer if set, to bypass TUN)
	var conn net.Conn
	var err error
	if c.directDialer != nil {
		slog.Debug("TCPWSClient: using directDialer for TCP dial")
		conn, err = c.directDialer.DialContext(ctx, "tcp", c.serverAddr)
	} else {
		slog.Debug("TCPWSClient: using default dialer for TCP dial")
		var d net.Dialer
		conn, err = d.DialContext(ctx, "tcp", c.serverAddr)
	}
	if err != nil {
		slog.Debug("TCPWSClient: TCP dial failed", "serverAddr", c.serverAddr, "err", err)
		return nil, fmt.Errorf("wire: TCP dial %s: %w", c.serverAddr, err)
	}
	slog.Debug("TCPWSClient: TCP dial success", "local", conn.LocalAddr(), "remote", conn.RemoteAddr())

	// 2. uTLS handshake
	var tlsCfg *tls.Config
	var clientHelloID utls.ClientHelloID

	if c.tlsConfig != nil {
		// Test mode with override TLS config
		tlsCfg = c.tlsConfig.Clone()
		clientHelloID = utls.HelloChrome_120_PQ // Default for tests
	} else {
		// Production mode: build from Config
		tlsCfg, clientHelloID, err = BuildClientTLSConfig(c.cfg)
		if err != nil {
			conn.Close()
			return nil, err
		}
	}

	// Create uTLS config (subset of standard tls.Config fields)
	utlsCfg := &utls.Config{
		ServerName:         tlsCfg.ServerName,
		InsecureSkipVerify: tlsCfg.InsecureSkipVerify,
		MinVersion:         tlsCfg.MinVersion,
		MaxVersion:         tlsCfg.MaxVersion,
	}

	// Pass ECH config if available
	if len(tlsCfg.EncryptedClientHelloConfigList) > 0 {
		utlsCfg.EncryptedClientHelloConfigList = tlsCfg.EncryptedClientHelloConfigList
	}

	// Create uTLS connection with fingerprint
	slog.Debug("TCPWSClient: starting uTLS handshake", "sni", tlsCfg.ServerName, "fingerprint", fmt.Sprintf("%v", clientHelloID))
	uconn := utls.UClient(conn, utlsCfg, clientHelloID)
	if err := uconn.HandshakeContext(ctx); err != nil {
		conn.Close()
		slog.Debug("TCPWSClient: uTLS handshake failed", "err", err)
		return nil, fmt.Errorf("wire: uTLS handshake: %w", err)
	}
	slog.Debug("TCPWSClient: uTLS handshake success")

	// 3. WebSocket upgrade over TLS connection
	slog.Debug("TCPWSClient: starting WebSocket upgrade", "host", c.cfg.Host)
	wsConn, err := c.upgradeWebSocket(ctx, uconn)
	if err != nil {
		uconn.Close()
		slog.Debug("TCPWSClient: WebSocket upgrade failed", "err", err)
		return nil, fmt.Errorf("wire: WebSocket upgrade: %w", err)
	}
	slog.Debug("TCPWSClient: WebSocket upgrade success — connection fully established")

	return wsConn, nil
}

// upgradeWebSocket performs WebSocket upgrade to /k2v5/tunnel over an existing TLS connection.
// Returns a net.Conn-compatible wrapper around the WebSocket connection.
func (c *TCPWSClient) upgradeWebSocket(ctx context.Context, tlsConn net.Conn) (net.Conn, error) {
	// Create custom dialer that returns our existing TLS connection
	// We need to use DialTLSContext to signal that TLS is already established
	dialOpts := &websocket.DialOptions{
		HTTPClient: &http.Client{
			Transport: &http.Transport{
				DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
					// Return our already-established TLS connection
					return tlsConn, nil
				},
			},
		},
	}

	wsURL := fmt.Sprintf("wss://%s/k2v5/tunnel", c.cfg.Host)
	ws, _, err := websocket.Dial(ctx, wsURL, dialOpts)
	if err != nil {
		return nil, err
	}

	// Use background context for NetConn — caller's ctx is only for dial/handshake.
	// websocket.NetConn derives read/write contexts from ctx (netconn.go:58-59).
	// A short-lived dial context (e.g. engine wire probe's probeCtx) would kill
	// the WebSocket on cancel, destroying the cached smux session.
	// Connection lifecycle is managed by TCPWSClient.Close()/resetConnection().
	return websocket.NetConn(context.Background(), ws, websocket.MessageBinary), nil
}

// DialTCP opens a TCP proxy stream through the smux session.
func (c *TCPWSClient) DialTCP(ctx context.Context, addr string) (net.Conn, error) {
	sess, err := c.connect(ctx)
	if err != nil {
		return nil, err
	}

	stream, err := sess.OpenStream()
	if err != nil {
		return nil, fmt.Errorf("wire: open stream: %w", err)
	}

	if err := WriteStreamHeader(stream, StreamTypeTCP, addr); err != nil {
		stream.Close()
		return nil, fmt.Errorf("wire: write stream header: %w", err)
	}

	br := bufio.NewReader(stream)
	ok, errCode, errMsg, err := ReadStreamResponse(br)
	if err != nil {
		stream.Close()
		return nil, fmt.Errorf("wire: read response: %w", err)
	}
	if !ok {
		stream.Close()
		if errCode > 0 {
			return nil, &StreamError{Code: errCode, Message: errMsg}
		}
		return nil, fmt.Errorf("%w: %s", ErrStreamRejected, errMsg)
	}

	return &smuxStreamConn{Stream: stream, br: br}, nil
}

// DialUDP opens a UDP proxy session through a shared smux UDP stream.
// Returns a PacketConn backed by SmuxUDPFrame messages.
func (c *TCPWSClient) DialUDP(ctx context.Context, addr string) (net.PacketConn, error) {
	slog.Debug("TCPWSClient.DialUDP", "addr", addr)
	sess, err := c.connect(ctx)
	if err != nil {
		slog.Warn("TCPWSClient.DialUDP: connect failed", "addr", addr, "err", err)
		return nil, err
	}

	// Ensure shared UDP stream and mux exist.
	// Check alloc first — nil means resetConnection() ran (network change).
	c.mu.Lock()
	if c.alloc == nil {
		c.mu.Unlock()
		return nil, ErrNotConnected
	}
	if c.udpMux == nil {
		stream, err := sess.OpenStream()
		if err != nil {
			c.mu.Unlock()
			return nil, fmt.Errorf("wire: open UDP stream: %w", err)
		}
		if err := WriteStreamHeader(stream, StreamTypeUDP, ""); err != nil {
			stream.Close()
			c.mu.Unlock()
			return nil, fmt.Errorf("wire: write UDP stream header: %w", err)
		}
		c.udpMux = newSmuxUDPMux(stream)
	}
	mux := c.udpMux
	alloc := c.alloc
	c.mu.Unlock()

	sessionID := alloc.Next()
	readCh := mux.register(sessionID)
	slog.Debug("TCPWSClient.DialUDP: session ready", "addr", addr, "sessionID", sessionID)

	return &smuxUDPConn{
		mux:        mux,
		sessionID:  sessionID,
		targetAddr: addr,
		readCh:     readCh,
		done:       make(chan struct{}),
	}, nil
}

// resetConnection closes cached session resources without marking client as closed.
// After reset, the next DialTCP/DialUDP will establish a fresh connection.
func (c *TCPWSClient) resetConnection() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.udpMux != nil {
		c.udpMux.close()
		c.udpMux = nil
	}
	if c.sess != nil {
		if err := c.sess.Close(); err != nil {
			slog.Debug("TCPWSClient: resetConnection close error", "err", err)
		}
		c.sess = nil
	}
	c.alloc = nil
	// NOTE: do NOT set c.closed = true — client must remain usable
}

// Close closes the smux session and underlying connection.
func (c *TCPWSClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}
	c.closed = true

	if c.udpMux != nil {
		c.udpMux.close()
	}
	if c.sess != nil {
		return c.sess.Close()
	}
	return nil
}

// ============================================================================
// TCPWSServer — implements wire.Listener
// ============================================================================

// TCPWSServer is the TCP-WS + smux server transport.
type TCPWSServer struct {
	store *TicketStore

	mu    sync.Mutex
	tcpCh chan tcpAcceptResult
	udpH  UDPHandler
	done  chan struct{}
}

// NewTCPWSServer creates a new TCP-WS server transport.
func NewTCPWSServer(store *TicketStore) *TCPWSServer {
	return &TCPWSServer{
		store: store,
		tcpCh: make(chan tcpAcceptResult, 64),
		done:  make(chan struct{}),
	}
}

// HandleConn processes a new TCP-WS client connection.
// Creates a smux session and starts accepting streams.
// Called by the server daemon for each authenticated client connection.
func (s *TCPWSServer) HandleConn(conn net.Conn) {
	sess, err := smux.Server(conn, DefaultSmuxConfig())
	if err != nil {
		conn.Close()
		return
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("TCPWSServer: panic in handleSession", "panic", r)
			}
		}()
		s.handleSession(sess)
	}()
}

// HandleSession processes streams from a smux session, blocking until it ends.
// Used by handleHTTP for WebSocket connections where the caller must block
// to keep the HTTP handler (and hijacked connection) alive.
func (s *TCPWSServer) HandleSession(sess *smux.Session) {
	s.handleSession(sess)
}

func (s *TCPWSServer) handleSession(sess *smux.Session) {
	defer sess.Close()
	for {
		stream, err := sess.AcceptStream()
		if err != nil {
			return
		}
		go func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("TCPWSServer: panic in handleStream", "panic", r)
				}
			}()
			s.handleStream(stream)
		}()
	}
}

func (s *TCPWSServer) handleStream(stream *smux.Stream) {
	br := bufio.NewReader(stream)

	streamType, addr, err := ReadStreamHeader(br)
	if err != nil {
		stream.Close()
		return
	}

	switch streamType {
	case StreamTypeTCP:
		sc := &smuxStreamConn{Stream: stream, br: br}
		s.sendTCP(tcpAcceptResult{conn: sc, addr: addr})
	case StreamTypeUDP:
		s.handleUDPStream(stream, br)
	default:
		stream.Close()
	}
}

func (s *TCPWSServer) handleUDPStream(stream *smux.Stream, br *bufio.Reader) {
	mux := &serverSmuxWriter{stream: stream}
	sessions := make(map[uint32]*smuxServerUDPConn)

	for {
		frame, err := ReadSmuxUDPFrame(br)
		if err != nil {
			// Close all sessions
			for _, sess := range sessions {
				sess.close()
			}
			return
		}

		sess, ok := sessions[frame.SessionID]
		if !ok {
			if len(sessions) >= maxServerUDPSessions {
				continue // at capacity, drop frame
			}
			sess = &smuxServerUDPConn{
				writer:    mux,
				sessionID: frame.SessionID,
				addr:      frame.Addr,
				readCh:    make(chan []byte, 32),
				done:      make(chan struct{}),
			}
			sessions[frame.SessionID] = sess

			s.mu.Lock()
			handler := s.udpH
			s.mu.Unlock()
			if handler != nil {
				addr := frame.Addr
				go func() {
					defer func() {
						if r := recover(); r != nil {
							slog.Error("TCPWSServer: panic in HandleUDP", "panic", r, "addr", addr)
						}
					}()
					handler.HandleUDP(sess, addr)
				}()
			}
		}

		sess.deliver(frame.Payload)
	}
}

func (s *TCPWSServer) sendTCP(result tcpAcceptResult) {
	select {
	case s.tcpCh <- result:
	case <-s.done:
		if result.conn != nil {
			result.conn.Close()
		}
	}
}

// AcceptTCP accepts the next TCP proxy stream from any connected client.
func (s *TCPWSServer) AcceptTCP(ctx context.Context) (net.Conn, string, error) {
	select {
	case result := <-s.tcpCh:
		return result.conn, result.addr, result.err
	case <-ctx.Done():
		return nil, "", ctx.Err()
	case <-s.done:
		return nil, "", net.ErrClosed
	}
}

// ServeUDP sets the UDP handler and blocks until ctx is done.
func (s *TCPWSServer) ServeUDP(ctx context.Context, handler UDPHandler) error {
	s.mu.Lock()
	s.udpH = handler
	s.mu.Unlock()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.done:
		return net.ErrClosed
	}
}

// Close stops the server.
func (s *TCPWSServer) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	select {
	case <-s.done:
		return nil
	default:
		close(s.done)
	}
	return nil
}

// ============================================================================
// Client-side shared UDP stream mux
// ============================================================================

type smuxUDPMux struct {
	stream   *smux.Stream
	br       *bufio.Reader
	writeMu  sync.Mutex
	mu       sync.Mutex
	sessions map[uint32]chan *UDPFrame
	done     chan struct{}
	started  bool
}

func newSmuxUDPMux(stream *smux.Stream) *smuxUDPMux {
	return &smuxUDPMux{
		stream:   stream,
		br:       bufio.NewReader(stream),
		sessions: make(map[uint32]chan *UDPFrame),
		done:     make(chan struct{}),
	}
}

func (m *smuxUDPMux) start() {
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return
	}
	m.started = true
	m.mu.Unlock()
	go m.readLoop()
}

func (m *smuxUDPMux) readLoop() {
	defer func() {
		select {
		case <-m.done:
		default:
			close(m.done)
		}
	}()
	for {
		frame, err := ReadSmuxUDPFrame(m.br)
		if err != nil {
			slog.Warn("smuxUDPMux: readLoop exiting", "err", err)
			return
		}
		m.mu.Lock()
		ch, ok := m.sessions[frame.SessionID]
		m.mu.Unlock()
		if ok {
			select {
			case ch <- frame:
			default:
			}
		}
	}
}

func (m *smuxUDPMux) writeFrame(sessionID uint32, addr string, payload []byte) error {
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	return WriteSmuxUDPFrame(m.stream, sessionID, addr, payload)
}

func (m *smuxUDPMux) register(sessionID uint32) chan *UDPFrame {
	ch := make(chan *UDPFrame, 32)
	m.mu.Lock()
	m.sessions[sessionID] = ch
	m.mu.Unlock()
	m.start()
	return ch
}

func (m *smuxUDPMux) unregister(sessionID uint32) {
	m.mu.Lock()
	delete(m.sessions, sessionID)
	m.mu.Unlock()
}

func (m *smuxUDPMux) close() {
	select {
	case <-m.done:
	default:
		close(m.done)
	}
	m.stream.Close()
}

// ============================================================================
// Client-side PacketConn over smux UDP stream
// ============================================================================

type smuxUDPConn struct {
	mux        *smuxUDPMux
	sessionID  uint32
	targetAddr string
	readCh     chan *UDPFrame
	done       chan struct{}
	closeOnce  sync.Once
	rd         readDeadline
}

var _ net.PacketConn = (*smuxUDPConn)(nil)

func (c *smuxUDPConn) ReadFrom(b []byte) (int, net.Addr, error) {
	timerCh, timer, expired := c.rd.makeTimer()
	if expired != nil {
		return 0, nil, expired
	}
	if timer != nil {
		defer timer.Stop()
	}
	select {
	case frame := <-c.readCh:
		n := copy(b, frame.Payload)
		addr, _ := net.ResolveUDPAddr("udp", c.targetAddr)
		return n, addr, nil
	case <-c.done:
		return 0, nil, net.ErrClosed
	case <-c.mux.done:
		return 0, nil, net.ErrClosed
	case <-timerCh:
		slog.Warn("smuxUDPConn: ReadFrom timeout", "sessionID", c.sessionID,
			"target", c.targetAddr)
		return 0, nil, os.ErrDeadlineExceeded
	}
}

func (c *smuxUDPConn) WriteTo(b []byte, _ net.Addr) (int, error) {
	if err := c.mux.writeFrame(c.sessionID, c.targetAddr, b); err != nil {
		if errors.Is(err, io.ErrClosedPipe) || errors.Is(err, net.ErrClosed) {
			slog.Debug("smuxUDPConn: WriteTo failed (closed)", "sessionID", c.sessionID,
				"target", c.targetAddr, "err", err)
		} else {
			slog.Warn("smuxUDPConn: WriteTo failed", "sessionID", c.sessionID,
				"target", c.targetAddr, "len", len(b), "err", err)
		}
		return 0, err
	}
	slog.Debug("smuxUDPConn: WriteTo ok", "sessionID", c.sessionID,
		"target", c.targetAddr, "len", len(b))
	return len(b), nil
}

func (c *smuxUDPConn) Close() error {
	c.closeOnce.Do(func() {
		close(c.done)
		c.mux.unregister(c.sessionID)
	})
	return nil
}

func (c *smuxUDPConn) LocalAddr() net.Addr                    { return c.mux.stream.LocalAddr() }
func (c *smuxUDPConn) SetDeadline(t time.Time) error          { c.rd.set(t); return nil }
func (c *smuxUDPConn) SetReadDeadline(t time.Time) error      { c.rd.set(t); return nil }
func (c *smuxUDPConn) SetWriteDeadline(time.Time) error       { return nil }

// ============================================================================
// Server-side smux UDP stream writer (shared, mutex-protected)
// ============================================================================

type serverSmuxWriter struct {
	stream  *smux.Stream
	writeMu sync.Mutex
}

func (w *serverSmuxWriter) writeFrame(sessionID uint32, addr string, payload []byte) error {
	w.writeMu.Lock()
	defer w.writeMu.Unlock()
	return WriteSmuxUDPFrame(w.stream, sessionID, addr, payload)
}

// ============================================================================
// Server-side PacketConn for smux UDP sessions
// ============================================================================

type smuxServerUDPConn struct {
	writer    *serverSmuxWriter
	sessionID uint32
	addr      string
	readCh    chan []byte
	done      chan struct{}
	closeOnce sync.Once
	rd        readDeadline
}

var _ net.PacketConn = (*smuxServerUDPConn)(nil)

func (c *smuxServerUDPConn) deliver(payload []byte) {
	select {
	case c.readCh <- payload:
	case <-c.done:
	}
}

func (c *smuxServerUDPConn) ReadFrom(b []byte) (int, net.Addr, error) {
	timerCh, timer, expired := c.rd.makeTimer()
	if expired != nil {
		return 0, nil, expired
	}
	if timer != nil {
		defer timer.Stop()
	}
	select {
	case payload := <-c.readCh:
		n := copy(b, payload)
		addr, _ := net.ResolveUDPAddr("udp", c.addr)
		return n, addr, nil
	case <-c.done:
		return 0, nil, net.ErrClosed
	case <-timerCh:
		return 0, nil, os.ErrDeadlineExceeded
	}
}

func (c *smuxServerUDPConn) WriteTo(b []byte, _ net.Addr) (int, error) {
	// Empty addr for responses
	if err := c.writer.writeFrame(c.sessionID, "", b); err != nil {
		return 0, err
	}
	return len(b), nil
}

func (c *smuxServerUDPConn) close() {
	c.closeOnce.Do(func() { close(c.done) })
}

func (c *smuxServerUDPConn) Close() error {
	c.close()
	return nil
}

func (c *smuxServerUDPConn) LocalAddr() net.Addr                    { return c.writer.stream.LocalAddr() }
func (c *smuxServerUDPConn) SetDeadline(t time.Time) error          { c.rd.set(t); return nil }
func (c *smuxServerUDPConn) SetReadDeadline(t time.Time) error      { c.rd.set(t); return nil }
func (c *smuxServerUDPConn) SetWriteDeadline(time.Time) error       { return nil }

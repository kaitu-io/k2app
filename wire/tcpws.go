package wire

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"sync"
	"time"

	smux "github.com/xtaci/smux/v2"
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
type TCPWSClient struct {
	cfg        Config
	store      *TicketStore
	serverAddr string
	tlsConfig  *tls.Config // override for testing
	testConn   net.Conn    // override for testing (bypasses TCP+TLS)

	mu     sync.Mutex
	sess   *smux.Session
	udpMux *smuxUDPMux
	alloc  *SessionIDAllocator
	closed bool
}

// NewTCPWSClient creates a new TCP-WS client transport.
func NewTCPWSClient(cfg Config, store *TicketStore) *TCPWSClient {
	return &TCPWSClient{
		cfg:        cfg,
		store:      store,
		serverAddr: fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
	}
}

// SetTestConn sets a test connection that bypasses TCP+TLS dial.
func (c *TCPWSClient) SetTestConn(conn net.Conn) {
	c.testConn = conn
}

// connect establishes a smux session if not already connected.
func (c *TCPWSClient) connect(ctx context.Context) (*smux.Session, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil, ErrNotConnected
	}
	if c.sess != nil {
		return c.sess, nil
	}

	var conn net.Conn
	if c.testConn != nil {
		conn = c.testConn
	} else {
		// TCP dial
		var d net.Dialer
		var err error
		conn, err = d.DialContext(ctx, "tcp", c.serverAddr)
		if err != nil {
			return nil, fmt.Errorf("wire: TCP dial %s: %w", c.serverAddr, err)
		}

		// TLS handshake
		var tlsCfg *tls.Config
		if c.tlsConfig != nil {
			tlsCfg = c.tlsConfig.Clone()
		} else {
			tlsCfg, _, err = BuildClientTLSConfig(c.cfg)
			if err != nil {
				conn.Close()
				return nil, err
			}
		}
		tlsConn := tls.Client(conn, tlsCfg)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			conn.Close()
			return nil, fmt.Errorf("wire: TLS handshake: %w", err)
		}
		conn = tlsConn
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
	ok, errMsg, err := ReadStreamResponse(br)
	if err != nil {
		stream.Close()
		return nil, fmt.Errorf("wire: read response: %w", err)
	}
	if !ok {
		stream.Close()
		return nil, fmt.Errorf("%w: %s", ErrStreamRejected, errMsg)
	}

	return &smuxStreamConn{Stream: stream, br: br}, nil
}

// DialUDP opens a UDP proxy session through a shared smux UDP stream.
// Returns a PacketConn backed by SmuxUDPFrame messages.
func (c *TCPWSClient) DialUDP(ctx context.Context, addr string) (net.PacketConn, error) {
	sess, err := c.connect(ctx)
	if err != nil {
		return nil, err
	}

	// Ensure shared UDP stream and mux exist
	c.mu.Lock()
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

	return &smuxUDPConn{
		mux:        mux,
		sessionID:  sessionID,
		targetAddr: addr,
		readCh:     readCh,
		done:       make(chan struct{}),
	}, nil
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
		return
	}
	go s.handleSession(sess)
}

func (s *TCPWSServer) handleSession(sess *smux.Session) {
	defer sess.Close()
	for {
		stream, err := sess.AcceptStream()
		if err != nil {
			return
		}
		go s.handleStream(stream)
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
				go handler.HandleUDP(sess, frame.Addr)
			}
		}

		sess.deliver(frame.Payload)
	}
}

func (s *TCPWSServer) sendTCP(result tcpAcceptResult) {
	select {
	case s.tcpCh <- result:
	case <-s.done:
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
	for {
		frame, err := ReadSmuxUDPFrame(m.br)
		if err != nil {
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
}

var _ net.PacketConn = (*smuxUDPConn)(nil)

func (c *smuxUDPConn) ReadFrom(b []byte) (int, net.Addr, error) {
	select {
	case frame := <-c.readCh:
		n := copy(b, frame.Payload)
		addr, _ := net.ResolveUDPAddr("udp", c.targetAddr)
		return n, addr, nil
	case <-c.done:
		return 0, nil, net.ErrClosed
	}
}

func (c *smuxUDPConn) WriteTo(b []byte, _ net.Addr) (int, error) {
	if err := c.mux.writeFrame(c.sessionID, c.targetAddr, b); err != nil {
		return 0, err
	}
	return len(b), nil
}

func (c *smuxUDPConn) Close() error {
	c.closeOnce.Do(func() {
		close(c.done)
		c.mux.unregister(c.sessionID)
	})
	return nil
}

func (c *smuxUDPConn) LocalAddr() net.Addr             { return c.mux.stream.LocalAddr() }
func (c *smuxUDPConn) SetDeadline(time.Time) error      { return nil }
func (c *smuxUDPConn) SetReadDeadline(time.Time) error   { return nil }
func (c *smuxUDPConn) SetWriteDeadline(time.Time) error  { return nil }

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
}

var _ net.PacketConn = (*smuxServerUDPConn)(nil)

func (c *smuxServerUDPConn) deliver(payload []byte) {
	select {
	case c.readCh <- payload:
	case <-c.done:
	}
}

func (c *smuxServerUDPConn) ReadFrom(b []byte) (int, net.Addr, error) {
	select {
	case payload := <-c.readCh:
		n := copy(b, payload)
		addr, _ := net.ResolveUDPAddr("udp", c.addr)
		return n, addr, nil
	case <-c.done:
		return 0, nil, net.ErrClosed
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

func (c *smuxServerUDPConn) LocalAddr() net.Addr             { return c.writer.stream.LocalAddr() }
func (c *smuxServerUDPConn) SetDeadline(time.Time) error      { return nil }
func (c *smuxServerUDPConn) SetReadDeadline(time.Time) error   { return nil }
func (c *smuxServerUDPConn) SetWriteDeadline(time.Time) error  { return nil }

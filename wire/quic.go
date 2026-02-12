package wire

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"math/rand"
	"net"
	"sync"
	"time"

	"github.com/apernet/quic-go"
)

// Compile-time interface checks.
var (
	_ Dialer   = (*QUICClient)(nil)
	_ Listener = (*QUICServer)(nil)
)

// DefaultQUICConfig returns the QUIC configuration for k2v5 transport.
func DefaultQUICConfig() *quic.Config {
	return &quic.Config{
		EnableDatagrams:   true,
		MaxIdleTimeout:    30 * time.Second,
		KeepAlivePeriod:   10 * time.Second,
		MaxIncomingStreams: 256,
	}
}

// quicStreamConn wraps a quic.Stream to satisfy net.Conn.
type quicStreamConn struct {
	*quic.Stream
	local  net.Addr
	remote net.Addr
	br     *bufio.Reader // set after parsing stream header
}

func (c *quicStreamConn) LocalAddr() net.Addr  { return c.local }
func (c *quicStreamConn) RemoteAddr() net.Addr { return c.remote }

// Read uses the buffered reader if present (to consume any data
// buffered during header parsing), otherwise reads from the stream directly.
func (c *quicStreamConn) Read(b []byte) (int, error) {
	if c.br != nil {
		return c.br.Read(b)
	}
	return c.Stream.Read(b)
}

// ============================================================================
// QUICClient — implements wire.Dialer
// ============================================================================

// QUICClient is the QUIC/H3 client transport.
type QUICClient struct {
	cfg        Config
	store      *TicketStore
	serverAddr string
	hopStart   int
	hopEnd     int
	tlsConfig  *tls.Config // override for testing (bypasses BuildClientTLSConfig)

	mu        sync.Mutex
	conn      *quic.Conn
	transport *quic.Transport
	alloc     *SessionIDAllocator
	udpMux    *quicUDPMux
	closed    bool
}

// NewQUICClient creates a new QUIC client transport.
func NewQUICClient(cfg Config, store *TicketStore) *QUICClient {
	return &QUICClient{
		cfg:        cfg,
		store:      store,
		serverAddr: fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		hopStart:   cfg.HopStart,
		hopEnd:     cfg.HopEnd,
	}
}

// hopEnabled returns true if port hopping is configured.
func (c *QUICClient) hopEnabled() bool {
	return c.hopStart > 0 && c.hopEnd > 0 && c.hopStart < c.hopEnd
}

// hopPort returns a random port in the hop range.
func (c *QUICClient) hopPort() int {
	return c.hopStart + rand.Intn(c.hopEnd-c.hopStart)
}

// connect establishes a QUIC connection if not already connected.
func (c *QUICClient) connect(ctx context.Context) (*quic.Conn, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil, ErrNotConnected
	}
	if c.conn != nil {
		return c.conn, nil
	}

	addr := c.serverAddr
	if c.hopEnabled() {
		addr = fmt.Sprintf("%s:%d", c.cfg.Host, c.hopPort())
	}

	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("wire: resolve %s: %w", addr, err)
	}

	udpConn, err := net.ListenUDP("udp", &net.UDPAddr{})
	if err != nil {
		return nil, fmt.Errorf("wire: listen UDP: %w", err)
	}

	transport := &quic.Transport{Conn: udpConn}

	var tlsCfg *tls.Config
	if c.tlsConfig != nil {
		tlsCfg = c.tlsConfig.Clone()
	} else {
		tlsCfg, _, err = BuildClientTLSConfig(c.cfg)
		if err != nil {
			transport.Close()
			return nil, err
		}
	}
	if len(tlsCfg.NextProtos) == 0 {
		tlsCfg.NextProtos = []string{"h3"}
	}

	conn, err := transport.Dial(ctx, udpAddr, tlsCfg, DefaultQUICConfig())
	if err != nil {
		transport.Close()
		return nil, fmt.Errorf("wire: QUIC dial: %w", err)
	}

	c.conn = conn
	c.transport = transport
	c.alloc = NewSessionIDAllocator()
	c.udpMux = newQuicUDPMux(conn)

	return conn, nil
}

// DialTCP opens a TCP proxy stream through the QUIC connection.
// Writes H3 frame type + stream header, reads response, returns the stream.
func (c *QUICClient) DialTCP(ctx context.Context, addr string) (net.Conn, error) {
	conn, err := c.connect(ctx)
	if err != nil {
		return nil, err
	}

	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		return nil, fmt.Errorf("wire: open stream: %w", err)
	}

	// Write H3 custom frame type marker + TCP stream header
	if err := WriteH3ProxyFrameType(stream); err != nil {
		stream.Close()
		return nil, fmt.Errorf("wire: write H3 frame type: %w", err)
	}
	if err := WriteStreamHeader(stream, StreamTypeTCP, addr); err != nil {
		stream.Close()
		return nil, fmt.Errorf("wire: write stream header: %w", err)
	}

	// Read response
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

	return &quicStreamConn{
		Stream: stream,
		local:  conn.LocalAddr(),
		remote: conn.RemoteAddr(),
		br:     br,
	}, nil
}

// DialUDP opens a UDP proxy session through QUIC datagrams.
// Returns a PacketConn backed by QUIC datagrams with a unique session ID.
func (c *QUICClient) DialUDP(ctx context.Context, addr string) (net.PacketConn, error) {
	conn, err := c.connect(ctx)
	if err != nil {
		return nil, err
	}

	sessionID := c.alloc.Next()
	readCh := c.udpMux.register(sessionID)

	return &quicUDPConn{
		qconn:      conn,
		sessionID:  sessionID,
		targetAddr: addr,
		mux:        c.udpMux,
		readCh:     readCh,
		done:       make(chan struct{}),
	}, nil
}

// Close closes the QUIC connection and underlying transport.
func (c *QUICClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}
	c.closed = true

	var firstErr error
	if c.udpMux != nil {
		c.udpMux.close()
	}
	if c.conn != nil {
		if err := c.conn.CloseWithError(0, "client close"); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if c.transport != nil {
		if err := c.transport.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// ============================================================================
// QUICServer — implements wire.Listener
// ============================================================================

type tcpAcceptResult struct {
	conn net.Conn
	addr string
	err  error
}

// QUICServer is the QUIC/H3 server transport.
type QUICServer struct {
	store *TicketStore

	mu    sync.Mutex
	ln    *quic.Listener
	tcpCh chan tcpAcceptResult
	udpH  UDPHandler
	done  chan struct{}
}

// NewQUICServer creates a new QUIC server transport.
func NewQUICServer(store *TicketStore) *QUICServer {
	return &QUICServer{
		store: store,
		tcpCh: make(chan tcpAcceptResult, 64),
		done:  make(chan struct{}),
	}
}

// Listen starts accepting QUIC connections from the given listener.
func (s *QUICServer) Listen(ln *quic.Listener) {
	s.mu.Lock()
	s.ln = ln
	s.mu.Unlock()
	go s.acceptLoop()
}

func (s *QUICServer) acceptLoop() {
	for {
		conn, err := s.ln.Accept(context.Background())
		if err != nil {
			select {
			case <-s.done:
				return
			default:
			}
			s.sendTCP(tcpAcceptResult{err: fmt.Errorf("wire: accept: %w", err)})
			return
		}
		go s.handleConn(conn)
	}
}

func (s *QUICServer) handleConn(conn *quic.Conn) {
	// Accept streams (TCP proxy)
	go func() {
		for {
			stream, err := conn.AcceptStream(context.Background())
			if err != nil {
				return
			}
			go s.handleStream(conn, stream)
		}
	}()

	// Read datagrams (UDP proxy) — blocks until connection closes
	s.handleDatagrams(conn)
}

func (s *QUICServer) handleStream(qconn *quic.Conn, stream *quic.Stream) {
	br := bufio.NewReader(stream)

	// Read H3 frame type marker
	frameType, _, err := ReadVarint(br)
	if err != nil {
		stream.Close()
		return
	}
	if frameType != H3FrameTypeK2Proxy {
		stream.Close()
		return
	}

	// Read stream header
	streamType, addr, err := ReadStreamHeader(br)
	if err != nil {
		stream.Close()
		return
	}

	if streamType != StreamTypeTCP {
		stream.Close()
		return
	}

	sc := &quicStreamConn{
		Stream: stream,
		local:  qconn.LocalAddr(),
		remote: qconn.RemoteAddr(),
		br:     br,
	}
	s.sendTCP(tcpAcceptResult{conn: sc, addr: addr})
}

func (s *QUICServer) handleDatagrams(conn *quic.Conn) {
	sessions := make(map[uint32]*serverUDPConn)
	var mu sync.Mutex

	for {
		data, err := conn.ReceiveDatagram(context.Background())
		if err != nil {
			mu.Lock()
			for _, sess := range sessions {
				sess.close()
			}
			mu.Unlock()
			return
		}

		frame, err := ReadUDPFrame(bytes.NewReader(data))
		if err != nil {
			continue
		}

		mu.Lock()
		sess, ok := sessions[frame.SessionID]
		if !ok {
			sess = &serverUDPConn{
				qconn:     conn,
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
		mu.Unlock()

		sess.deliver(frame.Payload)
	}
}

func (s *QUICServer) sendTCP(result tcpAcceptResult) {
	select {
	case s.tcpCh <- result:
	case <-s.done:
	}
}

// AcceptTCP accepts the next TCP proxy stream from any connected client.
// The caller should write a StreamResponse to the returned conn.
func (s *QUICServer) AcceptTCP(ctx context.Context) (net.Conn, string, error) {
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
// Incoming UDP datagrams from all connected clients are dispatched to handler.
func (s *QUICServer) ServeUDP(ctx context.Context, handler UDPHandler) error {
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

// Close stops the server and closes the listener.
func (s *QUICServer) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	select {
	case <-s.done:
		return nil
	default:
		close(s.done)
	}

	if s.ln != nil {
		return s.ln.Close()
	}
	return nil
}

// ============================================================================
// Client-side UDP datagram demuxer
// ============================================================================

type quicUDPMux struct {
	conn     *quic.Conn
	mu       sync.Mutex
	sessions map[uint32]chan *UDPFrame
	done     chan struct{}
	started  bool
}

func newQuicUDPMux(conn *quic.Conn) *quicUDPMux {
	return &quicUDPMux{
		conn:     conn,
		sessions: make(map[uint32]chan *UDPFrame),
		done:     make(chan struct{}),
	}
}

func (m *quicUDPMux) start() {
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return
	}
	m.started = true
	m.mu.Unlock()
	go m.readLoop()
}

func (m *quicUDPMux) readLoop() {
	for {
		data, err := m.conn.ReceiveDatagram(context.Background())
		if err != nil {
			return
		}
		frame, err := ReadUDPFrame(bytes.NewReader(data))
		if err != nil {
			continue
		}
		m.mu.Lock()
		ch, ok := m.sessions[frame.SessionID]
		m.mu.Unlock()
		if ok {
			select {
			case ch <- frame:
			default: // drop if full
			}
		}
	}
}

func (m *quicUDPMux) register(sessionID uint32) chan *UDPFrame {
	ch := make(chan *UDPFrame, 32)
	m.mu.Lock()
	m.sessions[sessionID] = ch
	m.mu.Unlock()
	m.start()
	return ch
}

func (m *quicUDPMux) unregister(sessionID uint32) {
	m.mu.Lock()
	delete(m.sessions, sessionID)
	m.mu.Unlock()
}

func (m *quicUDPMux) close() {
	select {
	case <-m.done:
	default:
		close(m.done)
	}
}

// ============================================================================
// Client-side PacketConn over QUIC datagrams
// ============================================================================

type quicUDPConn struct {
	qconn      *quic.Conn
	sessionID  uint32
	targetAddr string
	mux        *quicUDPMux
	readCh     chan *UDPFrame
	done       chan struct{}
	closeOnce  sync.Once
}

var _ net.PacketConn = (*quicUDPConn)(nil)

func (c *quicUDPConn) ReadFrom(b []byte) (int, net.Addr, error) {
	select {
	case frame := <-c.readCh:
		n := copy(b, frame.Payload)
		addr, _ := net.ResolveUDPAddr("udp", c.targetAddr)
		return n, addr, nil
	case <-c.done:
		return 0, nil, net.ErrClosed
	}
}

func (c *quicUDPConn) WriteTo(b []byte, _ net.Addr) (int, error) {
	var buf bytes.Buffer
	WriteUDPFrame(&buf, c.sessionID, c.targetAddr, b)
	if err := c.qconn.SendDatagram(buf.Bytes()); err != nil {
		return 0, err
	}
	return len(b), nil
}

func (c *quicUDPConn) Close() error {
	c.closeOnce.Do(func() {
		close(c.done)
		c.mux.unregister(c.sessionID)
	})
	return nil
}

func (c *quicUDPConn) LocalAddr() net.Addr             { return c.qconn.LocalAddr() }
func (c *quicUDPConn) SetDeadline(time.Time) error      { return nil }
func (c *quicUDPConn) SetReadDeadline(time.Time) error   { return nil }
func (c *quicUDPConn) SetWriteDeadline(time.Time) error  { return nil }

// ============================================================================
// Server-side PacketConn for UDP sessions
// ============================================================================

type serverUDPConn struct {
	qconn     *quic.Conn
	sessionID uint32
	addr      string
	readCh    chan []byte
	done      chan struct{}
	closeOnce sync.Once
}

var _ net.PacketConn = (*serverUDPConn)(nil)

func (c *serverUDPConn) deliver(payload []byte) {
	select {
	case c.readCh <- payload:
	case <-c.done:
	}
}

func (c *serverUDPConn) ReadFrom(b []byte) (int, net.Addr, error) {
	select {
	case payload := <-c.readCh:
		n := copy(b, payload)
		addr, _ := net.ResolveUDPAddr("udp", c.addr)
		return n, addr, nil
	case <-c.done:
		return 0, nil, net.ErrClosed
	}
}

func (c *serverUDPConn) WriteTo(b []byte, _ net.Addr) (int, error) {
	var buf bytes.Buffer
	// Empty addr for response frames (client already knows the target)
	WriteUDPFrame(&buf, c.sessionID, "", b)
	if err := c.qconn.SendDatagram(buf.Bytes()); err != nil {
		return 0, err
	}
	return len(b), nil
}

func (c *serverUDPConn) close() {
	c.closeOnce.Do(func() { close(c.done) })
}

func (c *serverUDPConn) Close() error {
	c.close()
	return nil
}

func (c *serverUDPConn) LocalAddr() net.Addr             { return c.qconn.LocalAddr() }
func (c *serverUDPConn) SetDeadline(time.Time) error      { return nil }
func (c *serverUDPConn) SetReadDeadline(time.Time) error   { return nil }
func (c *serverUDPConn) SetWriteDeadline(time.Time) error  { return nil }

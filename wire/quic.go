package wire

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"net"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/apernet/quic-go"
	"github.com/kaitu-io/k2/wire/arc"
)

// Compile-time interface checks.
var (
	_ Dialer   = (*QUICClient)(nil)
	_ Listener = (*QUICServer)(nil)
)

// maxServerUDPSessions caps per-connection UDP session count on the server.
// Prevents a malicious client from creating unbounded goroutines via
// incrementing session IDs in datagrams or stream-based UDP frames.
const maxServerUDPSessions = 4096

// maxDatagramHops is the number of port hops to attempt when datagrams become
// unhealthy before falling back to TCP-WS. ISP QoS typically matches on
// port/5-tuple, so hopping to a new port often restores QUIC datagram path.
const maxDatagramHops int32 = 3

// datagramRecoveryInterval is the cooldown after hop exhaustion before
// resetting the counter for another round of probes. Modeled after
// OpenConnect's dtls_attempt_period (60s) — we use 3 minutes because
// each recovery round costs 3 hops (3 resetConnection calls).
const datagramRecoveryInterval = 3 * time.Minute

// ============================================================================
// UDP overflow stream — per-packet fallback for oversized datagrams
// ============================================================================

// udpOverflowStream lazily opens a QUIC stream for sending UDP packets that
// exceed the QUIC datagram size limit. The stream uses SmuxUDPFrame format
// and is shared across all UDP sessions on a connection.
//
// Write serialization: initMu protects lazy init (rare), writeMu serializes
// frame writes (hot path). This avoids holding a single lock across both
// init and I/O, keeping mutex hold time minimal on the data plane.
type udpOverflowStream struct {
	qconn   *quic.Conn
	initMu  sync.Mutex
	writeMu sync.Mutex
	stream  *quic.Stream
	initErr error
	opened  bool
}

// init lazily opens the overflow stream on first call. Subsequent calls are no-ops.
func (o *udpOverflowStream) init() error {
	o.initMu.Lock()
	defer o.initMu.Unlock()

	if o.opened {
		return o.initErr
	}
	o.opened = true

	stream, err := o.qconn.OpenStreamSync(context.Background())
	if err != nil {
		o.initErr = fmt.Errorf("wire: open overflow stream: %w", err)
		slog.Warn("udpOverflowStream: open failed", "err", o.initErr)
		return o.initErr
	}

	// Write H3 frame type + StreamTypeUDPOverflow header
	if err := WriteH3ProxyFrameType(stream); err != nil {
		stream.Close()
		o.initErr = fmt.Errorf("wire: write overflow H3 frame: %w", err)
		return o.initErr
	}
	if err := WriteStreamHeader(stream, StreamTypeUDPOverflow, ""); err != nil {
		stream.Close()
		o.initErr = fmt.Errorf("wire: write overflow header: %w", err)
		return o.initErr
	}

	o.stream = stream
	slog.Info("udpOverflowStream: opened", "streamID", stream.StreamID())
	return nil
}

// send writes a single UDP frame to the overflow stream, lazily opening it on first call.
func (o *udpOverflowStream) send(sessionID uint32, addr string, payload []byte) error {
	if err := o.init(); err != nil {
		return err
	}

	o.writeMu.Lock()
	defer o.writeMu.Unlock()
	return WriteSmuxUDPFrame(o.stream, sessionID, addr, payload)
}

func (o *udpOverflowStream) close() {
	o.initMu.Lock()
	defer o.initMu.Unlock()
	if o.stream != nil {
		o.stream.Close()
	}
}

// connUDPState holds shared UDP state for a single QUIC connection on the server.
// Both the datagram reader and overflow stream reader share this state so that
// packets from the same session ID are delivered to the same serverUDPConn.
type connUDPState struct {
	mu       sync.Mutex
	sessions map[uint32]*serverUDPConn
	overflow *udpOverflowStream
	done     chan struct{}
}

// DefaultQUICConfig returns the QUIC configuration.
// Flow control windows and max streams are platform-specific (see quic_limits_*.go).
// Desktop/server: large windows for throughput. Mobile: small windows for memory.
func DefaultQUICConfig() *quic.Config {
	return &quic.Config{
		InitialStreamReceiveWindow:     quicStreamWindow,
		MaxStreamReceiveWindow:         quicStreamWindow,
		InitialConnectionReceiveWindow: quicConnectionWindow,
		MaxConnectionReceiveWindow:     quicConnectionWindow,
		EnableDatagrams:                true,
		MaxIdleTimeout:                 30 * time.Second,
		KeepAlivePeriod:                10 * time.Second,
		MaxIncomingStreams:             quicMaxStreams,
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
	cfg          Config
	store        *TicketStore
	serverAddr   string
	hopStart     int
	hopEnd       int
	tlsConfig    *tls.Config // override for testing (bypasses BuildClientTLSConfig)
	directDialer DirectDialer

	mu        sync.Mutex
	conn      *quic.Conn
	transport *quic.Transport
	alloc     *SessionIDAllocator
	udpMux    *quicUDPMux
	closed    bool
	arcSender *arc.K2ARCSender // stored for stats access

	datagramHopAttempts  atomic.Int32 // hop attempts before TCP-WS fallback
	datagramHopExhausted atomic.Int64 // UnixNano: when hops were exhausted (0 = not exhausted)
	hopInProgress        atomic.Bool  // CAS guard: prevents concurrent hops
}

// NewQUICClient creates a new QUIC client transport.
func NewQUICClient(cfg Config, store *TicketStore) *QUICClient {
	dialHost := cfg.Host
	if cfg.DialAddr != "" {
		dialHost = cfg.DialAddr
	}
	return &QUICClient{
		cfg:        cfg,
		store:      store,
		serverAddr: net.JoinHostPort(dialHost, strconv.Itoa(cfg.Port)),
		hopStart:   cfg.HopStart,
		hopEnd:     cfg.HopEnd,
	}
}

// SetTLSConfig overrides the TLS config for testing (bypasses BuildClientTLSConfig).
func (c *QUICClient) SetTLSConfig(cfg *tls.Config) {
	c.tlsConfig = cfg
}

// SetDirectDialer sets a physical-interface-bound dialer for bypassing TUN.
func (c *QUICClient) SetDirectDialer(d DirectDialer) {
	c.directDialer = d
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
		dialHost := c.cfg.Host
		if c.cfg.DialAddr != "" {
			dialHost = c.cfg.DialAddr
		}
		addr = net.JoinHostPort(dialHost, strconv.Itoa(c.hopPort()))
	}

	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("wire: resolve %s: %w", addr, err)
	}

	var udpConn net.PacketConn
	if c.directDialer != nil {
		udpConn, err = c.directDialer.ListenPacket("udp", ":0")
	} else {
		udpConn, err = net.ListenUDP("udp", &net.UDPAddr{})
	}
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

	qcfg := DefaultQUICConfig()

	conn, err := transport.Dial(ctx, udpAddr, tlsCfg, qcfg)
	if err != nil {
		transport.Close()
		return nil, fmt.Errorf("wire: QUIC dial: %w", err)
	}

	// Set K2ARC congestion control (replaces default Cubic)
	sender := arc.NewArcSenderGFWMode(arc.DefaultMaxRate)
	conn.SetCongestionControl(sender)
	c.arcSender = sender
	slog.Info("wire: QUIC client K2ARC enabled", "maxRateMbps", arc.DefaultMaxRate/1_000_000)

	c.conn = conn
	c.transport = transport
	c.alloc = NewSessionIDAllocator()
	c.udpMux = newQuicUDPMux(conn, &c.datagramHopAttempts, &c.datagramHopExhausted)

	// Start accept loop for server-initiated overflow streams
	go c.acceptOverflowStreams(conn, c.udpMux)

	return conn, nil
}

// DialTCP opens a TCP proxy stream through the QUIC connection.
// Writes H3 frame type + stream header, reads response, returns the stream.
func (c *QUICClient) DialTCP(ctx context.Context, addr string) (net.Conn, error) {
	slog.Debug("QUICClient.DialTCP: connecting", "addr", addr)
	conn, err := c.connect(ctx)
	if err != nil {
		slog.Debug("QUICClient.DialTCP: connect failed", "addr", addr, "err", err)
		return nil, err
	}

	slog.Debug("QUICClient.DialTCP: opening stream", "addr", addr)
	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		slog.Debug("QUICClient.DialTCP: open stream failed", "addr", addr, "err", err)
		return nil, fmt.Errorf("wire: open stream: %w", err)
	}
	slog.Debug("QUICClient.DialTCP: stream opened", "addr", addr, "streamID", stream.StreamID())

	// Write H3 custom frame type marker + TCP stream header
	if err := WriteH3ProxyFrameType(stream); err != nil {
		stream.Close()
		return nil, fmt.Errorf("wire: write H3 frame type: %w", err)
	}
	if err := WriteStreamHeader(stream, StreamTypeTCP, addr); err != nil {
		stream.Close()
		return nil, fmt.Errorf("wire: write stream header: %w", err)
	}
	slog.Debug("QUICClient.DialTCP: header written, waiting for response", "addr", addr)

	// Read response
	br := bufio.NewReader(stream)
	ok, errCode, errMsg, err := ReadStreamResponse(br)
	if err != nil {
		slog.Debug("QUICClient.DialTCP: read response failed", "addr", addr, "err", err)
		stream.Close()
		return nil, fmt.Errorf("wire: read response: %w", err)
	}
	if !ok {
		slog.Debug("QUICClient.DialTCP: stream rejected", "addr", addr, "errCode", errCode, "errMsg", errMsg)
		stream.Close()
		if errCode > 0 {
			return nil, &StreamError{Code: errCode, Message: errMsg}
		}
		return nil, fmt.Errorf("%w: %s", ErrStreamRejected, errMsg)
	}

	slog.Debug("QUICClient.DialTCP: success", "addr", addr)
	return &quicStreamConn{
		Stream: stream,
		local:  conn.LocalAddr(),
		remote: conn.RemoteAddr(),
		br:     br,
	}, nil
}

// DialUDP opens a UDP proxy session through QUIC datagrams or stream fallback.
// Returns a PacketConn backed by QUIC datagrams with a unique session ID.
//
// When the datagram path is unhealthy (3 consecutive ReadFrom timeouts) and
// port hopping is configured, DialUDP hops to a new port (resetConnection +
// reconnect) up to maxDatagramHops times before returning ErrDatagramUnhealthy.
// This gives QUIC a chance to recover from ISP QoS that blocks specific ports.
func (c *QUICClient) DialUDP(ctx context.Context, addr string) (net.PacketConn, error) {
	for {
		conn, err := c.connect(ctx)
		if err != nil {
			return nil, err
		}

		// Single lock hold: verify conn is still current + read all resources.
		// Prevents TOCTOU race with resetConnection() which can close conn and
		// nil alloc/udpMux between connect() return and resource access.
		c.mu.Lock()
		stale := c.conn != conn
		alloc := c.alloc
		mux := c.udpMux
		c.mu.Unlock()

		if stale || alloc == nil || mux == nil {
			return nil, ErrNotConnected
		}

		// Safe: conn verified current under lock; ConnectionState() reads
		// immutable handshake data set during QUIC handshake.
		dgSupported := conn.ConnectionState().SupportsDatagrams
		slog.Info("QUICClient.DialUDP", "addr", addr, "datagramsSupported", dgSupported)
		if dgSupported {
			if !mux.datagramHealthy() {
				// Hop port before falling back to TCP-WS.
				// tryHop() uses CAS to ensure only one goroutine hops at a time.
				if c.hopEnabled() && c.datagramHopAttempts.Load() < maxDatagramHops {
					c.tryHop() // winner resets connection; loser gets stale conn
					continue   // both retry: connect() gives fresh conn/mux
				}

				// Hops exhausted. Record exhaustion time (once) and check
				// if cooldown has elapsed for a recovery probe.
				if c.hopEnabled() {
					exAt := c.datagramHopExhausted.Load()
					if exAt == 0 {
						c.datagramHopExhausted.Store(time.Now().UnixNano())
					} else if time.Since(time.Unix(0, exAt)) >= datagramRecoveryInterval {
						// Cooldown elapsed — reset for a fresh round of hops.
						c.datagramHopAttempts.Store(0)
						c.datagramHopExhausted.Store(0)
						slog.Info("QUICClient.DialUDP: datagram recovery probe, resetting hops")
						c.tryHop()
						continue
					}
				}

				// Within cooldown (or hop not configured) → TCP-WS fallback
				slog.Debug("QUICClient.DialUDP: datagram path unhealthy, using TCP-WS",
					"addr", addr,
					"hopAttempts", c.datagramHopAttempts.Load())
				return nil, ErrDatagramUnhealthy
			}

			sessionID := alloc.Next()
			readCh := mux.register(sessionID)

			return &quicUDPConn{
				qconn:      conn,
				sessionID:  sessionID,
				targetAddr: addr,
				mux:        mux,
				readCh:     readCh,
				done:       make(chan struct{}),
			}, nil
		}

		// Fallback: stream-based UDP (datagrams not supported by server)
		return c.dialUDPStream(ctx, conn, addr)
	}
}

// dialUDPStream opens a QUIC stream for UDP and returns a stream-based PacketConn.
func (c *QUICClient) dialUDPStream(ctx context.Context, conn *quic.Conn, addr string) (net.PacketConn, error) {
	// Ensure shared UDP stream and mux exist
	c.mu.Lock()
	if c.udpMux == nil || c.alloc == nil {
		c.mu.Unlock()
		return nil, ErrNotConnected
	}
	if c.udpMux.streamMux == nil {
		stream, err := conn.OpenStreamSync(ctx)
		if err != nil {
			c.mu.Unlock()
			return nil, fmt.Errorf("wire: open UDP stream: %w", err)
		}
		// Write H3 frame type + UDP stream header
		if err := WriteH3ProxyFrameType(stream); err != nil {
			stream.Close()
			c.mu.Unlock()
			return nil, fmt.Errorf("wire: write H3 frame type: %w", err)
		}
		if err := WriteStreamHeader(stream, StreamTypeUDP, ""); err != nil {
			stream.Close()
			c.mu.Unlock()
			return nil, fmt.Errorf("wire: write UDP stream header: %w", err)
		}
		c.udpMux.streamMux = newQuicStreamUDPMux(stream, conn)
	}
	mux := c.udpMux.streamMux
	alloc := c.alloc
	c.mu.Unlock()

	sessionID := alloc.Next()
	readCh := mux.register(sessionID)

	return &quicStreamUDPConn{
		mux:        mux,
		sessionID:  sessionID,
		targetAddr: addr,
		readCh:     readCh,
		done:       make(chan struct{}),
	}, nil
}

// WireStats returns a snapshot of QUIC transport metrics including K2ARC state.
func (c *QUICClient) WireStats() WireStats {
	c.mu.Lock()
	sender := c.arcSender
	mux := c.udpMux
	c.mu.Unlock()

	s := WireStats{Transport: "quic"}
	if sender != nil {
		snap := sender.Snapshot()
		s.LossRate = snap.LossRate
		s.Throughput = snap.Throughput
		s.SendingRate = snap.SendingRate
		s.RTT = snap.RTT
		s.MinRTT = snap.MinRTT
		s.Cwnd = snap.Cwnd
		s.Mode = int(snap.Mode)
		s.MIID = snap.MIID
	}
	if mux != nil {
		s.DatagramLossRate = mux.evaluateLossRate()
		s.DatagramTimeouts = mux.consecutiveTimeouts.Load()
	}
	s.DatagramHopAttempts = c.datagramHopAttempts.Load()
	return s
}

var _ StatsProvider = (*QUICClient)(nil)

// acceptOverflowStreams runs a loop accepting server-initiated QUIC streams.
// It handles StreamTypeUDPOverflow streams by reading SmuxUDPFrame and delivering
// to the udpMux session map (same sessions as datagram path).
func (c *QUICClient) acceptOverflowStreams(conn *quic.Conn, mux *quicUDPMux) {
	for {
		stream, err := conn.AcceptStream(context.Background())
		if err != nil {
			return // connection closed
		}
		go c.handleServerOverflowStream(stream, mux)
	}
}

// handleServerOverflowStream reads one overflow stream from the server.
func (c *QUICClient) handleServerOverflowStream(stream *quic.Stream, mux *quicUDPMux) {
	defer stream.Close()

	br := bufio.NewReader(stream)

	// Read H3 frame type marker
	frameType, _, err := ReadVarint(br)
	if err != nil {
		slog.Debug("QUICClient: overflow stream read frame type failed", "err", err)
		return
	}
	if frameType != H3FrameTypeK2Proxy {
		slog.Debug("QUICClient: overflow stream unknown frame type", "frameType", frameType)
		return
	}

	// Read stream header
	streamType, _, err := ReadStreamHeader(br)
	if err != nil {
		slog.Debug("QUICClient: overflow stream read header failed", "err", err)
		return
	}
	if streamType != StreamTypeUDPOverflow {
		slog.Debug("QUICClient: overflow stream unexpected type", "streamType", streamType)
		return
	}

	slog.Info("QUICClient: receiving overflow stream from server")

	// Read SmuxUDPFrame loop and deliver to mux sessions
	for {
		frame, err := ReadSmuxUDPFrame(br)
		if err != nil {
			return
		}

		mux.mu.Lock()
		ch, ok := mux.sessions[frame.SessionID]
		mux.mu.Unlock()
		if ok {
			select {
			case ch <- frame:
			default: // drop if full
			}
		}
	}
}

// tryHop attempts a single datagram port hop. Returns true if this goroutine
// won the race and executed the hop. Concurrent callers get false immediately.
func (c *QUICClient) tryHop() bool {
	if !c.hopInProgress.CompareAndSwap(false, true) {
		return false
	}
	defer c.hopInProgress.Store(false)

	attempt := c.datagramHopAttempts.Add(1)
	slog.Warn("QUICClient: datagram unhealthy, hopping port",
		"hopAttempt", attempt, "maxHops", maxDatagramHops)
	c.resetConnection()
	return true
}

// resetConnection closes cached connection resources without marking client as closed.
// After reset, the next DialTCP/DialUDP will establish a fresh connection.
func (c *QUICClient) resetConnection() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.udpMux != nil {
		c.udpMux.close()
		c.udpMux = nil
	}
	if c.conn != nil {
		if err := c.conn.CloseWithError(0, "network changed"); err != nil {
			slog.Debug("QUICClient: resetConnection close error", "err", err)
		}
		c.conn = nil
	}
	if c.transport != nil {
		c.transport.Close()
		c.transport = nil
	}
	c.alloc = nil
	c.arcSender = nil
	// NOTE: do NOT set c.closed = true — client must remain usable
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
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("QUICServer: panic in acceptLoop", "panic", r)
			}
		}()
		s.acceptLoop()
	}()
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
		slog.Debug("QUICServer: accepted connection", "remote", conn.RemoteAddr())
		// Set K2ARC congestion control for server-side sending (critical for download speed)
		conn.SetCongestionControl(arc.NewArcSenderGFWMode(arc.DefaultMaxRate))
		go func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("QUICServer: panic in handleConn", "panic", r, "remote", conn.RemoteAddr())
				}
			}()
			s.handleConn(conn)
		}()
	}
}

func (s *QUICServer) handleConn(conn *quic.Conn) {
	// Shared UDP state for datagram + overflow stream paths
	state := &connUDPState{
		sessions: make(map[uint32]*serverUDPConn),
		overflow: &udpOverflowStream{qconn: conn},
		done:     make(chan struct{}),
	}

	// Accept streams (TCP proxy + UDP overflow)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("QUICServer: panic in AcceptStream loop", "panic", r, "remote", conn.RemoteAddr())
			}
		}()
		for {
			stream, err := conn.AcceptStream(context.Background())
			if err != nil {
				return
			}
			go func() {
				defer func() {
					if r := recover(); r != nil {
						slog.Error("QUICServer: panic in handleStream", "panic", r, "remote", conn.RemoteAddr())
					}
				}()
				s.handleStream(conn, stream, state)
			}()
		}
	}()

	// Read datagrams (UDP proxy) — blocks until connection closes
	s.handleDatagrams(conn, state)

	// Cleanup: close all UDP sessions and overflow stream
	close(state.done)
	state.overflow.close()
	state.mu.Lock()
	for _, sess := range state.sessions {
		sess.close()
	}
	state.mu.Unlock()
}

func (s *QUICServer) handleStream(qconn *quic.Conn, stream *quic.Stream, state *connUDPState) {
	br := bufio.NewReader(stream)

	// Read H3 frame type marker
	frameType, _, err := ReadVarint(br)
	if err != nil {
		slog.Debug("QUICServer.handleStream: read frame type failed", "remote", qconn.RemoteAddr(), "err", err)
		stream.Close()
		return
	}
	if frameType != H3FrameTypeK2Proxy {
		slog.Debug("QUICServer.handleStream: unknown frame type", "frameType", frameType, "remote", qconn.RemoteAddr())
		stream.Close()
		return
	}

	// Read stream header
	streamType, addr, err := ReadStreamHeader(br)
	if err != nil {
		slog.Debug("QUICServer.handleStream: read header failed", "remote", qconn.RemoteAddr(), "err", err)
		stream.Close()
		return
	}

	slog.Debug("QUICServer.handleStream: parsed", "streamType", streamType, "addr", addr, "remote", qconn.RemoteAddr())

	switch streamType {
	case StreamTypeTCP:
		sc := &quicStreamConn{
			Stream: stream,
			local:  qconn.LocalAddr(),
			remote: qconn.RemoteAddr(),
			br:     br,
		}
		s.sendTCP(tcpAcceptResult{conn: sc, addr: addr})
		slog.Debug("QUICServer.handleStream: TCP queued", "addr", addr)
	case StreamTypeUDP:
		s.handleUDPStream(stream, br)
	case StreamTypeUDPOverflow:
		s.handleUDPOverflowStream(br, state)
	default:
		slog.Debug("QUICServer.handleStream: unknown stream type", "streamType", streamType)
		stream.Close()
	}
}

func (s *QUICServer) handleUDPStream(stream *quic.Stream, br *bufio.Reader) {
	writer := &quicStreamWriter{stream: stream}
	sessions := make(map[uint32]*quicStreamServerUDPConn)

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
			sess = &quicStreamServerUDPConn{
				writer:    writer,
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
							slog.Error("QUICServer: panic in HandleUDP (stream)", "panic", r, "addr", addr)
						}
					}()
					handler.HandleUDP(sess, addr)
				}()
			}
		}

		sess.deliver(frame.Payload)
	}
}

func (s *QUICServer) handleDatagrams(conn *quic.Conn, state *connUDPState) {
	slog.Info("QUICServer: handleDatagrams started", "remote", conn.RemoteAddr())

	for {
		data, err := conn.ReceiveDatagram(context.Background())
		if err != nil {
			slog.Info("QUICServer: handleDatagrams exiting", "remote", conn.RemoteAddr(), "err", err)
			return
		}

		frame, err := ReadUDPFrame(bytes.NewReader(data))
		if err != nil {
			slog.Warn("QUICServer: datagram parse failed", "len", len(data), "err", err)
			continue
		}
		slog.Debug("QUICServer: datagram received", "sessionID", frame.SessionID, "addr", frame.Addr, "payloadLen", len(frame.Payload))

		s.deliverToSession(state, conn, frame)
	}
}

// deliverToSession looks up or creates a serverUDPConn in state.sessions, then delivers the payload.
// Used by both handleDatagrams and handleUDPOverflowStream for session sharing.
func (s *QUICServer) deliverToSession(state *connUDPState, conn *quic.Conn, frame *UDPFrame) {
	state.mu.Lock()
	sess, ok := state.sessions[frame.SessionID]
	if !ok {
		if len(state.sessions) >= maxServerUDPSessions {
			state.mu.Unlock()
			slog.Warn("QUICServer: max UDP sessions reached, dropping")
			return
		}
		sess = &serverUDPConn{
			qconn:     conn,
			sessionID: frame.SessionID,
			addr:      frame.Addr,
			readCh:    make(chan []byte, 32),
			done:      make(chan struct{}),
			overflow:  state.overflow,
		}
		state.sessions[frame.SessionID] = sess

		s.mu.Lock()
		handler := s.udpH
		s.mu.Unlock()
		slog.Info("QUICServer: new UDP session", "sessionID", frame.SessionID, "addr", frame.Addr, "handlerSet", handler != nil)
		if handler != nil {
			addr := frame.Addr
			go func() {
				defer func() {
					if r := recover(); r != nil {
						slog.Error("QUICServer: panic in HandleUDP", "panic", r, "addr", addr)
					}
				}()
				handler.HandleUDP(sess, addr)
			}()
		}
	}
	state.mu.Unlock()

	sess.deliver(frame.Payload)
}

// handleUDPOverflowStream reads SmuxUDPFrame from a client-initiated overflow stream
// and delivers them to the shared session map (same sessions as datagram path).
func (s *QUICServer) handleUDPOverflowStream(br *bufio.Reader, state *connUDPState) {
	slog.Info("QUICServer: receiving overflow stream from client")
	// We need the qconn to create new sessions — get it from the overflow struct.
	conn := state.overflow.qconn
	for {
		frame, err := ReadSmuxUDPFrame(br)
		if err != nil {
			return
		}
		s.deliverToSession(state, conn, frame)
	}
}

func (s *QUICServer) sendTCP(result tcpAcceptResult) {
	select {
	case s.tcpCh <- result:
	case <-s.done:
		if result.conn != nil {
			result.conn.Close()
		}
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
	conn      *quic.Conn
	mu        sync.Mutex
	sessions  map[uint32]chan *UDPFrame
	done      chan struct{}
	started   bool
	streamMux *quicStreamUDPMux // stream-based UDP fallback
	overflow  *udpOverflowStream // per-packet overflow for oversized datagrams

	// Datagram health tracking: consecutive ReadFrom timeouts indicate
	// a broken QUIC datagram path (e.g. ISP QoS dropping UDP).
	consecutiveTimeouts atomic.Int32
	lastRecv            atomic.Int64 // UnixNano of last received datagram

	clientHopAttempts  *atomic.Int32 // shared with QUICClient; nil on server/tests
	clientHopExhausted *atomic.Int64 // shared with QUICClient; nil on server/tests

	// Sliding window loss rate tracking.
	datagramSent atomic.Int64
	datagramRecv atomic.Int64

	// Cached loss rate (lazy evaluation per window).
	lossMu       sync.Mutex
	lossRate     float64
	lossTime     int64 // UnixNano of last evaluation
	lossPrevSent int64
	lossPrevRecv int64
}

const (
	datagramTimeoutThreshold int32 = 3

	datagramLossWindow    = 5 * time.Second
	datagramLossUnhealthy = 0.40 // 40% loss → unhealthy
	datagramLossRecovered = 0.15 // <15% → recovered (hop reset)
	datagramMinSamples    = 10   // min packets in window to evaluate
)

func (m *quicUDPMux) recordRecv() {
	m.consecutiveTimeouts.Store(0)
	m.lastRecv.Store(time.Now().UnixNano())
	m.datagramRecv.Add(1)
	// Hop counter reset moved to evaluateLossRate() — requires sustained
	// low loss rate, not just a single lucky packet.
}

func (m *quicUDPMux) recordSend() {
	m.datagramSent.Add(1)
}

func (m *quicUDPMux) recordTimeout() {
	m.consecutiveTimeouts.Add(1)
}

func (m *quicUDPMux) datagramHealthy() bool {
	if m.consecutiveTimeouts.Load() >= datagramTimeoutThreshold {
		return false
	}
	if m.evaluateLossRate() >= datagramLossUnhealthy {
		return false
	}
	return true
}

// evaluateLossRate computes datagram loss rate from sent/received deltas.
// Lazy evaluation: caches result for datagramLossWindow, recomputes on expiry.
func (m *quicUDPMux) evaluateLossRate() float64 {
	m.lossMu.Lock()
	defer m.lossMu.Unlock()

	now := time.Now().UnixNano()
	if m.lossTime > 0 && now-m.lossTime < datagramLossWindow.Nanoseconds() {
		return m.lossRate // cached within window
	}

	sent := m.datagramSent.Load()
	recv := m.datagramRecv.Load()
	deltaSent := sent - m.lossPrevSent
	deltaRecv := recv - m.lossPrevRecv

	m.lossPrevSent = sent
	m.lossPrevRecv = recv
	m.lossTime = now

	if deltaSent < datagramMinSamples {
		return m.lossRate // not enough data
	}

	if deltaRecv >= deltaSent {
		m.lossRate = 0
	} else {
		m.lossRate = 1.0 - float64(deltaRecv)/float64(deltaSent)
	}

	// Reset hop counter and exhaustion timestamp when loss rate recovers.
	if m.lossRate < datagramLossRecovered && m.clientHopAttempts != nil {
		m.clientHopAttempts.Store(0)
		if m.clientHopExhausted != nil {
			m.clientHopExhausted.Store(0)
		}
	}

	return m.lossRate
}

// expireLossWindow forces next evaluateLossRate to recompute. Test helper.
func (m *quicUDPMux) expireLossWindow() {
	m.lossMu.Lock()
	m.lossTime = time.Now().Add(-2 * datagramLossWindow).UnixNano()
	m.lossMu.Unlock()
}

func newQuicUDPMux(conn *quic.Conn, hopAttempts *atomic.Int32, hopExhausted *atomic.Int64) *quicUDPMux {
	return &quicUDPMux{
		conn:               conn,
		sessions:           make(map[uint32]chan *UDPFrame),
		done:               make(chan struct{}),
		clientHopAttempts:  hopAttempts,
		clientHopExhausted: hopExhausted,
		overflow:           &udpOverflowStream{qconn: conn},
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
	defer func() {
		select {
		case <-m.done:
		default:
			close(m.done)
		}
	}()
	slog.Info("quicUDPMux: readLoop started")
	for {
		data, err := m.conn.ReceiveDatagram(context.Background())
		if err != nil {
			slog.Warn("quicUDPMux: readLoop exiting", "err", err)
			return
		}
		m.recordRecv()
		slog.Debug("quicUDPMux: received datagram", "len", len(data))
		frame, err := ReadUDPFrame(bytes.NewReader(data))
		if err != nil {
			continue
		}
		m.mu.Lock()
		ch, ok := m.sessions[frame.SessionID]
		m.mu.Unlock()
		if !ok {
			slog.Debug("quicUDPMux: no session for datagram", "sessionID", frame.SessionID, "addr", frame.Addr, "payloadLen", len(frame.Payload))
			continue
		}
		select {
		case ch <- frame:
		default:
			slog.Warn("quicUDPMux: channel full, dropping datagram", "sessionID", frame.SessionID, "addr", frame.Addr, "payloadLen", len(frame.Payload))
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
	if m.overflow != nil {
		m.overflow.close()
	}
	if m.streamMux != nil {
		m.streamMux.close()
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
	rd         readDeadline
}

var _ net.PacketConn = (*quicUDPConn)(nil)

func (c *quicUDPConn) ReadFrom(b []byte) (int, net.Addr, error) {
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
		slog.Debug("quicUDPConn: ReadFrom ok", "sessionID", c.sessionID, "target", c.targetAddr, "len", n)
		addr, _ := net.ResolveUDPAddr("udp", c.targetAddr)
		return n, addr, nil
	case <-c.done:
		return 0, nil, net.ErrClosed
	case <-c.mux.done:
		return 0, nil, net.ErrClosed
	case <-timerCh:
		c.mux.recordTimeout()
		slog.Warn("quicUDPConn: ReadFrom timeout", "sessionID", c.sessionID, "target", c.targetAddr)
		return 0, nil, os.ErrDeadlineExceeded
	}
}

func (c *quicUDPConn) WriteTo(b []byte, _ net.Addr) (int, error) {
	frame, put := MarshalUDPFrame(c.sessionID, c.targetAddr, b)
	err := c.qconn.SendDatagram(frame)
	put() // SendDatagram copies internally; safe to return buffer to pool
	if err == nil {
		c.mux.recordSend()
		return len(b), nil
	}

	// Per-packet overflow: if datagram too large, send via overflow stream
	var tooLarge *quic.DatagramTooLargeError
	if errors.As(err, &tooLarge) && c.mux.overflow != nil {
		if err2 := c.mux.overflow.send(c.sessionID, c.targetAddr, b); err2 != nil {
			return 0, err2
		}
		c.mux.recordSend()
		return len(b), nil
	}

	// Other errors: track for health monitoring
	slog.Warn("quicUDPConn: SendDatagram failed", "sessionID", c.sessionID,
		"target", c.targetAddr, "payloadLen", len(b), "err", err)
	c.mux.recordSend()
	c.mux.recordTimeout()
	return 0, err
}

func (c *quicUDPConn) Close() error {
	c.closeOnce.Do(func() {
		close(c.done)
		c.mux.unregister(c.sessionID)
	})
	return nil
}

func (c *quicUDPConn) LocalAddr() net.Addr                { return c.qconn.LocalAddr() }
func (c *quicUDPConn) SetDeadline(t time.Time) error      { c.rd.set(t); return nil }
func (c *quicUDPConn) SetReadDeadline(t time.Time) error   { c.rd.set(t); return nil }
func (c *quicUDPConn) SetWriteDeadline(time.Time) error    { return nil }

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
	rd        readDeadline
	overflow  *udpOverflowStream // per-packet overflow for oversized datagrams
}

var _ net.PacketConn = (*serverUDPConn)(nil)

func (c *serverUDPConn) deliver(payload []byte) {
	select {
	case c.readCh <- payload:
	case <-c.done:
	}
}

func (c *serverUDPConn) ReadFrom(b []byte) (int, net.Addr, error) {
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

func (c *serverUDPConn) WriteTo(b []byte, _ net.Addr) (int, error) {
	// Empty addr for response frames (client already knows the target)
	frame, put := MarshalUDPFrame(c.sessionID, "", b)
	err := c.qconn.SendDatagram(frame)
	put() // SendDatagram copies internally; safe to return buffer to pool
	if err == nil {
		return len(b), nil
	}

	// Per-packet overflow: if datagram too large, send via overflow stream
	var tooLarge *quic.DatagramTooLargeError
	if errors.As(err, &tooLarge) && c.overflow != nil {
		if err2 := c.overflow.send(c.sessionID, "", b); err2 != nil {
			return 0, err2
		}
		return len(b), nil
	}

	slog.Warn("serverUDPConn: SendDatagram failed", "sessionID", c.sessionID, "addr", c.addr, "err", err)
	return 0, err
}

func (c *serverUDPConn) close() {
	c.closeOnce.Do(func() { close(c.done) })
}

func (c *serverUDPConn) Close() error {
	c.close()
	return nil
}

func (c *serverUDPConn) LocalAddr() net.Addr                { return c.qconn.LocalAddr() }
func (c *serverUDPConn) SetDeadline(t time.Time) error      { c.rd.set(t); return nil }
func (c *serverUDPConn) SetReadDeadline(t time.Time) error   { c.rd.set(t); return nil }
func (c *serverUDPConn) SetWriteDeadline(time.Time) error    { return nil }

// ============================================================================
// Server-side QUIC stream writer (shared, mutex-protected)
// ============================================================================

type quicStreamWriter struct {
	stream  *quic.Stream
	writeMu sync.Mutex
}

func (w *quicStreamWriter) writeFrame(sessionID uint32, addr string, payload []byte) error {
	w.writeMu.Lock()
	defer w.writeMu.Unlock()
	return WriteSmuxUDPFrame(w.stream, sessionID, addr, payload)
}

// ============================================================================
// Server-side PacketConn for QUIC stream UDP sessions
// ============================================================================

type quicStreamServerUDPConn struct {
	writer    *quicStreamWriter
	sessionID uint32
	addr      string
	readCh    chan []byte
	done      chan struct{}
	closeOnce sync.Once
	rd        readDeadline
}

var _ net.PacketConn = (*quicStreamServerUDPConn)(nil)

func (c *quicStreamServerUDPConn) deliver(payload []byte) {
	select {
	case c.readCh <- payload:
	case <-c.done:
	}
}

func (c *quicStreamServerUDPConn) ReadFrom(b []byte) (int, net.Addr, error) {
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

func (c *quicStreamServerUDPConn) WriteTo(b []byte, _ net.Addr) (int, error) {
	// Empty addr for responses
	if err := c.writer.writeFrame(c.sessionID, "", b); err != nil {
		return 0, err
	}
	return len(b), nil
}

func (c *quicStreamServerUDPConn) close() {
	c.closeOnce.Do(func() { close(c.done) })
}

func (c *quicStreamServerUDPConn) Close() error {
	c.close()
	return nil
}

func (c *quicStreamServerUDPConn) LocalAddr() net.Addr                { return &net.UDPAddr{} }
func (c *quicStreamServerUDPConn) SetDeadline(t time.Time) error      { c.rd.set(t); return nil }
func (c *quicStreamServerUDPConn) SetReadDeadline(t time.Time) error   { c.rd.set(t); return nil }
func (c *quicStreamServerUDPConn) SetWriteDeadline(time.Time) error    { return nil }

// ============================================================================
// Client-side stream-based UDP mux (fallback when datagrams not supported)
// ============================================================================

type quicStreamUDPMux struct {
	stream   *quic.Stream
	qconn    *quic.Conn
	br       *bufio.Reader
	writeMu  sync.Mutex
	mu       sync.Mutex
	sessions map[uint32]chan *UDPFrame
	done     chan struct{}
	started  bool
}

func newQuicStreamUDPMux(stream *quic.Stream, qconn *quic.Conn) *quicStreamUDPMux {
	return &quicStreamUDPMux{
		stream:   stream,
		qconn:    qconn,
		br:       bufio.NewReader(stream),
		sessions: make(map[uint32]chan *UDPFrame),
		done:     make(chan struct{}),
	}
}

func (m *quicStreamUDPMux) start() {
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return
	}
	m.started = true
	m.mu.Unlock()
	go m.readLoop()
}

func (m *quicStreamUDPMux) readLoop() {
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
			slog.Warn("quicStreamUDPMux: readLoop exiting", "err", err)
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

func (m *quicStreamUDPMux) writeFrame(sessionID uint32, addr string, payload []byte) error {
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	return WriteSmuxUDPFrame(m.stream, sessionID, addr, payload)
}

func (m *quicStreamUDPMux) register(sessionID uint32) chan *UDPFrame {
	ch := make(chan *UDPFrame, 32)
	m.mu.Lock()
	m.sessions[sessionID] = ch
	m.mu.Unlock()
	m.start()
	return ch
}

func (m *quicStreamUDPMux) unregister(sessionID uint32) {
	m.mu.Lock()
	delete(m.sessions, sessionID)
	m.mu.Unlock()
}

func (m *quicStreamUDPMux) close() {
	select {
	case <-m.done:
	default:
		close(m.done)
	}
	m.stream.Close()
}

// ============================================================================
// Client-side PacketConn over QUIC stream (fallback)
// ============================================================================

type quicStreamUDPConn struct {
	mux        *quicStreamUDPMux
	sessionID  uint32
	targetAddr string
	readCh     chan *UDPFrame
	done       chan struct{}
	closeOnce  sync.Once
	rd         readDeadline
}

var _ net.PacketConn = (*quicStreamUDPConn)(nil)

func (c *quicStreamUDPConn) ReadFrom(b []byte) (int, net.Addr, error) {
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
		return 0, nil, os.ErrDeadlineExceeded
	}
}

func (c *quicStreamUDPConn) WriteTo(b []byte, _ net.Addr) (int, error) {
	if err := c.mux.writeFrame(c.sessionID, c.targetAddr, b); err != nil {
		return 0, err
	}
	return len(b), nil
}

func (c *quicStreamUDPConn) Close() error {
	c.closeOnce.Do(func() {
		close(c.done)
		c.mux.unregister(c.sessionID)
	})
	return nil
}

func (c *quicStreamUDPConn) LocalAddr() net.Addr                { return c.mux.qconn.LocalAddr() }
func (c *quicStreamUDPConn) SetDeadline(t time.Time) error      { c.rd.set(t); return nil }
func (c *quicStreamUDPConn) SetReadDeadline(t time.Time) error   { c.rd.set(t); return nil }
func (c *quicStreamUDPConn) SetWriteDeadline(time.Time) error    { return nil }

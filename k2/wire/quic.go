package wire

import (
	"bufio"
	"bytes"
	"context"
	tls "github.com/metacubex/utls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sasha-s/go-deadlock"

	"github.com/kaitu-io/kuic"
	"github.com/kaitu-io/k2/safego"
	"github.com/kaitu-io/k2/wire/k2cc"
)

// Compile-time interface checks.
var (
	_ Dialer     = (*QUICClient)(nil)
	_ Listener   = (*QUICServer)(nil)
	_ EchoProber = (*QUICClient)(nil)
)

// maxServerUDPSessions caps per-connection UDP session count on the server.
// Prevents a malicious client from creating unbounded goroutines via
// incrementing session IDs in datagrams or stream-based UDP frames.
const maxServerUDPSessions = 4096

// tcpHealthWindow is how recently TCP must have succeeded to consider the
// connection usable. Matches QUIC MaxIdleTimeout (30s). Used by health
// monitor to skip reconnect when TCP streams are working.
const tcpHealthWindow = 30 * time.Second

// ErrDatagramSendTimeout is returned when concurrent SendDatagram calls exceed maxDatagramInflight.
var ErrDatagramSendTimeout = errors.New("wire: SendDatagram queue saturated")

// maxDatagramInflight caps concurrent SendDatagram calls per connection.
// Must be well below quic-go's 32-frame datagram queue to prevent mass
// goroutine blocking: datagramQueue.Add() blocks indefinitely when full,
// freezing HandleUDP goroutines (can't call target.Read while blocked).
// With N sessions sharing one queue, only maxDatagramInflight goroutines
// may enter the blocking Add() path; the rest drop immediately.
// Value 8 ensures ≤8 goroutines block (~1.4s each at typical drain rates)
// while remaining sessions keep reading from targets.
const maxDatagramInflight int32 = 8

// datagramSender abstracts the QUIC connection methods used by quicUDPConn
// and serverUDPConn. Enables testing with mock connections.
type datagramSender interface {
	SendDatagram(p []byte) error
	LocalAddr() net.Addr
}

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
//
// Both locks are sync.Mutex (not deadlock.Mutex) because they legitimately
// hold across I/O: initMu spans OpenStreamSync + header writes (up to 10s),
// writeMu spans WriteSmuxUDPFrame which can block under peer flow control.
// Tracking them as state locks would trigger spurious POTENTIAL DEADLOCK →
// os.Exit(2) crashes under sustained slow consumers (same class of bug as
// serverSmuxWriter in tcpws.go).
//
// Same stream-level isolation as serverSmuxWriter: per-Write deadline +
// active stream kill on failure (`dead` flag bypasses the mutex for fast
// fail-out, closeOnce ensures Close is invoked exactly once).
type udpOverflowStream struct {
	qconn     *quic.Conn
	initMu    sync.Mutex
	writeMu   sync.Mutex
	stream    *quic.Stream
	initErr   error
	opened    bool
	closeOnce sync.Once
	dead      atomic.Bool
}

// init lazily opens the overflow stream on first call. Subsequent calls are no-ops.
func (o *udpOverflowStream) init() error {
	o.initMu.Lock()
	defer o.initMu.Unlock()

	if o.opened {
		return o.initErr
	}
	o.opened = true

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stream, err := o.qconn.OpenStreamSync(ctx)
	if err != nil {
		o.initErr = fmt.Errorf("wire: open overflow stream: %w", err)
		slog.Warn("udpOverflowStream: open failed", "err", o.initErr)
		return o.initErr
	}

	// Write H3 frame type + StreamTypeUDPOverflow header
	if err := WriteH3ProxyFrameType(stream); err != nil {
		stream.CancelRead(0)
		stream.Close()
		o.initErr = fmt.Errorf("wire: write overflow H3 frame: %w", err)
		return o.initErr
	}
	if err := WriteStreamHeader(stream, StreamTypeUDPOverflow, ""); err != nil {
		stream.CancelRead(0)
		stream.Close()
		o.initErr = fmt.Errorf("wire: write overflow header: %w", err)
		return o.initErr
	}

	o.stream = stream
	slog.Info("udpOverflowStream: opened", "streamID", stream.StreamID())
	return nil
}

// send writes a single UDP frame to the overflow stream, lazily opening it on first call.
//
// Concurrency contract:
//   - dead.Store happens under writeMu (so subsequent writers see it before
//     they get past the in-lock re-check)
//   - closeStreamOnce runs OUTSIDE writeMu and OUTSIDE the writeFrame lock
//     ordering. It uses initMu only to safely read o.stream (set inside init()).
//   - Therefore writeMu is NEVER held while acquiring initMu — no nested
//     acquisition, lock-order graph unchanged.
func (o *udpOverflowStream) send(sessionID uint32, addr string, payload []byte) error {
	if o.dead.Load() {
		return errStreamDead
	}
	if err := o.init(); err != nil {
		return err
	}

	o.writeMu.Lock()
	if o.dead.Load() {
		o.writeMu.Unlock()
		return errStreamDead
	}

	_ = o.stream.SetWriteDeadline(time.Now().Add(streamWriteDeadline))
	start := time.Now()
	err := WriteSmuxUDPFrame(o.stream, sessionID, addr, payload)
	if err != nil {
		o.dead.Store(true)
	}
	o.writeMu.Unlock()

	if err != nil {
		writeMs := time.Since(start).Milliseconds()
		slog.Warn("DIAG: stream-killed-on-write-fail",
			"transport", "quic-overflow", "sessionID", sessionID,
			"len", len(payload), "writeMs", writeMs, "err", err)
		o.closeStreamOnce()
		return err
	}
	return nil
}

// markDead is the external entry point (e.g., from connection cleanup).
func (o *udpOverflowStream) markDead() {
	o.dead.Store(true)
	o.closeStreamOnce()
}

// closeStreamOnce closes the underlying stream exactly once. It acquires
// initMu to synchronize-read o.stream (which is set inside init() under
// initMu) and to nil it out for safe re-entry. It does NOT acquire writeMu.
func (o *udpOverflowStream) closeStreamOnce() {
	o.closeOnce.Do(func() {
		o.initMu.Lock()
		defer o.initMu.Unlock()
		if o.stream != nil {
			o.stream.CancelRead(0)
			_ = o.stream.Close()
			o.stream = nil
		}
	})
}

func (o *udpOverflowStream) close() {
	o.markDead()
}

// connUDPState holds shared UDP state for a single QUIC connection on the server.
// Both the datagram reader and overflow stream reader share this state so that
// packets from the same session ID are delivered to the same serverUDPConn.
type connUDPState struct {
	mu           deadlock.Mutex
	sessions     map[uint32]*serverUDPConn
	overflow     *udpOverflowStream
	done         chan struct{}
	sendInflight atomic.Int32 // concurrent SendDatagram calls in progress
	log          *slog.Logger // scoped logger with "remote" field
	dev          *deviceIDHolder
}

// runUDPHandler invokes handler.HandleUDP and removes the session from the
// map when it returns. ProxyHandler.HandleUDP returns within ~60s of target
// idle (handler.go:286 SetReadDeadline), so without this cleanup state.sessions
// grows monotonically and eventually hits maxServerUDPSessions (4096), silently
// dropping new flows for the rest of the QUIC connection's lifetime.
//
// closeOnce in serverUDPConn.close() makes the cleanup idempotent vs the
// connection-shutdown path in handleConn.
func (state *connUDPState) runUDPHandler(handler UDPHandler, sess *serverUDPConn, addr string) {
	defer func() {
		if r := recover(); r != nil {
			sess.log.Error("QUICServer: panic in HandleUDP", "panic", r)
		}
		state.mu.Lock()
		delete(state.sessions, sess.sessionID)
		state.mu.Unlock()
		sess.close()
	}()
	handler.HandleUDP(sess, addr)
}

// DefaultQUICConfig returns the QUIC configuration for clients.
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
		InitialPacketSize:              1400,
	}
}

// ServerQUICConfig returns the QUIC configuration for servers.
// Server uses a longer idle timeout (90s) than client (30s) so that under
// packet loss the CLIENT always times out first. This ensures the client
// detects the dead connection via conn.Context().Done() in connect() and
// rebuilds immediately, instead of the server closing first and the client
// opening streams into a dead connection (EOF) for 30-100s.
func ServerQUICConfig() *quic.Config {
	cfg := DefaultQUICConfig()
	cfg.MaxIdleTimeout = 90 * time.Second
	return cfg
}

// quicStreamConn wraps a quic.Stream to satisfy net.Conn.
type quicStreamConn struct {
	*quic.Stream
	local     net.Addr
	remote    net.Addr
	br        *bufio.Reader // set after parsing stream header
	closeOnce sync.Once
	closeErr  error
	dev       *deviceIDHolder
}

func (c *quicStreamConn) LocalAddr() net.Addr  { return c.local }
func (c *quicStreamConn) RemoteAddr() net.Addr { return c.remote }

// DeviceID implements ConnDeviceID. Returns "" if no holder is wired
// (defensive) or if the metadata stream hasn't landed yet.
func (c *quicStreamConn) DeviceID() string {
	if c.dev == nil {
		return ""
	}
	return c.dev.get()
}

// SessionID implements ConnSessionID. "" until the metadata verdict lands.
func (c *quicStreamConn) SessionID() string {
	if c.dev == nil {
		return ""
	}
	return c.dev.session()
}

// Read uses the buffered reader if present (to consume any data
// buffered during header parsing), otherwise reads from the stream directly.
func (c *quicStreamConn) Read(b []byte) (int, error) {
	if c.br != nil {
		return c.br.Read(b)
	}
	return c.Stream.Read(b)
}

// CloseWrite closes only the write direction (sends FIN).
// The read direction stays open so the peer can finish sending data.
// Used by pipe() for half-close; full cleanup is the caller's defer Close().
func (c *quicStreamConn) CloseWrite() error {
	return c.Stream.Close() // FIN only, no CancelRead
}

// Close closes both read and write directions of the QUIC stream.
// quic-go's Stream.Close() only sends FIN (write direction).
// Without CancelRead, the receive half never completes and the
// stream slot is never released — causing slot exhaustion under
// rapid short-lived connections (e.g. YouTube Shorts scrolling).
// Idempotent via sync.Once — safe to call after CloseWrite().
func (c *quicStreamConn) Close() error {
	c.closeOnce.Do(func() {
		c.Stream.CancelRead(0)
		c.closeErr = c.Stream.Close()
	})
	return c.closeErr
}

// ============================================================================
// QUICClient — implements wire.Dialer
// ============================================================================

// QUICClient is the QUIC/H3 client transport.
type QUICClient struct {
	cfg          Config
	jar          *CookieJar // per-outbound cookie store; nil = no cookie support (probe/p2p)
	serverAddr   string
	tlsConfig    *tls.Config // override for testing (bypasses BuildClientTLSConfig)
	directDialer DirectDialer

	mu        deadlock.Mutex
	connectMu deadlock.Mutex // serializes expensive connect path; ordering: connectMu → mu
	initUDPMu deadlock.Mutex // serializes UDP mux lazy init; ordering: initUDPMu → mu
	conn      *quic.Conn
	transport *quic.Transport
	alloc     *SessionIDAllocator
	udpMux    *quicUDPMux
	closed    bool
	ccSender  *k2cc.K2CCSender // stored for stats access

	// udpMuxRebuilds counts dead stream-UDP mux evictions, reported by the
	// udp-mux-rebuild DIAG event.
	udpMuxRebuilds atomic.Uint64

	lastTCPSuccessAt    atomic.Int64 // UnixNano of last successful DialTCP (0 = unknown)
	handshakeFailStreak atomic.Int32 // consecutive connect() handshake failures across reconnect attempts
}

// NewQUICClient creates a new QUIC client transport. jar is the per-outbound
// CookieJar shared with the sibling TCP-WS client (nil disables cookies) —
// this parameter slot is the same one master used for *TicketStore.
func NewQUICClient(cfg Config, jar *CookieJar) *QUICClient {
	dialHost := cfg.Host
	if cfg.DialAddr != "" {
		dialHost = cfg.DialAddr
	}
	return &QUICClient{
		cfg:        cfg,
		jar:        jar,
		serverAddr: net.JoinHostPort(dialHost, strconv.Itoa(cfg.Port)),
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

// ConsecConnFails returns the number of consecutive connection handshake failures.
func (c *QUICClient) ConsecConnFails() int32 {
	return c.handshakeFailStreak.Load()
}

// RecordSuccess clears the handshake failure streak after a successful QUIC dial.
// Called by TransportManager when QUIC DialTCP succeeds.
func (c *QUICClient) RecordSuccess() {
	c.handshakeFailStreak.Store(0)
}

// RecordTCPSuccess marks the current time as a successful TCP stream.
// Exported for testing — allows engine tests to set TCP health state.
func (c *QUICClient) RecordTCPSuccess() {
	c.lastTCPSuccessAt.Store(time.Now().UnixNano())
}

// RunEchoProbe implements EchoProber for data-path verification.
func (c *QUICClient) RunEchoProbe(ctx context.Context) error {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn == nil {
		return errors.New("no cached connection")
	}
	_, err := EchoProbe(ctx, conn)
	return err
}

// tcpRecentlyHealthy returns true if a TCP stream succeeded within the given window.
// Used by DialUDP to skip destructive reconnects when TCP streams are healthy
// (indicating normal packet loss, not a broken UDP path).
func (c *QUICClient) tcpRecentlyHealthy(window time.Duration) bool {
	ts := c.lastTCPSuccessAt.Load()
	if ts == 0 {
		return false
	}
	return time.Since(time.Unix(0, ts)) < window
}

// connect establishes a QUIC connection if not already connected.
// Uses check-release-connect-reacquire pattern to avoid holding mu
// during blocking I/O (DNS, UDP bind, QUIC handshake). connectMu
// serializes the expensive connect path so only one goroutine dials.
func (c *QUICClient) connect(ctx context.Context) (*quic.Conn, error) {
	// Fast path: return cached live connection under mu.
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, ErrNotConnected
	}
	if c.conn != nil {
		select {
		case <-c.conn.Context().Done():
			slog.Info("QUICClient: cached connection dead, rebuilding")
			mux, oldConn, tr := c.extractConnLocked()
			c.mu.Unlock()
			closeQUICResources(mux, oldConn, tr, "connection reset")
			goto serialize
		default:
			conn := c.conn
			c.mu.Unlock()
			return conn, nil
		}
	}
	c.mu.Unlock()

serialize:
	// Serialize expensive connect attempts — only one goroutine dials.
	c.connectMu.Lock()
	defer c.connectMu.Unlock()

	// Re-check: another goroutine may have connected while we waited.
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, ErrNotConnected
	}
	if c.conn != nil {
		select {
		case <-c.conn.Context().Done():
			mux, oldConn, tr := c.extractConnLocked()
			c.mu.Unlock()
			closeQUICResources(mux, oldConn, tr, "connection reset")
			c.mu.Lock()
			if c.closed {
				c.mu.Unlock()
				return nil, ErrNotConnected
			}
		default:
			conn := c.conn
			c.mu.Unlock()
			return conn, nil
		}
	}
	// Read immutable config fields under mu for address computation.
	cfg := c.cfg
	directDialer := c.directDialer
	tlsConfig := c.tlsConfig
	c.mu.Unlock()

	// --- All blocking I/O below — NO lock held ---

	addr := c.serverAddr // immutable after construction; fixed 443, no port hopping

	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("wire: resolve %s: %w", addr, err)
	}

	var udpConn net.PacketConn
	if directDialer != nil {
		udpConn, err = directDialer.ListenPacket("udp", ":0")
	} else {
		udpConn, err = net.ListenUDP("udp", &net.UDPAddr{})
	}
	if err != nil {
		return nil, fmt.Errorf("wire: listen UDP: %w", err)
	}

	transport := &quic.Transport{Conn: udpConn}

	var tlsCfg *tls.Config
	if tlsConfig != nil {
		tlsCfg = tlsConfig.Clone()
	} else {
		tlsCfg, _, err = BuildClientTLSConfig(cfg)
		if err != nil {
			transport.Close()
			return nil, err
		}
	}
	if len(tlsCfg.NextProtos) == 0 {
		tlsCfg.NextProtos = []string{"h3"}
	}

	qcfg := DefaultQUICConfig()
	// Shape the ClientHello to Chrome-over-QUIC. The factory runs per dial:
	// each connection needs its own spec, because kuic writes this connection's
	// initial_source_connection_id into it. A dial with no fingerprint is worse
	// than no dial — the QUIC leg would go out looking like nothing else on the
	// internet — so a spec failure fails the dial rather than falling back to
	// the unshaped handshake.
	qcfg.NewClientHelloSpec = buildChromeH3Spec
	// Shape the Initial packets around it. Without this the ClientHello looks
	// like Chrome inside packets that do not: quic-go picks a *random* Initial
	// destination connection ID length in [8,20] (varying run to run, which no
	// real client does) and starts packet numbering at 0 where Chrome starts
	// at 1. Both are readable from the long header.
	qcfg.InitialPacketSpec = chromeH3InitialPacketSpec()

	// Wrap transport.Dial in a goroutine with hard timeout.
	// quic-go's Transport.Dial internally calls Conn.destroy() on context cancel,
	// which blocks on <-c.ctx.Done() waiting for conn.run() exit (up to 30s).
	// This hard timeout prevents connectMu starvation.
	//
	// Orphan goroutine lifecycle (bounded, not a permanent leak):
	//   ctx has 5s timeout (quicContext with hasFallback=true) → Dial cancel
	//   at t=5s → Conn.destroy waits conn.run() exit (≤MaxIdleTimeout 30s)
	//   → Dial returns by ~t=35s → cleanup goroutine runs closeQUICResources
	//   (5s timeout) → all goroutines exit by ~t=40s.
	//   Accumulation capped at ~4 goroutines: connectMu serializes to 1
	//   connect() per 15s, and 3 consecutive fails activate TCP-WS fallback
	//   (stops QUIC attempts entirely). engine.Stop() cancels ctx chain,
	//   accelerating cleanup.
	//   Grep "DIAG: quic-dial-orphan-cleanup" to verify orphans are resolved.
	type dialResult struct {
		conn *quic.Conn
		err  error
	}
	dialDone := make(chan dialResult, 1)
	dialStart := time.Now()
	go func() {
		conn, err := transport.Dial(ctx, udpAddr, tlsCfg, qcfg)
		dialDone <- dialResult{conn, err}
	}()

	// NOTE: `err` is already declared in outer scope (from ResolveUDPAddr/ListenPacket).
	// `conn` is freshly declared here. The `conn, err = r.conn, r.err` assignment (no :=)
	// reuses the existing `err` variable.
	var conn *quic.Conn
	select {
	case r := <-dialDone:
		conn, err = r.conn, r.err
	case <-time.After(quicDialHardTimeout):
		// transport.Dial is stuck (likely in Conn.destroy). Orphan the goroutine
		// and let it clean up asynchronously using the bounded-timeout helpers.
		go func() {
			orphanStart := time.Now()
			r := <-dialDone
			slog.Warn("DIAG: quic-dial-orphan-cleanup",
				"addr", addr,
				"blockedMs", time.Since(orphanStart).Milliseconds(),
			)
			closeQUICResources(nil, r.conn, transport, "orphaned after hard timeout")
		}()
		c.handshakeFailStreak.Add(1)
		slog.Warn("DIAG: quic-handshake-fail",
			"addr", addr,
			"consecutiveFails", c.handshakeFailStreak.Load(),
			"err", "hard timeout (transport.Dial blocked)",
		)
		return nil, fmt.Errorf("wire: QUIC dial: hard timeout after %v", quicDialHardTimeout)
	}

	if err != nil {
		transport.Close()
		c.handshakeFailStreak.Add(1)
		slog.Warn("DIAG: quic-handshake-fail",
			"addr", addr,
			"consecutiveFails", c.handshakeFailStreak.Load(),
			"err", err,
		)
		return nil, fmt.Errorf("wire: QUIC dial: %w", err)
	}
	c.handshakeFailStreak.Store(0)
	slog.Info("wire: QUIC connected",
		"dialMs", time.Since(dialStart).Milliseconds(),
		"remoteAddr", conn.RemoteAddr().String(),
	)

	// Set K2CC congestion control (replaces default Cubic)
	sender := k2cc.NewCCSenderGFWMode(k2cc.DefaultMaxRate)
	conn.SetCongestionControl(sender)
	slog.Info("wire: QUIC K2CC enabled", "maxRateMbps", k2cc.DefaultMaxRate/1_000_000)

	// Send metadata stream before storing connection (no lock held — Rule #2).
	// Must happen before any other streams so the server can authenticate and
	// attribute this connection. Sent by ALL clients (normal + gateway), not
	// just gateway — previously gated on Mode!="", which left normal clients
	// completely unauthenticated on the QUIC path (the P0 this fix closes).
	//
	// Single shared orchestration (metadata_client.go): transport-layer errors
	// stay best-effort — the server's enforce gate, not the client, is the
	// authority; but an EXPLICIT server rejection (only possible under
	// enforce — permissive servers always answer OK) must fail connect() with
	// the structured code instead of limping into a closed connection.
	mdResp, mdErr := sendMetadata(ctx, quicMetaTransport{conn: conn}, MetadataFrame{
		UDID:    c.cfg.UDID,
		Token:   c.cfg.Token,
		Mode:    c.cfg.Mode,
		Version: c.cfg.Version,
		Cookie:  c.jar.Get(c.cfg.Pin), // nil-safe; "" = cold auth
	})
	if mdErr != nil {
		if isMetadataReject(mdErr) {
			closeQUICConnSafe(conn, "metadata auth rejected")
			transport.Close()
			// ProxyLayerError: the wire transport worked (stream opened, server
			// responded with an explicit rejection) — TCP-WS fallback to the same
			// server/credentials cannot help. Matches the established convention
			// at :714 for the proxy-stream rejection path.
			return nil, &ProxyLayerError{Err: fmt.Errorf("%w: %w", ErrMetadataRejected, mdErr)}
		}
		slog.Warn("QUICClient: metadata stream failed (best-effort)", "mode", c.cfg.Mode, "err", mdErr)
	}
	// newCookie is non-empty only right after a Center cold auth (spec §6.3);
	// jar.Put is a no-op on empty. Never log cookie/newCookie/meta.Token
	// (spec §6.2/§9).
	c.jar.Put(c.cfg.Pin, mdResp.Cookie)

	// --- Store result under lock ---
	// concurrency:allow — closing locally-owned conn/transport not yet stored in struct;
	// defer scope: mu acquired at tail of function, actual critical section is ~15 lines
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		closeQUICConnSafe(conn, "closed during connect")
		transport.Close()
		return nil, ErrNotConnected
	}
	c.conn = conn
	c.transport = transport
	c.ccSender = sender
	c.alloc = NewSessionIDAllocator()
	c.udpMux = newQuicUDPMux(conn)
	// Start accept loop for server-initiated overflow streams
	go c.acceptOverflowStreams(conn, c.udpMux)

	return conn, nil
}

// DialTCP opens a TCP proxy stream through the QUIC connection.
// Writes H3 frame type + stream header, reads response, returns the stream.
func (c *QUICClient) DialTCP(ctx context.Context, addr string, earlyData []byte) (net.Conn, error) {
	slog.Debug("QUICClient.DialTCP: connecting", "addr", addr)
	conn, err := c.connect(ctx)
	if err != nil {
		slog.Debug("QUICClient.DialTCP: connect failed", "addr", addr, "err", err)
		return nil, err
	}

	slog.Debug("QUICClient.DialTCP: opening stream", "addr", addr)
	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		// Dead connections are detected by connect() via conn.Context().Done().
		// No manual resetConnection() needed here.
		slog.Debug("QUICClient.DialTCP: stream open failed", "addr", addr, "err", err)
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
	// Write early data (e.g. TLS ClientHello) before waiting for server response.
	// Server's bufio.Reader buffers this during header parsing; pipe() consumes it
	// after upstream dial succeeds. Saves one RTT on high-latency links.
	if len(earlyData) > 0 {
		if _, err := stream.Write(earlyData); err != nil {
			stream.CancelRead(0)
			stream.Close()
			return nil, fmt.Errorf("wire: write early data: %w", err)
		}
	}
	slog.Debug("QUICClient.DialTCP: header written, waiting for response", "addr", addr, "earlyDataLen", len(earlyData))

	// Read response with timeout. Server dial timeout is 5s — allow 5s for
	// dial + response write. Successful dials complete in <1s; 5s is generous.
	// Without this, a half-dead QUIC connection hangs forever.
	stream.SetReadDeadline(time.Now().Add(5 * time.Second))
	br := bufio.NewReader(stream)
	// Past this point, the request has been sent to the server. The wire
	// transport (k2 ↔ k2s) is proven working. All errors are proxy-layer
	// (server processing) — TCP-WS fallback to the same server won't help.
	ok, errCode, errMsg, err := ReadStreamResponse(br)
	if err != nil {
		slog.Warn("QUICClient.DialTCP: read response failed", "addr", addr, "err", err)
		stream.Close()
		// errors.As, not a bare assertion: ReadStreamResponse wraps the
		// deadline error, so `err.(net.Error)` never matched and this line
		// silently never fired (see TestReadResponseError_RealQUICStreamDeadline).
		var nErr net.Error
		if errors.As(err, &nErr) && nErr.Timeout() {
			slog.Warn("QUICClient.DialTCP: stream response timeout (server upstream slow, or the UDP path drops the request)", "addr", addr)
		}
		return nil, readResponseError(err)
	}
	stream.SetReadDeadline(time.Time{}) // clear deadline for data transfer
	if !ok {
		slog.Debug("QUICClient.DialTCP: stream rejected", "addr", addr, "errCode", errCode, "errMsg", errMsg)
		stream.Close()
		if errCode > 0 {
			return nil, &StreamError{Code: errCode, Message: errMsg}
		}
		return nil, &ProxyLayerError{Err: fmt.Errorf("%w: %s", ErrStreamRejected, errMsg)}
	}

	slog.Debug("QUICClient.DialTCP: success", "addr", addr)
	c.lastTCPSuccessAt.Store(time.Now().UnixNano())
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
// When the datagram relay path is unhealthy (consecutive relay timeouts),
// returns ErrDatagramRelayUnhealthy for TCP-WS fallback — except for one trial
// session per relayTrialInterval, which is how the path gets to prove itself
// again. Relay-layer metrics never trigger wire-layer actions (reconnect,
// transport switch).
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
		slog.Debug("QUICClient.DialUDP", "addr", addr, "datagramsSupported", dgSupported)
		if dgSupported {
			admitted, trial := mux.admitSession()
			if !admitted {
				slog.Debug("QUICClient.DialUDP: datagram relay unhealthy",
					"addr", addr,
					"relayTimeouts", mux.relayTimeouts.Load())
				return nil, ErrDatagramRelayUnhealthy
			}
			if trial {
				slog.Info("QUICClient.DialUDP: datagram relay unhealthy, trial session",
					"addr", addr,
					"relayTimeouts", mux.relayTimeouts.Load())
			}

			sessionID := alloc.Next()
			readCh, reaped := mux.register(sessionID)

			return &quicUDPConn{
				qconn:      conn,
				sessionID:  sessionID,
				targetAddr: addr,
				mux:        mux,
				created:    time.Now(),
				readCh:     readCh,
				reaped:     reaped,
				done:       make(chan struct{}),
			}, nil
		}

		// Fallback: stream-based UDP (datagrams not supported by server)
		return c.dialUDPStream(ctx, conn, addr)
	}
}

// DialStreamUDP opens a stream-backed UDP session through QUIC streams.
// Unlike DialUDP which prefers DATAGRAM (unreliable), this always uses streams
// (reliable, congestion-controlled). Used for DNS queries where packet loss
// causes 5s timeouts.
func (c *QUICClient) DialStreamUDP(ctx context.Context, addr string) (net.PacketConn, error) {
	conn, err := c.connect(ctx)
	if err != nil {
		return nil, err
	}
	return c.dialUDPStream(ctx, conn, addr)
}

// dialUDPStream opens a QUIC stream for UDP and returns a stream-based PacketConn.
// Uses check-release-act-reacquire to avoid holding mu during blocking I/O
// (OpenStreamSync, WriteH3ProxyFrameType, WriteStreamHeader).
func (c *QUICClient) dialUDPStream(ctx context.Context, conn *quic.Conn, addr string) (net.PacketConn, error) {
	// Fast path: a live mux is already initialized.
	c.mu.Lock()
	if c.udpMux == nil || c.alloc == nil {
		c.mu.Unlock()
		return nil, ErrNotConnected
	}
	live, evicted := c.takeLiveStreamMuxLocked()
	alloc := c.alloc
	c.mu.Unlock()
	c.discardDeadStreamMux(evicted)
	if live != nil {
		return c.newStreamUDPConn(live, alloc, addr), nil
	}

	// Slow path: initialize UDP stream mux. Serialized by initUDPMu.
	c.initUDPMu.Lock()
	defer c.initUDPMu.Unlock()

	// Re-check: another goroutine may have initialized while we waited.
	c.mu.Lock()
	if c.udpMux == nil || c.alloc == nil {
		c.mu.Unlock()
		return nil, ErrNotConnected
	}
	live, evicted = c.takeLiveStreamMuxLocked()
	alloc = c.alloc
	c.mu.Unlock()
	c.discardDeadStreamMux(evicted)
	if live != nil {
		return c.newStreamUDPConn(live, alloc, addr), nil
	}

	// Blocking I/O — no lock held.
	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		slog.Debug("QUICClient.dialUDPStream: stream open failed", "addr", addr, "err", err)
		return nil, fmt.Errorf("wire: open UDP stream: %w", err)
	}
	if err := WriteH3ProxyFrameType(stream); err != nil {
		stream.Close()
		return nil, fmt.Errorf("wire: write H3 frame type: %w", err)
	}
	if err := WriteStreamHeader(stream, StreamTypeUDP, ""); err != nil {
		stream.Close()
		return nil, fmt.Errorf("wire: write UDP stream header: %w", err)
	}

	// Store result under lock. Re-check for concurrent resetConnection.
	// The connection may also have been replaced while we dialed; a mux
	// bound to the superseded conn could never carry a frame.
	c.mu.Lock()
	if c.udpMux == nil || c.conn != conn {
		c.mu.Unlock()
		stream.Close()
		return nil, ErrNotConnected
	}
	c.udpMux.streamMux = newQuicStreamUDPMux(stream, conn)
	mux := c.udpMux.streamMux
	alloc = c.alloc
	c.mu.Unlock()

	return c.newStreamUDPConn(mux, alloc, addr), nil
}

// takeLiveStreamMuxLocked returns the cached stream-UDP mux if it is still
// usable, evicting it if it has died. A dead mux comes back as `evicted` and
// must be disposed of via discardDeadStreamMux with no lock held.
//
// Mirror of TCPWSClient.takeLiveUDPMuxLocked — same trap: the cached mux is
// otherwise only cleared when the whole QUIC *connection* resets, so a
// stream-level death would strand every subsequent UDP send (#3420).
//
// Caller must hold c.mu and have checked c.udpMux != nil.
func (c *QUICClient) takeLiveStreamMuxLocked() (live, evicted *quicStreamUDPMux) {
	m := c.udpMux.streamMux
	if m == nil {
		return nil, nil
	}
	if !m.dead.Load() {
		return m, nil
	}
	c.udpMux.streamMux = nil
	return nil, m
}

// discardDeadStreamMux disposes of an evicted mux and records the rebuild.
// Must be called with no lock held.
func (c *QUICClient) discardDeadStreamMux(m *quicStreamUDPMux) {
	if m == nil {
		return
	}
	// Also closes m.done, releasing consumers parked in ReadFrom.
	m.close()
	slog.Warn("DIAG: udp-mux-rebuild",
		"transport", "quic-stream",
		"reason", m.deathReason(),
		"rebuilds", c.udpMuxRebuilds.Add(1),
	)
}

// newStreamUDPConn creates a quicStreamUDPConn with a new session ID.
func (c *QUICClient) newStreamUDPConn(mux *quicStreamUDPMux, alloc *SessionIDAllocator, addr string) *quicStreamUDPConn {
	sessionID := alloc.Next()
	readCh, reaped := mux.register(sessionID)
	return &quicStreamUDPConn{
		mux:        mux,
		sessionID:  sessionID,
		targetAddr: addr,
		readCh:     readCh,
		reaped:     reaped,
		done:       make(chan struct{}),
	}
}

// WireStats returns a snapshot of QUIC transport metrics including K2CC state.
func (c *QUICClient) WireStats() WireStats {
	c.mu.Lock()
	sender := c.ccSender
	mux := c.udpMux
	var streamMux *quicStreamUDPMux
	if mux != nil {
		streamMux = mux.streamMux
	}
	c.mu.Unlock()

	s := WireStats{Transport: "quic"}
	if sender != nil {
		snap := sender.Snapshot()
		s.PacketLossRate = snap.LossRate
		s.Throughput = snap.Throughput
		s.SendingRate = snap.SendingRate
		s.RTT = snap.RTT
		s.MinRTT = snap.MinRTT
		s.Cwnd = snap.Cwnd
		s.Mode = int(snap.Mode)
		s.MIID = snap.MIID
		s.Epsilon = snap.Epsilon
		s.CC = sender.Summary()
	}
	if mux != nil {
		s.RelayUDPTimeouts = mux.relayTimeouts.Load()
		s.UDPTxDrops = mux.udpTxDrops.Load()
		s.UDPRxDrops = mux.udpRxDrops.Load()
		s.UDPRxDropsNoSession = mux.udpRxDropsNoSession.Load()
		// Sessions on a datagram-less server run over the stream-UDP mux
		// instead; their drops belong to the same connection, so fold them
		// in rather than reporting a structural 0 for those sessions.
		// streamMux is written under c.mu (dialUDPStream), so it is read in
		// the same critical section that captured mux above.
		if sm := streamMux; sm != nil {
			s.UDPRxDrops += sm.udpRxDrops.Load()
			s.UDPRxDropsNoSession += sm.udpRxDropsNoSession.Load()
		}
	}
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

// extractConnLocked nils all connection resource fields and returns them.
// Caller must hold c.mu. Resources must be closed OUTSIDE the lock.
func (c *QUICClient) extractConnLocked() (mux *quicUDPMux, conn *quic.Conn, tr *quic.Transport) {
	mux = c.udpMux
	c.udpMux = nil
	conn = c.conn
	c.conn = nil
	tr = c.transport
	c.transport = nil
	c.alloc = nil
	c.ccSender = nil
	c.lastTCPSuccessAt.Store(0)
	return
}

// quicCloseTimeout bounds quic.Conn.CloseWithError and quic.Transport.Close.
// quic-go's Conn.destroy() waits for its run loop via <-c.ctx.Done(), which
// blocks indefinitely when the network is unreachable (sendQueue.Run stuck
// on a dead UDP write). 5s is 50x normal close time (<100ms).
const quicCloseTimeout = 5 * time.Second

// quicMetadataRejectDrainDelay bounds the async delay between closing the
// metadata stream (rejection response written) and closing the connection.
// See handleMetadataStream's enforce-rejection branch: without this window,
// CloseWithError can race ahead of the still-unflushed stream FIN.
const quicMetadataRejectDrainDelay = 50 * time.Millisecond

// metadataStreamDrainTimeout bounds how long handleMetadataStream waits for
// the client's own FIN before falling back to CancelRead as a leak backstop.
// See handleMetadataStream's deferred cleanup.
const metadataStreamDrainTimeout = 200 * time.Millisecond

// quicDialHardTimeout bounds the total time connect() holds connectMu.
// Must be well under go-deadlock's 60s threshold. Covers: UDP listen +
// QUIC handshake + Conn.destroy cleanup on failure. If transport.Dial
// blocks beyond this (e.g. Conn.destroy waiting for conn.run exit),
// the goroutine is orphaned and cleanup happens asynchronously.
const quicDialHardTimeout = 15 * time.Second

// closeQUICConnSafe closes a QUIC connection with a bounded timeout.
// If CloseWithError blocks (quic-go Conn.destroy bug when network is dead),
// the goroutine is orphaned and the function returns after timeout.
// The orphaned goroutine is harmless — it holds only quic-go internal state
// and will be collected when the process exits.
func closeQUICConnSafe(conn *quic.Conn, reason string) {
	done := make(chan struct{})
	go func() {
		conn.CloseWithError(0, reason)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(quicCloseTimeout):
		slog.Warn("wire: QUIC conn close timed out, orphaning goroutine",
			"reason", reason, "timeout", quicCloseTimeout)
	}
}

// closeQUICResources closes extracted QUIC connection resources.
// Safe to call with nil arguments. Must be called WITHOUT holding c.mu.
func closeQUICResources(mux *quicUDPMux, conn *quic.Conn, tr *quic.Transport, reason string) {
	if mux != nil {
		mux.close()
	}
	if conn != nil {
		closeQUICConnSafe(conn, reason)
	}
	if tr != nil {
		done := make(chan struct{})
		go func() {
			tr.Close()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(quicCloseTimeout):
			slog.Warn("wire: QUIC transport close timed out", "reason", reason)
		}
	}
}

// resetConnection closes cached connection resources without marking client as closed.
// Used only for intentional resets (network change) — NOT for error recovery.
// Dead connections are detected by connect() via conn.Context().Done().
func (c *QUICClient) resetConnection() {
	c.mu.Lock()
	mux, conn, tr := c.extractConnLocked()
	c.mu.Unlock()
	closeQUICResources(mux, conn, tr, "connection reset")
}

// Reset closes cached connections and clears all fallback state.
// Called by TransportManager.Reset() on network change.
// After Reset(), the next DialTCP/DialUDP will establish a fresh connection
// with a clean handshake failure streak.
func (c *QUICClient) Reset() {
	c.resetConnection()
	c.handshakeFailStreak.Store(0)
}

// Close closes the QUIC connection and underlying transport.
func (c *QUICClient) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	mux, conn, tr := c.extractConnLocked()
	c.mu.Unlock()
	closeQUICResources(mux, conn, tr, "client close")
	return nil
}

// ============================================================================
// QUICServer — implements wire.Listener
// ============================================================================

type tcpAcceptResult struct {
	conn net.Conn
	addr string
	err  error
}

// authWaitDefault bounds how long a proxy stream waits for a late metadata
// stream under enforce before deciding the connection is unauthenticated. The
// client sends metadata synchronously inside connect() before any proxy stream,
// so on the wire metadata always precedes proxy streams; this only covers the
// server accepting the two streams out of order. Kept short so a legacy client
// (which never sends metadata) isn't stalled long before rejection.
const authWaitDefault = 500 * time.Millisecond

// QUICServer is the QUIC/H3 server transport.
type QUICServer struct {
	validate Validator       // set by SetValidator; used by metadata stream auth
	adm      admissionPolicy // device-auth admission; see admission.go

	ccInterval time.Duration // cc-summary cadence, ccSummaryInterval at construction

	// cookieSigner enables the local cookie fast path; nil = cold auth only
	// (legacy TLS mode without cert_dir, k2p). Immutable after SetCookieSigner
	// (called before Listen), no lock needed on the read path.
	cookieSigner *CookieSigner

	// sessions is the live-connection registry + revalidation sweeper (spec
	// §6.5); nil = disabled (tests, k2p). Immutable after EnableRevalidation
	// (called before Listen).
	sessions *authSessions

	// rollout is the Phase 3 enforce-rollout counter set; set by
	// SetRolloutStats, immutable after (called before Listen). nil-safe (all
	// Record* are nil-receiver safe) — tests and callers without stats never
	// crash.
	rollout *RolloutStats

	mu    deadlock.Mutex
	ln    *quic.Listener
	tcpCh chan tcpAcceptResult
	// udpH is an egress sink: reach it only through admitUDP, never directly.
	udpH UDPHandler
	done chan struct{}
}

// NewQUICServer creates a new QUIC server transport.
func NewQUICServer() *QUICServer {
	return &QUICServer{
		tcpCh:      make(chan tcpAcceptResult, 64),
		done:       make(chan struct{}),
		adm:        admissionPolicy{authWait: authWaitDefault},
		ccInterval: ccSummaryInterval,
	}
}

// SetValidator sets the Validator used by metadata stream auth.
// Must be called before Listen.
func (s *QUICServer) SetValidator(v Validator) {
	s.validate = v
}

// SetCookieSigner enables node-local cookie issue/verify (spec §6). Must be
// called before Listen. nil (the default) disables the cookie fast path.
func (s *QUICServer) SetCookieSigner(cs *CookieSigner) { s.cookieSigner = cs }

// EnableRevalidation creates the connection registry and starts the periodic
// revalidation sweeper against v (pass the same cached validator chain used
// for cold auth so Center load is shared). Must be called before Listen;
// Close() tears the sweeper down.
func (s *QUICServer) EnableRevalidation(v Validator) {
	s.sessions = newAuthSessions(v, authSweepInterval)
}

// quicCloseShutdown is the QUIC application close code for "this server is
// exiting" (0x01 = unauthorized at admission, 0x03 = revoked by
// revalidation). Clients treat any close as a dead connection and redial on
// the next stream; the distinct code is for logs and for a client that wants
// to retry faster on a planned restart.
const quicCloseShutdown = 0x04

// CloseConnections severs every live connection with quicCloseShutdown, so
// clients learn at once that the server is going away instead of waiting out
// MaxIdleTimeout. Call it on shutdown after the listener stops accepting. It
// returns the number of connections closed and is bounded by
// closeConnectionsTimeout. A no-op before EnableRevalidation.
func (s *QUICServer) CloseConnections() int {
	if s.sessions == nil {
		return 0
	}
	return s.sessions.closeAll(closeShutdown)
}

// SetEnforce toggles QUIC device-auth enforcement. k2s (server.New) always
// sets true; false (the zero value, permissive) remains only for wire-level
// tests and the k2p P2P path, where the metadata stream never blocks a
// connection.
// Attribution still follows the verdict, not the self-report: only a passing
// verdict feeds ctx.dev, an unvalidated connection lands in the empty-udid
// bucket. When true, proxy streams on a connection that failed or omitted
// metadata auth are rejected. Must be called before Listen.
func (s *QUICServer) SetEnforce(v bool) {
	s.adm.enforce = v
}

// SetRolloutStats wires the Phase 3 enforce-rollout counters. Must be called
// before Listen; nil is safe (all Record* are nil-receiver safe).
func (s *QUICServer) SetRolloutStats(r *RolloutStats) { s.rollout = r }

// admitTCP is the only route from an accepted proxy stream to AcceptTCP, and so
// the whole of the TCP admission gate: on denial the client gets an
// `unauthorized` stream response instead of a hung stream. Nothing else may
// call sendTCP with a client-originated connection — see
// TestAdmissionGate_EgressSinksAreGated.
func (s *QUICServer) admitTCP(ctx *connContext, qconn *quic.Conn, stream *quic.Stream, br *bufio.Reader, addr string, log *slog.Logger) {
	if !s.adm.admit(ctx.auth) {
		log.Warn("QUICServer: TCP rejected (enforce: unauthenticated)", "addr", addr)
		WriteStreamResponse(stream, fmt.Errorf("unauthorized"))
		stream.Close()
		return
	}
	s.sendTCP(tcpAcceptResult{conn: &quicStreamConn{
		Stream: stream,
		local:  qconn.LocalAddr(),
		remote: qconn.RemoteAddr(),
		br:     br,
		dev:    ctx.dev,
	}, addr: addr})
	log.Debug("QUICServer.handleStream: TCP queued", "addr", addr)
}

// admitUDP is the only reader of s.udpH, and so the whole of the UDP admission
// gate: a UDP path that skips it has no handler to proxy with. That is what
// stops a newly added path from silently bypassing enforce, as the datagram and
// overflow paths once did.
//
// ok=false means the connection failed admission — drop the frame without
// creating a session. A nil handler with ok=true means ServeUDP has not been
// called yet, which is not an auth condition and keeps the pre-existing
// no-handler behaviour.
func (s *QUICServer) admitUDP(ctx *connContext) (handler UDPHandler, ok bool) {
	if !s.adm.admit(ctx.auth) {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.udpH, true
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
		// Set K2CC congestion control for server-side sending (critical for download speed).
		// The sender is handed to handleConn so the connection's cc-summary
		// loop can read its Summary() — before this, the server-side (download)
		// sender was unobservable.
		sender := k2cc.NewCCSenderVariant(pickCCVariant(), k2cc.DefaultMaxRate)
		conn.SetCongestionControl(sender)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("QUICServer: panic in handleConn", "panic", r, "remote", conn.RemoteAddr())
				}
			}()
			s.handleConn(conn, sender)
		}()
	}
}

// connContext holds per-QUIC-connection state shared across all streams.
type connContext struct {
	mode atomic.Value // string: "" (normal) or "gateway"
	dev  *deviceIDHolder
	udp  *connUDPState

	// auth is the device-auth verdict for this connection, consumed by the
	// admission gate (admitTCP / admitUDP). See admission.go.
	auth *connAuth

	// sess is this connection's registry entry (nil when revalidation is
	// disabled). Identity is published onto it by handleMetadataStream.
	sess *authSession

	// cc is the download-direction k2cc sender attached in acceptLoop, read
	// by ccSummaryLoop (cc_summary.go). nil on P2P server connections, which
	// keep kuic's default controller — no cc-summary is emitted for those.
	cc *k2cc.K2CCSender
}

// handleConn owns one server connection. cc is the download-direction k2cc
// sender attached in acceptLoop (nil for P2P connections that keep kuic's
// default controller — no cc-summary is emitted for those).
func (s *QUICServer) handleConn(conn *quic.Conn, cc *k2cc.K2CCSender) {
	connLog := slog.With("remote", conn.RemoteAddr())

	// Shared UDP state for datagram + overflow stream paths
	dev := &deviceIDHolder{}
	state := &connUDPState{
		sessions: make(map[uint32]*serverUDPConn),
		overflow: &udpOverflowStream{qconn: conn},
		done:     make(chan struct{}),
		log:      connLog,
		dev:      dev,
	}
	ctx := &connContext{udp: state, dev: dev, auth: newConnAuth(), cc: cc}
	ctx.mode.Store("") // default: normal client

	// Per-connection CC observability (cc_summary.go). Exits on state.done
	// or conn.Context().Done(), whichever comes first.
	go func() {
		defer func() {
			if r := recover(); r != nil {
				connLog.Error("QUICServer: panic in ccSummaryLoop", "panic", r)
			}
		}()
		s.ccSummaryLoop(connLog, ctx, conn, state.done)
	}()

	s.rollout.RecordConn()
	defer func() {
		// !settled() alone is not enough to decide "count as legacy": the
		// metadata goroutine may still be resolving its verdict (see
		// connAuth.rolloutCounted). claimRolloutCount() is the actual
		// tie-breaker — only the winner of that race increments a counter.
		if !ctx.auth.settled() && ctx.auth.claimRolloutCount() {
			s.rollout.RecordLegacyNoMetadata()
		}
	}()

	if s.sessions != nil {
		as := newAuthSession(ctx.auth, func(r closeReason) {
			if r == closeShutdown {
				conn.CloseWithError(quicCloseShutdown, "server restarting")
				return
			}
			// 0x03: revoked-by-revalidation (0x01 = unauthorized at admission).
			conn.CloseWithError(0x03, "session revoked")
		})
		ctx.sess = as
		unregister := s.sessions.register(as)
		defer unregister() // handleConn returns when the connection dies
	}

	// Accept streams (TCP proxy + UDP overflow + metadata)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				connLog.Error("QUICServer: panic in AcceptStream loop", "panic", r)
			}
		}()
		for {
			stream, err := conn.AcceptStream(context.Background())
			if err != nil {
				connLog.Info("QUICServer: AcceptStream done", "err", err)
				return
			}
			connLog.Debug("QUICServer: accepted stream", "streamID", stream.StreamID())
			go func() {
				defer func() {
					if r := recover(); r != nil {
						connLog.Error("QUICServer: panic in handleStream", "panic", r)
					}
				}()
				s.handleStream(conn, stream, ctx)
			}()
		}
	}()

	// Read datagrams (UDP proxy) — blocks until connection closes
	s.handleDatagrams(conn, ctx)

	// Cleanup: close all UDP sessions and overflow stream
	close(state.done)
	state.overflow.close()
	// concurrency:allow — sess.close() is closeOnce.Do(func(){ close(done) }), pure memory op
	state.mu.Lock()
	for _, sess := range state.sessions {
		sess.close()
	}
	state.mu.Unlock()
}

func (s *QUICServer) handleStream(qconn *quic.Conn, stream *quic.Stream, ctx *connContext) {
	log := ctx.udp.log.With("streamID", stream.StreamID())
	br := bufio.NewReader(stream)

	stream.SetReadDeadline(time.Now().Add(streamHeaderTimeout))

	// Read H3 frame type marker
	frameType, _, err := ReadVarint(br)
	if err != nil {
		log.Warn("QUICServer.handleStream: read frame type failed", "err", err)
		stream.Close()
		return
	}
	if frameType != H3FrameTypeK2Proxy {
		log.Debug("QUICServer.handleStream: unknown frame type", "frameType", frameType)
		stream.Close()
		return
	}

	// Read stream header (for meta: streamType only, addr is empty)
	streamType, addr, err := ReadStreamHeader(br)
	if err != nil {
		log.Warn("QUICServer.handleStream: read header failed", "err", err)
		stream.Close()
		return
	}

	log.Info("QUICServer.handleStream: parsed", "streamType", streamType, "addr", addr)
	if streamType != StreamTypeMeta { // metadata: the deadline covers its frame too
		stream.SetReadDeadline(time.Time{})
	}

	switch streamType {
	case StreamTypeMeta:
		s.handleMetadataStream(qconn, stream, br, ctx)
	case StreamTypeTCP:
		s.admitTCP(ctx, qconn, stream, br, addr, log)
	case StreamTypeUDP:
		s.handleUDPStream(stream, br, ctx, qconn.RemoteAddr().String())
	case StreamTypeUDPOverflow:
		s.handleUDPOverflowStream(br, ctx)
	case StreamTypeEcho:
		// Deliberately not gated: the echo stream reflects bytes back inside the
		// same QUIC connection and never reaches the network, so it grants an
		// unauthenticated peer no egress. Gating it would only add authWait to
		// the transport race's data-path probe.
		HandleEchoStream(stream, br)
	default:
		log.Debug("QUICServer.handleStream: unknown stream type", "streamType", streamType)
		stream.Close()
	}
}

// handleMetadataStream processes a connection metadata stream (StreamType
// 0x10). Mirrors TCPWSServer.handleMetadataStream.
//
// Rollout counting (spec §7.2): recordVerdict fires once, right after
// ctx.auth.settle(st.authed), covering the enforce-reject, permissive-accept,
// and success branches below. A pre-parse ReadMetadataFrame failure (bad
// metadata) returns before that point — the deferred ctx.auth.settle(false)
// still settles the connection (so it does NOT count as
// RecordLegacyNoMetadata, which only fires on a connection that never
// presented metadata at all), but it also never reaches recordVerdict. That
// leaves a small, documented residual bucket (parse-failed connections)
// uncounted by both cookie_hit/cold/unauthed and legacy_no_metadata — expect
// total != authed+unauthed+legacy_no_metadata by roughly that delta; it is
// not a counting bug.
//
// At-most-once counting (connAuth.rolloutCounted): this handler can race the
// accept loop's teardown defer (RecordLegacyNoMetadata fires if the
// connection dies while this goroutine is still blocked resolving a
// verdict), and nothing limits a client to a single metadata stream per
// connection, so this function can run more than once per connection. Both
// the teardown defer and the `recordVerdict` call below first claim
// ctx.auth.rolloutCounted via CAS and only proceed on a win — so every
// connection contributes at most one rollout-counter increment, no matter
// which of those two paths gets there first or how many metadata streams the
// client opens.
//
// Attribution follows the SERVER-side verdict, never the self-reported value:
// only a validated connection gets mode.Store/dev.set (a Center-validated
// udid is trustworthy — Center rejects device.UDID != udid), and a failed or
// absent validation lands in the empty-udid bucket (spec §5.3 udid 收口).
//
// Response contract (spec §5.2): permissive ALWAYS answers OK — rollout must
// never fail a legacy-credential client. Under enforce, an explicit rejection
// (Code 401/402/403 — only Center issues these) answers with the coded 0x02
// form so the client can classify without string matching; "cannot determine"
// and parse failures answer the legacy 0x01 form (no invented code). The
// verdict is always settled; the deferred settle is the fail-closed backstop.
func (s *QUICServer) handleMetadataStream(qconn *quic.Conn, stream *quic.Stream, br *bufio.Reader, ctx *connContext) {
	defer ctx.auth.settle(false)
	// Release the receive half when this handler returns: quic-go's
	// Stream.Close() only sends FIN (write side); without CancelRead the
	// receive half never completes and the stream slot leaks — the same leak
	// quicStreamConn.Close fixes for proxy streams (wire/CLAUDE.md). Drain to
	// EOF first (bounded): a well-behaved client (including an OLD client —
	// spec §5.1) closes its own send side right after reading the response,
	// so this lets that graceful FIN land normally — CancelRead is then a
	// no-op (quic-go only emits STOP_SENDING if the receive side hasn't
	// already reached EOF; a STOP_SENDING racing an in-flight peer Close()
	// resets the peer's send stream and makes ITS Close() return an error —
	// reproduced deterministically without the drain). If the client never
	// closes, the deadline expires and CancelRead is the real backstop.
	defer func() {
		stream.SetReadDeadline(time.Now().Add(metadataStreamDrainTimeout))
		io.Copy(io.Discard, br)
		stream.CancelRead(0)
	}()

	meta, err := ReadMetadataFrame(br)
	if err != nil {
		slog.Warn("QUICServer: metadata stream parse failed", "remote", qconn.RemoteAddr(), "err", err)
		WriteStreamResponse(stream, fmt.Errorf("bad metadata"))
		stream.Close()
		return
	}

	slog.Info("QUICServer: metadata stream", "udid", meta.UDID, "mode", meta.Mode, "version", meta.Version, "remote", qconn.RemoteAddr())

	// Single decision point shared verbatim with TCP-WS (spec §9 two-transport
	// consistency): a valid cookie short-circuits the validator chain entirely
	// and issues no new cookie; otherwise this runs cold auth through
	// s.validate exactly as before, now also minting a cookie on success.
	st := resolveMetadataAuth(s.cookieSigner, s.validate, meta, s.adm.enforce, time.Now())

	// Attribution from the SERVER-determined identity (spec §5.3): cookie's
	// udid on a hit, Center-confirmed udid on cold OK, "" otherwise.
	ctx.mode.Store(st.mode)
	ctx.dev.set(st.udid)
	ctx.dev.setSession(st.sessionID)
	ctx.auth.settle(st.authed)
	if ctx.auth.claimRolloutCount() {
		recordVerdict(s.rollout, st.cookieHit, st.authed)
	}

	if !st.authed {
		slog.Warn("QUICServer: metadata auth rejected", "udid", meta.UDID, "code", st.code, "enforce", s.adm.enforce, "remote", qconn.RemoteAddr())
		if s.adm.enforce {
			if st.code != 0 {
				WriteStreamResponse(stream, &StreamError{Code: st.code, Message: "auth rejected"})
			} else {
				WriteStreamResponse(stream, fmt.Errorf("unauthorized"))
			}
			stream.Close()
			// Tear the connection down, but not synchronously: quic-go's
			// SendStream.Close() only schedules the FIN for the packet
			// scheduler (send_stream.go onHasStreamData) — it does not wait
			// for the response bytes to actually reach the wire. Calling
			// CloseWithError inline races ahead of that and the client sees
			// a bare "Application error" instead of the coded/legacy
			// rejection body (reproduced deterministically without this
			// delay). The bounded async delay gives the scheduler a window
			// to flush first; the enforce gate (ctx.auth) — not this close —
			// is what actually prevents the rejected connection from
			// proxying anything in the meantime.
			safego.Go(func() {
				time.Sleep(quicMetadataRejectDrainDelay)
				qconn.CloseWithError(0x01, "unauthorized")
			})
			return
		}
		// Permissive: accept, no attribution (empty-udid bucket), never block.
		WriteMetadataResponse(stream, "", false) // no smux on QUIC
		stream.Close()
		return
	}

	// Publish before answering, as on TCP-WS (handleMetadataStream).
	if ctx.sess != nil {
		ctx.sess.markAuthed(st.udid, st.token, st.mode, st.authAt, st.svcExp)
	}
	// newCookie is non-empty ONLY after a Center cold auth (spec §6.3) — a
	// cookie hit answers with an empty kv and the client keeps its copy.
	WriteMetadataResponse(stream, st.newCookie, false) // no smux on QUIC
	stream.Close()
	slog.Info("QUICServer: metadata auth OK", "udid", st.udid, "mode", st.mode, "cookieHit", st.cookieHit, "remote", qconn.RemoteAddr())
}

func (s *QUICServer) handleUDPStream(stream *quic.Stream, br *bufio.Reader, ctx *connContext, remoteStr string) {
	writer := &quicStreamWriter{stream: stream}
	sessions := make(map[uint32]*quicStreamServerUDPConn)
	connLog := ctx.udp.log

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
			// Admission gate: on denial no session is created, and the stream is
			// torn down rather than left to re-ask on every new session id —
			// a connection that never sends metadata would otherwise pay the
			// admission wait again per frame.
			handler, admitted := s.admitUDP(ctx)
			if !admitted {
				connLog.Warn("QUICServer: UDP stream rejected (enforce: unauthenticated)",
					"sid", frame.SessionID, "addr", frame.Addr)
				for _, sess := range sessions {
					sess.close()
				}
				stream.Close()
				return
			}
			sessLog := connLog.With("sid", frame.SessionID, "addr", frame.Addr)
			sess = &quicStreamServerUDPConn{
				writer:     writer,
				sessionID:  frame.SessionID,
				addr:       frame.Addr,
				remoteAddr: remoteStr,
				log:        sessLog,
				readCh:     make(chan *UDPFrame, 32),
				done:       make(chan struct{}),
				dev:        ctx.dev,
			}
			sessions[frame.SessionID] = sess

			if handler != nil {
				addr := frame.Addr
				go func() {
					defer func() {
						if r := recover(); r != nil {
							sessLog.Error("QUICServer: panic in HandleUDP (stream)", "panic", r)
						}
					}()
					handler.HandleUDP(sess, addr)
				}()
			}
		}

		sess.deliver(frame)
	}
}

func (s *QUICServer) handleDatagrams(conn *quic.Conn, ctx *connContext) {
	state := ctx.udp
	log := state.log
	log.Info("QUICServer: handleDatagrams started")
	var totalReceived int64

	for {
		data, err := conn.ReceiveDatagram(context.Background())
		if err != nil {
			log.Warn("DIAG: datagram-recv-exit", "totalReceived", totalReceived, "err", err)
			return
		}
		totalReceived++

		frame, err := ReadUDPFrame(bytes.NewReader(data))
		if err != nil {
			log.Warn("QUICServer: datagram parse failed", "len", len(data), "err", err)
			continue
		}
		log.Debug("QUICServer: datagram received", "sessionID", frame.SessionID, "addr", frame.Addr, "payloadLen", len(frame.Payload))

		s.deliverToSession(ctx, state, conn, frame)
	}
}

// deliverToSession looks up or creates a serverUDPConn in state.sessions, then delivers the payload.
// Used by both handleDatagrams and handleUDPOverflowStream for session sharing.
//
// This is the choke point for the two QUIC UDP paths that never reach
// handleStream: the datagram path (the PRIMARY UDP path — DialUDP prefers
// datagrams, stream-UDP is only fallback) and the client overflow stream. Both
// obtain their handler from admitUDP, which is what makes the enforce gate
// unforgettable here rather than a check to remember.
//
// admitUDP runs OUTSIDE state.mu: under enforce an unauthenticated connection
// blocks up to authWait, and holding the map lock would stall every other
// session's datagrams. It also keeps s.mu from being taken under state.mu.
// Permissive and already-authenticated connections return instantly.
func (s *QUICServer) deliverToSession(ctx *connContext, state *connUDPState, conn *quic.Conn, frame *UDPFrame) {
	state.mu.Lock()
	sess, ok := state.sessions[frame.SessionID]
	state.mu.Unlock()

	if !ok {
		handler, admitted := s.admitUDP(ctx)
		if !admitted {
			state.log.Warn("QUICServer: UDP session rejected (enforce: unauthenticated)", "sid", frame.SessionID, "addr", frame.Addr)
			return
		}

		state.mu.Lock()
		// Re-check under lock: a concurrent frame for the same session may have
		// created it while we were gating.
		sess, ok = state.sessions[frame.SessionID]
		if !ok {
			if len(state.sessions) >= maxServerUDPSessions {
				state.mu.Unlock()
				state.log.Warn("QUICServer: max UDP sessions reached, dropping")
				return
			}
			sessLog := state.log.With("sid", frame.SessionID, "addr", frame.Addr)
			sess = &serverUDPConn{
				qconn:        conn,
				sessionID:    frame.SessionID,
				addr:         frame.Addr,
				remoteAddr:   conn.RemoteAddr().String(),
				readCh:       make(chan *UDPFrame, 32),
				done:         make(chan struct{}),
				overflow:     state.overflow,
				sendInflight: &state.sendInflight,
				created:      time.Now(),
				log:          sessLog,
				dev:          state.dev,
			}
			state.sessions[frame.SessionID] = sess

			sessLog.Info("QUICServer: new UDP session", "handlerSet", handler != nil)
			if handler != nil {
				addr := frame.Addr
				go state.runUDPHandler(handler, sess, addr)
			}
		}
		state.mu.Unlock()
	}

	sess.deliver(frame)
}

// handleUDPOverflowStream reads SmuxUDPFrame from a client-initiated overflow stream
// and delivers them to the shared session map (same sessions as datagram path).
func (s *QUICServer) handleUDPOverflowStream(br *bufio.Reader, ctx *connContext) {
	state := ctx.udp
	state.log.Info("QUICServer: receiving overflow stream from client")
	// We need the qconn to create new sessions — get it from the overflow struct.
	conn := state.overflow.qconn
	for {
		frame, err := ReadSmuxUDPFrame(br)
		if err != nil {
			return
		}
		s.deliverToSession(ctx, state, conn, frame)
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

// Close stops the server, tears down the revalidation sweeper (if enabled),
// and closes the listener. sessions.Close() calls wg.Wait() internally, which
// must never run under s.mu (Rule #3 — waiting under lock risks deadlock if
// the goroutine being waited on ever needs that same lock), so it runs after
// s.mu is released.
func (s *QUICServer) Close() error {
	s.mu.Lock()
	select {
	case <-s.done:
		s.mu.Unlock()
		return nil
	default:
		close(s.done)
	}
	ln := s.ln
	s.mu.Unlock()
	if s.sessions != nil {
		s.sessions.Close() // wg.Wait — no lock held (Rule #3)
	}
	if ln != nil {
		return ln.Close()
	}
	return nil
}

// ============================================================================
// Client-side UDP datagram demuxer
// ============================================================================

type quicUDPMux struct {
	conn        *quic.Conn
	mu          deadlock.Mutex
	sessions    map[uint32]chan *UDPFrame
	sessionDone map[uint32]chan struct{} // closed by reaper to notify quicUDPConn
	lastActive  map[uint32]*atomic.Int64 // unix seconds per session
	done        chan struct{}
	started     bool
	streamMux   *quicStreamUDPMux  // stream-based UDP fallback
	overflow    *udpOverflowStream // per-packet overflow for oversized datagrams

	// Datagram health tracking: consecutive ReadFrom timeouts indicate
	// a broken QUIC datagram path (e.g. ISP QoS dropping UDP).
	relayTimeouts atomic.Int32

	// nextRelayTrial is when (relayNow) an unhealthy relay admits its next
	// trial session — see admitSession.
	nextRelayTrial atomic.Int64
	now            func() time.Duration // nil outside tests: relayNow

	sendInflight atomic.Int32 // concurrent SendDatagram calls in progress

	// Connection-level UDP drop counters. Per-session counters only surface
	// in a close-time log line, which is useless while a game is stuttering;
	// these feed WireStats so the heartbeat can show loss in real time.
	//
	// Cumulative since the connection was established, not a per-interval
	// delta — unlike relayTimeouts above, nothing ever resets these to 0.
	// To read "is it dropping right now", diff two heartbeat samples rather
	// than reading the raw value.
	udpTxDrops atomic.Int64 // uplink datagrams dropped (send budget saturated)
	udpRxDrops atomic.Int64 // downlink datagrams dropped (session channel full)

	// Kept separate from udpRxDrops on purpose. A frame for an unknown
	// session is dominated by the benign tail: the peer keeps sending for a
	// moment after the client closed or the reaper evicted the session, and
	// every UDP session a client ever opens can contribute a few. Folding
	// that into udpRxDrops would make the canary's loss signal scale with
	// session churn instead of with actual loss. A live session losing
	// packets (udpRxDrops) and a dead session receiving them are different
	// diagnoses, so they get different counters.
	udpRxDropsNoSession atomic.Int64 // downlink datagrams for an unknown session
}

const relayTimeoutThreshold int32 = 3

// relayTrialInterval is how often an unhealthy datagram relay lets one new
// session through anyway (admitSession).
const relayTrialInterval = 30 * time.Second

// relayEpoch anchors relayNow, a monotonic clock: a wall-clock step (NTP on a
// router without an RTC) must not move a trial.
var relayEpoch = time.Now()

func (m *quicUDPMux) relayNow() int64 {
	if m.now != nil {
		return int64(m.now())
	}
	return int64(time.Since(relayEpoch))
}

func (m *quicUDPMux) recordRecv() {
	if m.relayTimeouts.Swap(0) >= relayTimeoutThreshold {
		slog.Info("quicUDPMux: datagram relay path healthy again")
	}
}

func (m *quicUDPMux) recordTimeout() {
	count := m.relayTimeouts.Add(1)
	if count == relayTimeoutThreshold {
		// Only on the transition: later timeouts (a burst of shed sends, the
		// trial session's own) must not keep pushing the trial away.
		m.nextRelayTrial.Store(m.relayNow() + int64(relayTrialInterval))
		slog.Warn("quicUDPMux: datagram relay path marked unhealthy",
			"relayTimeouts", count)
	}
}

func (m *quicUDPMux) datagramRelayHealthy() bool {
	return m.relayTimeouts.Load() < relayTimeoutThreshold
}

// admitSession reports whether a new session may use the datagram relay:
// always while it is healthy, and once per relayTrialInterval while it is not
// (trial = true).
//
// The timeout streak is only cleared by a datagram arriving (recordRecv).
// With every new session refused, that stops being possible once the last
// datagram session has ended, and the relay stayed unhealthy until the QUIC
// connection was replaced — on a router, for as long as it ran. The trial
// session is the probe: its reply clears the streak like any other datagram.
// If the path really is broken, or the trial's destination never answers,
// that one flow goes unanswered until its idle deadline (60 s) and the next
// trial is an interval later — so healing depends on a trial landing on a
// destination that answers, and is not instant when most new flows are to
// silent peers.
//
// A dial racing the moment the relay is marked unhealthy can read the previous
// schedule and be admitted at once; recordTimeout's Store then sets the real
// one. That costs one early trial, nothing else.
func (m *quicUDPMux) admitSession() (admitted, trial bool) {
	if m.datagramRelayHealthy() {
		return true, false
	}
	now, next := m.relayNow(), m.nextRelayTrial.Load()
	if now < next || !m.nextRelayTrial.CompareAndSwap(next, now+int64(relayTrialInterval)) {
		return false, false
	}
	return true, true
}

func newQuicUDPMux(conn *quic.Conn) *quicUDPMux {
	return &quicUDPMux{
		conn:        conn,
		sessions:    make(map[uint32]chan *UDPFrame),
		sessionDone: make(map[uint32]chan struct{}),
		lastActive:  make(map[uint32]*atomic.Int64),
		done:        make(chan struct{}),
		overflow:    &udpOverflowStream{qconn: conn},
	}
}

// udpSessionIdleTimeout is the max idle duration before a UDP session is reaped.
const udpSessionIdleTimeout = 60 * time.Second

func (m *quicUDPMux) start() {
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return
	}
	m.started = true
	m.mu.Unlock()
	go m.readLoop()
	go m.reapIdleSessions()
}

// reapIdleSessions periodically removes idle UDP sessions from the mux maps.
// Removed sessions stop receiving new datagrams. The corresponding
// quicUDPConn.ReadFrom will hit its read deadline or pipePacket idle timeout
// and close naturally — no channel close needed (which would cause nil panic).
func (m *quicUDPMux) reapIdleSessions() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.reapOnce()
		case <-m.done:
			return
		}
	}
}

func (m *quicUDPMux) reapOnce() {
	now := time.Now().Unix()
	m.mu.Lock()
	for id, ts := range m.lastActive {
		if now-ts.Load() > int64(udpSessionIdleTimeout/time.Second) {
			delete(m.sessions, id)
			delete(m.lastActive, id)
			reaped := m.sessionDone[id]
			delete(m.sessionDone, id)
			if reaped != nil {
				close(reaped)
			}
			slog.Debug("quicUDPMux: reaped idle session", "sessionID", id)
		}
	}
	m.mu.Unlock()
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
	var totalReceived int64
	for {
		data, err := m.conn.ReceiveDatagram(context.Background())
		if err != nil {
			slog.Warn("DIAG: datagram-readloop-exit", "totalReceived", totalReceived, "err", err)
			return
		}
		totalReceived++
		m.recordRecv()
		slog.Debug("quicUDPMux: received datagram", "len", len(data))
		frame, err := ReadUDPFrame(bytes.NewReader(data))
		if err != nil {
			slog.Warn("quicUDPMux: datagram parse failed", "len", len(data), "err", err)
			continue
		}
		m.dispatchFrame(frame)
	}
}

// dispatchFrame delivers a decoded frame to its session's channel, dropping
// it if the session is unknown (counted as udpRxDropsNoSession) or its
// channel is full (counted as udpRxDrops). Split out of readLoop so the drop
// paths are unit-testable without a live QUIC connection.
func (m *quicUDPMux) dispatchFrame(frame *UDPFrame) {
	m.mu.Lock()
	ch, ok := m.sessions[frame.SessionID]
	m.mu.Unlock()
	if !ok {
		m.udpRxDropsNoSession.Add(1)
		slog.Debug("quicUDPMux: no session for datagram", "sessionID", frame.SessionID, "addr", frame.Addr, "payloadLen", len(frame.Payload))
		return
	}
	m.touchSession(frame.SessionID)
	select {
	case ch <- frame:
	default:
		m.udpRxDrops.Add(1)
		slog.Warn("quicUDPMux: channel full, dropping datagram", "sessionID", frame.SessionID, "addr", frame.Addr, "payloadLen", len(frame.Payload))
	}
}

func (m *quicUDPMux) register(sessionID uint32) (chan *UDPFrame, chan struct{}) {
	ch := make(chan *UDPFrame, 32)
	reaped := make(chan struct{})
	ts := &atomic.Int64{}
	ts.Store(time.Now().Unix())
	m.mu.Lock()
	m.sessions[sessionID] = ch
	m.sessionDone[sessionID] = reaped
	m.lastActive[sessionID] = ts
	m.mu.Unlock()
	m.start()
	return ch, reaped
}

func (m *quicUDPMux) unregister(sessionID uint32) {
	m.mu.Lock()
	delete(m.sessions, sessionID)
	delete(m.sessionDone, sessionID)
	delete(m.lastActive, sessionID)
	m.mu.Unlock()
}

// touchSession updates the lastActive timestamp for a session (lock-free).
func (m *quicUDPMux) touchSession(sessionID uint32) {
	m.mu.Lock()
	ts := m.lastActive[sessionID]
	m.mu.Unlock()
	if ts != nil {
		ts.Store(time.Now().Unix())
	}
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
	qconn      datagramSender
	sessionID  uint32
	targetAddr string
	mux        *quicUDPMux
	readCh     chan *UDPFrame
	reaped     chan struct{} // closed by mux reaper when session is idle-reaped
	done       chan struct{}
	closeOnce  sync.Once
	rd         readDeadline
	created    time.Time
	txPackets  atomic.Int64
	txBytes    atomic.Int64
	txDrops    atomic.Int64 // datagrams silently dropped (queue saturated)
	rxPackets  atomic.Int64
	rxBytes    atomic.Int64
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
		c.rxPackets.Add(1)
		c.rxBytes.Add(int64(n))
		// Frame addr carries the real source when the peer is a full-cone
		// server; empty means an older server that only ever replies from
		// the session target.
		src := frame.Addr
		if src == "" {
			src = c.targetAddr
		}
		slog.Debug("quicUDPConn: ReadFrom ok", "sessionID", c.sessionID, "src", src, "len", n)
		return n, rawAddr(src), nil
	case <-c.reaped:
		return 0, nil, net.ErrClosed
	case <-c.done:
		return 0, nil, net.ErrClosed
	case <-c.mux.done:
		return 0, nil, net.ErrClosed
	case <-timerCh:
		// Every relayed UDP session ends here — UDP has no close, the idle
		// deadline is its end of life. Only a session that sent and never
		// heard back says anything about the datagram path; one that was
		// answered and then went quiet ended normally.
		if c.txPackets.Load() > 0 && c.rxPackets.Load() == 0 {
			c.mux.recordTimeout()
			slog.Warn("quicUDPConn: ReadFrom timeout", "sessionID", c.sessionID, "target", c.targetAddr)
		} else {
			slog.Debug("quicUDPConn: idle", "sessionID", c.sessionID, "target", c.targetAddr,
				"txPackets", c.txPackets.Load(), "rxPackets", c.rxPackets.Load())
		}
		return 0, nil, os.ErrDeadlineExceeded
	}
}

func (c *quicUDPConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	dest := c.targetAddr
	if addr != nil {
		dest = addr.String()
	}
	frame, put := MarshalUDPFrame(c.sessionID, dest, b)

	// Atomic inflight check: drop immediately when queue is saturated.
	// Returns (len, nil) — silent drop is protocol-correct for unreliable
	// DATAGRAM. Returning an error would propagate to the QUIC transport
	// and potentially close the connection to the target.
	// recordTimeout() is kept for relay health monitoring — enough drops
	// will mark the relay path unhealthy and trigger TCP-WS fallback.
	if c.mux.sendInflight.Add(1) > maxDatagramInflight {
		c.mux.sendInflight.Add(-1)
		put()
		c.txDrops.Add(1)
		c.mux.udpTxDrops.Add(1)
		c.mux.recordTimeout()
		return len(b), nil
	}
	err := c.qconn.SendDatagram(frame)
	c.mux.sendInflight.Add(-1)
	put()

	if err == nil {
		c.txPackets.Add(1)
		c.txBytes.Add(int64(len(b)))
		return len(b), nil
	}

	// Per-packet overflow: if datagram too large, send via overflow stream
	var tooLarge *quic.DatagramTooLargeError
	if errors.As(err, &tooLarge) && c.mux.overflow != nil {
		if err2 := c.mux.overflow.send(c.sessionID, dest, b); err2 != nil {
			return 0, err2
		}
		c.txPackets.Add(1)
		c.txBytes.Add(int64(len(b)))
		return len(b), nil
	}

	// Other errors: track for health monitoring
	slog.Warn("quicUDPConn: SendDatagram failed", "sessionID", c.sessionID,
		"target", dest, "payloadLen", len(b), "err", err)
	c.mux.recordTimeout()
	return 0, err
}

func (c *quicUDPConn) Close() error {
	c.closeOnce.Do(func() {
		if !c.created.IsZero() {
			slog.Info("quicUDPConn: closed",
				"sessionID", c.sessionID,
				"target", c.targetAddr,
				"duration", time.Since(c.created).Round(time.Millisecond),
				"txPackets", c.txPackets.Load(),
				"txBytes", c.txBytes.Load(),
				"txDrops", c.txDrops.Load(),
				"rxPackets", c.rxPackets.Load(),
				"rxBytes", c.rxBytes.Load())
		}
		close(c.done)
		c.mux.unregister(c.sessionID)
	})
	return nil
}

func (c *quicUDPConn) LocalAddr() net.Addr               { return c.qconn.LocalAddr() }
func (c *quicUDPConn) SetDeadline(t time.Time) error     { c.rd.set(t); return nil }
func (c *quicUDPConn) SetReadDeadline(t time.Time) error { c.rd.set(t); return nil }
func (c *quicUDPConn) SetWriteDeadline(time.Time) error  { return nil }

// ============================================================================
// Server-side PacketConn for UDP sessions
// ============================================================================

type serverUDPConn struct {
	qconn        datagramSender
	sessionID    uint32
	addr         string
	remoteAddr   string       // client IP:port for diagnostic attribution
	log          *slog.Logger // scoped logger with remote, sid, addr
	readCh       chan *UDPFrame
	done         chan struct{}
	closeOnce    sync.Once
	rd           readDeadline
	overflow     *udpOverflowStream // per-packet overflow for oversized datagrams
	sendInflight *atomic.Int32      // shared across all sessions on this connection
	created      time.Time
	txPackets    atomic.Int64
	txBytes      atomic.Int64
	txErrors     atomic.Int64
	txDrops      atomic.Int64 // datagrams silently dropped (queue saturated)
	rxPackets    atomic.Int64
	dev          *deviceIDHolder
}

var _ net.PacketConn = (*serverUDPConn)(nil)
var _ UDPSessionInfo = (*serverUDPConn)(nil)

func (c *serverUDPConn) UDPSessionID() uint32  { return c.sessionID }
func (c *serverUDPConn) UDPRemoteAddr() string { return c.remoteAddr }
func (c *serverUDPConn) Logger() *slog.Logger  { return c.log }

// DeviceID implements ConnDeviceID. Returns "" if no holder is wired
// (defensive) or if the metadata stream hasn't landed yet.
func (c *serverUDPConn) DeviceID() string {
	if c.dev == nil {
		return ""
	}
	return c.dev.get()
}

// SessionID implements ConnSessionID. "" until the metadata verdict lands.
func (c *serverUDPConn) SessionID() string {
	if c.dev == nil {
		return ""
	}
	return c.dev.session()
}

func (c *serverUDPConn) deliver(frame *UDPFrame) {
	select {
	case c.readCh <- frame:
	case <-c.done:
	default:
		// Drop: handler exited or too slow. Protocol-correct for unreliable datagrams.
		// Without default, a full readCh with open done channel blocks handleDatagrams
		// forever — freezing ALL datagram processing on this connection.
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
	case frame := <-c.readCh:
		n := copy(b, frame.Payload)
		c.rxPackets.Add(1)
		// Per-packet destination: a client may address several peers from one
		// source port. Empty addr means "same as the session's first target".
		dst := frame.Addr
		if dst == "" {
			dst = c.addr
		}
		return n, rawAddr(dst), nil
	case <-c.done:
		return 0, nil, net.ErrClosed
	case <-timerCh:
		return 0, nil, os.ErrDeadlineExceeded
	}
}

func (c *serverUDPConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	// Carry the real source back so the client can demultiplex replies from
	// multiple peers on one session.
	var src string
	if addr != nil {
		src = addr.String()
	}
	frame, put := MarshalUDPFrame(c.sessionID, src, b)

	// Atomic inflight check: when datagram queue is saturated, overflow to
	// the reliable stream path instead of blocking in datagramQueue.Add().
	// Without this, all HandleUDP goroutines block in Add() and can't call
	// target.Read() — starving every UDP session on this connection.
	if c.sendInflight.Add(1) > maxDatagramInflight {
		c.sendInflight.Add(-1)
		put()
		// Overflow to stream: preserves data via reliable STREAM frames.
		// Critical for QUIC handshake completion packets that MUST arrive.
		if c.overflow != nil {
			if err := c.overflow.send(c.sessionID, src, b); err == nil {
				c.txPackets.Add(1)
				c.txBytes.Add(int64(len(b)))
				return len(b), nil
			}
		}
		// Stream overflow also failed — silent drop (last resort).
		c.txDrops.Add(1)
		return len(b), nil
	}
	err := c.qconn.SendDatagram(frame)
	c.sendInflight.Add(-1)
	put()

	if err == nil {
		c.txPackets.Add(1)
		c.txBytes.Add(int64(len(b)))
		return len(b), nil
	}

	// Per-packet overflow: if datagram too large, send via overflow stream
	var tooLarge *quic.DatagramTooLargeError
	if errors.As(err, &tooLarge) && c.overflow != nil {
		if err2 := c.overflow.send(c.sessionID, src, b); err2 != nil {
			c.txErrors.Add(1)
			return 0, err2
		}
		c.txPackets.Add(1)
		c.txBytes.Add(int64(len(b)))
		return len(b), nil
	}

	c.txErrors.Add(1)
	c.log.Warn("serverUDPConn: SendDatagram failed", "err", err)
	return 0, err
}

func (c *serverUDPConn) close() {
	c.closeOnce.Do(func() {
		if !c.created.IsZero() {
			c.log.Info("serverUDPConn: closed",
				"duration", time.Since(c.created).Round(time.Millisecond),
				"rxPackets", c.rxPackets.Load(),
				"txPackets", c.txPackets.Load(),
				"txBytes", c.txBytes.Load(),
				"txErrors", c.txErrors.Load(),
				"txDrops", c.txDrops.Load())
		}
		close(c.done)
	})
}

func (c *serverUDPConn) Close() error {
	c.close()
	return nil
}

func (c *serverUDPConn) LocalAddr() net.Addr               { return c.qconn.LocalAddr() }
func (c *serverUDPConn) SetDeadline(t time.Time) error     { c.rd.set(t); return nil }
func (c *serverUDPConn) SetReadDeadline(t time.Time) error { c.rd.set(t); return nil }
func (c *serverUDPConn) SetWriteDeadline(time.Time) error  { return nil }

// ============================================================================
// Server-side QUIC stream writer (shared, mutex-protected)
// ============================================================================
//
// Same stream-level isolation design as serverSmuxWriter (tcpws.go):
// sync.Mutex for the serialization lock + per-Write deadline + active kill
// on failure. See that file for the rationale.

type quicStreamWriter struct {
	stream    *quic.Stream
	writeMu   sync.Mutex
	closeOnce sync.Once
	dead      atomic.Bool
}

func (w *quicStreamWriter) writeFrame(sessionID uint32, addr string, payload []byte) error {
	if w.dead.Load() {
		return errStreamDead
	}

	w.writeMu.Lock()
	if w.dead.Load() {
		w.writeMu.Unlock()
		return errStreamDead
	}

	_ = w.stream.SetWriteDeadline(time.Now().Add(streamWriteDeadline))
	start := time.Now()
	err := WriteSmuxUDPFrame(w.stream, sessionID, addr, payload)
	writeMs := time.Since(start).Milliseconds()
	if err != nil {
		w.dead.Store(true)
	}
	w.writeMu.Unlock()

	if err != nil {
		slog.Warn("DIAG: stream-killed-on-write-fail",
			"transport", "quic-stream", "sessionID", sessionID,
			"len", len(payload), "writeMs", writeMs, "err", err)
		w.closeStreamOnce()
		return err
	}
	if writeMs > 100 {
		slog.Warn("DIAG: stream-writer-slow",
			"transport", "quic-stream", "sessionID", sessionID,
			"len", len(payload), "writeMs", writeMs)
	}
	return nil
}

func (w *quicStreamWriter) markDead() {
	w.dead.Store(true)
	w.closeStreamOnce()
}

func (w *quicStreamWriter) closeStreamOnce() {
	w.closeOnce.Do(func() {
		w.stream.CancelRead(0)
		_ = w.stream.Close()
	})
}

// ============================================================================
// Server-side PacketConn for QUIC stream UDP sessions
// ============================================================================

type quicStreamServerUDPConn struct {
	writer     *quicStreamWriter
	sessionID  uint32
	addr       string
	remoteAddr string
	log        *slog.Logger // scoped logger with remote, sid, addr
	readCh     chan *UDPFrame
	done       chan struct{}
	closeOnce  sync.Once
	rd         readDeadline
	dev        *deviceIDHolder
}

var _ net.PacketConn = (*quicStreamServerUDPConn)(nil)
var _ UDPSessionInfo = (*quicStreamServerUDPConn)(nil)

func (c *quicStreamServerUDPConn) UDPSessionID() uint32  { return c.sessionID }
func (c *quicStreamServerUDPConn) UDPRemoteAddr() string { return c.remoteAddr }
func (c *quicStreamServerUDPConn) Logger() *slog.Logger  { return c.log }

// DeviceID implements ConnDeviceID. Returns "" if no holder is wired
// (defensive) or if the metadata stream hasn't landed yet.
func (c *quicStreamServerUDPConn) DeviceID() string {
	if c.dev == nil {
		return ""
	}
	return c.dev.get()
}

// SessionID implements ConnSessionID. "" until the metadata verdict lands.
func (c *quicStreamServerUDPConn) SessionID() string {
	if c.dev == nil {
		return ""
	}
	return c.dev.session()
}

func (c *quicStreamServerUDPConn) deliver(frame *UDPFrame) {
	select {
	case c.readCh <- frame:
	case <-c.done:
	default:
		// Drop: handler exited or too slow. Protocol-correct for unreliable UDP.
		// Without default, a full readCh blocks handleUDPStream main loop forever
		// — freezing flow control and triggering client-side writeMu deadlock
		// FATAL. Mirrors serverUDPConn.deliver datagram-path protection.
		slog.Debug("DIAG: udp-deliver-drop", "transport", "quic-stream",
			"sessionID", c.sessionID, "addr", c.addr, "len", len(frame.Payload))
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
	case frame := <-c.readCh:
		n := copy(b, frame.Payload)
		dst := frame.Addr
		if dst == "" {
			dst = c.addr
		}
		return n, rawAddr(dst), nil
	case <-c.done:
		return 0, nil, net.ErrClosed
	case <-timerCh:
		return 0, nil, os.ErrDeadlineExceeded
	}
}

func (c *quicStreamServerUDPConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	// Carry the real source back so the client can demultiplex replies.
	var src string
	if addr != nil {
		src = addr.String()
	}
	if err := c.writer.writeFrame(c.sessionID, src, b); err != nil {
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

func (c *quicStreamServerUDPConn) LocalAddr() net.Addr               { return &net.UDPAddr{} }
func (c *quicStreamServerUDPConn) SetDeadline(t time.Time) error     { c.rd.set(t); return nil }
func (c *quicStreamServerUDPConn) SetReadDeadline(t time.Time) error { c.rd.set(t); return nil }
func (c *quicStreamServerUDPConn) SetWriteDeadline(time.Time) error  { return nil }

// ============================================================================
// Client-side stream-based UDP mux (fallback when datagrams not supported)
// ============================================================================

// quicStreamUDPMux is the client-side multiplexer over a single quic.Stream
// used for UDP relay when QUIC datagrams are unavailable. Same lock-isolation
// design as smuxUDPMux (TCP-WS path); see that type's doc for full rationale.
//
// writeMu is sync.Mutex (not deadlock.Mutex) because Stream.Write may
// legitimately block under flow-control. Each Write has a 5s deadline;
// failure marks the mux dead and closes the stream once via streamOnce.
type quicStreamUDPMux struct {
	stream      *quic.Stream
	qconn       *quic.Conn
	br          *bufio.Reader
	writeMu     sync.Mutex // serialization, may hold across I/O
	streamOnce  sync.Once  // guards stream.Close
	dead        atomic.Bool
	deadReason  atomic.Value // string; written once, by whoever first marks dead
	doneOnce    sync.Once    // guards close(done) — replaces ad-hoc select pattern
	mu          deadlock.Mutex
	sessions    map[uint32]chan *UDPFrame
	sessionDone map[uint32]chan struct{} // closed by reaper to notify quicStreamUDPConn
	lastActive  map[uint32]*atomic.Int64
	done        chan struct{}
	started     bool

	// Downlink drop counters, same split and same semantics as quicUDPMux's.
	// There is deliberately no udpTxDrops here: this mux never silently
	// discards an uplink packet — writeFrame either writes the frame or
	// returns an error that tears the session down. See WireStats.
	udpRxDrops          atomic.Int64 // downlink frames dropped (session channel full)
	udpRxDropsNoSession atomic.Int64 // downlink frames for an unknown session
}

func newQuicStreamUDPMux(stream *quic.Stream, qconn *quic.Conn) *quicStreamUDPMux {
	return &quicStreamUDPMux{
		stream:      stream,
		qconn:       qconn,
		br:          bufio.NewReader(stream),
		sessions:    make(map[uint32]chan *UDPFrame),
		sessionDone: make(map[uint32]chan struct{}),
		lastActive:  make(map[uint32]*atomic.Int64),
		done:        make(chan struct{}),
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
	go m.reapIdleSessions()
}

func (m *quicStreamUDPMux) closeDone() {
	m.doneOnce.Do(func() { close(m.done) })
}

func (m *quicStreamUDPMux) readLoop() {
	defer m.closeDone()
	for {
		frame, err := ReadSmuxUDPFrame(m.br)
		if err != nil {
			// Relay stream gone — mark dead so the next dialUDPStream
			// rebuilds rather than reusing it. Mirror of smuxUDPMux.
			m.markDeadOnce(deathReasonPeerClosed)
			slog.Warn("quicStreamUDPMux: readLoop exiting", "err", err)
			return
		}
		m.dispatchFrame(frame)
	}
}

// dispatchFrame delivers a decoded frame to its session's channel, dropping
// it if the session is unknown (counted as udpRxDropsNoSession) or its
// channel is full (counted as udpRxDrops). Split out of readLoop so the drop
// paths are unit-testable without a live QUIC stream.
func (m *quicStreamUDPMux) dispatchFrame(frame *UDPFrame) {
	m.mu.Lock()
	ch, ok := m.sessions[frame.SessionID]
	m.mu.Unlock()
	if !ok {
		m.udpRxDropsNoSession.Add(1)
		slog.Warn("DIAG: stream-mux-no-session", "sessionID", frame.SessionID, "len", len(frame.Payload))
		return
	}
	m.touchSession(frame.SessionID)
	select {
	case ch <- frame:
		slog.Debug("DIAG: stream-mux-recv", "sessionID", frame.SessionID, "len", len(frame.Payload))
	default:
		m.udpRxDrops.Add(1)
		slog.Warn("DIAG: stream-mux-drop", "sessionID", frame.SessionID, "len", len(frame.Payload), "chanLen", len(ch))
	}
}

func (m *quicStreamUDPMux) writeFrame(sessionID uint32, addr string, payload []byte) error {
	if m.dead.Load() {
		return m.deadErr()
	}

	m.writeMu.Lock()
	if m.dead.Load() {
		m.writeMu.Unlock()
		return m.deadErr()
	}

	_ = m.stream.SetWriteDeadline(time.Now().Add(streamWriteDeadline))
	start := time.Now()
	err := WriteSmuxUDPFrame(m.stream, sessionID, addr, payload)
	if err != nil {
		m.markDeadOnce(deathReasonWriteFail)
	}
	m.writeMu.Unlock()

	if err != nil {
		slog.Warn("DIAG: stream-killed-on-write-fail",
			"transport", "quic-stream", "side", "client", "sessionID", sessionID,
			"len", len(payload), "writeMs", time.Since(start).Milliseconds(), "err", err)
		m.closeStreamOnce()
	}
	return err
}

// markDeadOnce sets the dead flag and records why. First caller wins, so the
// recorded reason is what actually killed the mux. Mirror of smuxUDPMux.
func (m *quicStreamUDPMux) markDeadOnce(reason string) {
	if m.dead.CompareAndSwap(false, true) {
		m.deadReason.Store(reason)
	}
}

// deathReason reports why the mux died, or "" while it is still alive.
func (m *quicStreamUDPMux) deathReason() string {
	s, _ := m.deadReason.Load().(string)
	if s == "" && m.dead.Load() {
		// Marked dead but the reason Store has not landed yet (nanosecond
		// window inside markDeadOnce). Never report a blank cause.
		return "unknown"
	}
	return s
}

// deadErr reports the death to a writer that arrived after the fact, naming
// the cause. Wraps errStreamDead so errors.Is keeps matching.
func (m *quicStreamUDPMux) deadErr() error {
	if r := m.deathReason(); r != "" {
		return fmt.Errorf("%w (%s)", errStreamDead, r)
	}
	return errStreamDead
}

func (m *quicStreamUDPMux) markDead() {
	m.markDeadOnce(deathReasonClosed)
	m.closeStreamOnce()
}

func (m *quicStreamUDPMux) closeStreamOnce() {
	m.streamOnce.Do(func() {
		if m.stream != nil {
			_ = m.stream.Close()
		}
	})
}

func (m *quicStreamUDPMux) register(sessionID uint32) (chan *UDPFrame, chan struct{}) {
	ch := make(chan *UDPFrame, 32)
	reaped := make(chan struct{})
	ts := &atomic.Int64{}
	ts.Store(time.Now().Unix())
	m.mu.Lock()
	m.sessions[sessionID] = ch
	m.sessionDone[sessionID] = reaped
	m.lastActive[sessionID] = ts
	m.mu.Unlock()
	m.start()
	return ch, reaped
}

func (m *quicStreamUDPMux) unregister(sessionID uint32) {
	m.mu.Lock()
	delete(m.sessions, sessionID)
	delete(m.sessionDone, sessionID)
	delete(m.lastActive, sessionID)
	m.mu.Unlock()
}

func (m *quicStreamUDPMux) touchSession(sessionID uint32) {
	m.mu.Lock()
	ts := m.lastActive[sessionID]
	m.mu.Unlock()
	if ts != nil {
		ts.Store(time.Now().Unix())
	}
}

func (m *quicStreamUDPMux) reapIdleSessions() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.reapOnce()
		case <-m.done:
			return
		}
	}
}

func (m *quicStreamUDPMux) reapOnce() {
	now := time.Now().Unix()
	m.mu.Lock()
	for id, ts := range m.lastActive {
		if now-ts.Load() > int64(udpSessionIdleTimeout/time.Second) {
			delete(m.sessions, id)
			delete(m.lastActive, id)
			reaped := m.sessionDone[id]
			delete(m.sessionDone, id)
			if reaped != nil {
				close(reaped)
			}
			slog.Debug("quicStreamUDPMux: reaped idle session", "sessionID", id)
		}
	}
	m.mu.Unlock()
}

func (m *quicStreamUDPMux) close() {
	m.markDead()
	m.closeDone()
}

// ============================================================================
// Client-side PacketConn over QUIC stream (fallback)
// ============================================================================

type quicStreamUDPConn struct {
	mux        *quicStreamUDPMux
	sessionID  uint32
	targetAddr string
	readCh     chan *UDPFrame
	reaped     chan struct{} // closed by mux reaper when session is idle-reaped
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
		src := frame.Addr
		if src == "" {
			src = c.targetAddr
		}
		return n, rawAddr(src), nil
	case <-c.reaped:
		return 0, nil, net.ErrClosed
	case <-c.done:
		return 0, nil, net.ErrClosed
	case <-c.mux.done:
		return 0, nil, net.ErrClosed
	case <-timerCh:
		return 0, nil, os.ErrDeadlineExceeded
	}
}

func (c *quicStreamUDPConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	dest := c.targetAddr
	if addr != nil {
		dest = addr.String()
	}
	if err := c.mux.writeFrame(c.sessionID, dest, b); err != nil {
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

func (c *quicStreamUDPConn) LocalAddr() net.Addr               { return c.mux.qconn.LocalAddr() }
func (c *quicStreamUDPConn) SetDeadline(t time.Time) error     { c.rd.set(t); return nil }
func (c *quicStreamUDPConn) SetReadDeadline(t time.Time) error { c.rd.set(t); return nil }
func (c *quicStreamUDPConn) SetWriteDeadline(time.Time) error  { return nil }

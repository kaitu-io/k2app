package wire

import (
	"bufio"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	tls "github.com/metacubex/utls"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sasha-s/go-deadlock"
	"golang.org/x/sync/singleflight"

	"github.com/coder/websocket"
	smux "github.com/xtaci/smux/v2"
)

// Compile-time interface checks.
var (
	_ Dialer     = (*TCPWSClient)(nil)
	_ Listener   = (*TCPWSServer)(nil)
	_ EchoProber = (*TCPWSClient)(nil)
)

// DefaultSmuxConfig returns the client-side smux configuration for the k2v5
// TCP-WS transport; ServerSmuxConfig is the server's. They differ only in
// the receive budget (smux K2 patch 6), which is local to each side: the
// window a receiver grants travels in its own window updates, so the two
// ends need not agree and any mix of versions interoperates.
//
// The receive budget, per session (one TCP-WS connection per server):
//   - MaxReceiveBuffer caps the bytes buffered for all streams together.
//   - Each stream's window starts at MinStreamBuffer and doubles while its
//     reader keeps up, up to MaxStreamBuffer; that growth, summed over all
//     streams, is capped at MaxReceiveBuffer/2, and shrinks back once a
//     reader has been idle for a second.
//   - smux caps one stream's growth at half the pool; MaxStreamBuffer is
//     set at or below that (MinStreamBuffer + MaxReceiveBuffer/4), so the
//     configured number is the real one.
//
// A stream whose reader has stalled therefore holds about MinStreamBuffer,
// and it takes MaxReceiveBuffer/2 / MinStreamBuffer of them to block the
// session. Single-stream throughput tops out near MaxStreamBuffer/RTT. The
// old fixed config (64 KB windows, 256 KB session) blocked on 4 stalled
// streams and capped one stream at ~5 Mbps on a 100 ms path.
func DefaultSmuxConfig() *smux.Config {
	return smuxConfig(clientRecvBuffer, clientMinStreamWindow, clientMaxStreamWindow)
}

// ServerSmuxConfig returns the server-side smux configuration. The server
// receives uploads; 2 MB per session, 16 KB per stalled stream, up to 512 KB
// (~20 Mbps at 200 ms, ~75 Mbps at 55 ms) per stream. MaxReceiveBuffer is
// also what one client that ignores flow control can pin on the node, per
// TCP-WS connection (the pre-auto-tuning config: 256 KB), so it stays at
// serverMaxSessionBuffer.
func ServerSmuxConfig() *smux.Config {
	return smuxConfig(serverMaxSessionBuffer, 16<<10, 512<<10)
}

const serverMaxSessionBuffer = 2 << 20

func smuxConfig(recvBuf, minWindow, maxWindow int) *smux.Config {
	return &smux.Config{
		KeepAliveInterval: 10 * time.Second,
		KeepAliveTimeout:  30 * time.Second,
		MaxFrameSize:      32768, // 32 KB, matches WebSocket message limit
		MaxReceiveBuffer:  recvBuf,
		MaxStreamBuffer:   maxWindow,
		MinStreamBuffer:   minWindow,
	}
}

// smuxStreamConn wraps a smux.Stream with a buffered reader for post-header reads.
type smuxStreamConn struct {
	*smux.Stream
	br         *bufio.Reader
	remoteAddr net.Addr
	closeOnce  sync.Once
	dev        *deviceIDHolder
}

// RemoteAddr returns the overridden remote address if set, otherwise delegates to Stream.
func (c *smuxStreamConn) RemoteAddr() net.Addr {
	if c.remoteAddr != nil {
		return c.remoteAddr
	}
	return c.Stream.RemoteAddr()
}

// DeviceID implements ConnDeviceID. Returns "" if no holder is wired
// (defensive) or if the metadata stream hasn't landed yet.
func (c *smuxStreamConn) DeviceID() string {
	if c.dev == nil {
		return ""
	}
	return c.dev.get()
}

// SessionID implements ConnSessionID. "" until the metadata verdict lands.
func (c *smuxStreamConn) SessionID() string {
	if c.dev == nil {
		return ""
	}
	return c.dev.session()
}

// Read uses the buffered reader if present.
func (c *smuxStreamConn) Read(b []byte) (int, error) {
	if c.br != nil {
		return c.br.Read(b)
	}
	return c.Stream.Read(b)
}

// WriteTo shadows the embedded smux.Stream.WriteTo, which io.Copy prefers
// over Read and which would skip whatever br buffered past the stream
// header. bufio.Reader.WriteTo drains that buffer, then hands off to the
// stream's WriteTo.
func (c *smuxStreamConn) WriteTo(w io.Writer) (int64, error) {
	if c.br != nil {
		return c.br.WriteTo(w)
	}
	return c.Stream.WriteTo(w)
}

// CloseWrite closes only the write direction (sends FIN); the stream stays
// readable (third_party/smux K2 patch 2) — when the peer has said it
// half-closes, via the metadata "smux_hc" key (SetPeerHalfClose on each
// side's session). Toward any other peer it is a full Close, as it was
// before half-close existed.
// Used by pipe() for half-close; full cleanup is the caller's defer Close().
func (c *smuxStreamConn) CloseWrite() error {
	return c.Stream.CloseWrite()
}

// Close closes the stream. Idempotent via sync.Once — safe to call after CloseWrite().
func (c *smuxStreamConn) Close() error {
	c.closeOnce.Do(func() {
		c.Stream.Close()
	})
	return nil
}

// ============================================================================
// TCPWSClient — implements wire.Dialer
// ============================================================================

// TCPWSClient is the TCP-WS + smux client transport.
//
// Connection flow (production mode):
//  1. TCP dial to server:port
//  2. uTLS handshake with fingerprint (chrome/firefox/safari/random)
//  3. WebSocket upgrade to /k2v5/tunnel over TLS connection
//  4. smux session multiplexing over WebSocket binary frames
//
// The uTLS library allows fingerprint customization to evade DPI detection.
// ECH (Encrypted Client Hello) config is passed through to hide the real SNI.
//
// Test mode: SetTestConn bypasses TCP+TLS+WS and uses a direct net.Conn for smux.
type TCPWSClient struct {
	cfg          Config
	jar          *CookieJar // per-outbound cookie store; nil = no cookie support (probe/p2p)
	serverAddr   string
	tlsConfig    *tls.Config // override for testing
	testConn     net.Conn    // override for testing (bypasses TCP+TLS+WS)
	directDialer DirectDialer

	mu        deadlock.Mutex
	connectSF singleflight.Group // dedupes concurrent connect attempts without held lock
	initUDPMu deadlock.Mutex     // serializes UDP mux lazy init; ordering: initUDPMu → mu
	sess      *smux.Session
	udpMux    *smuxUDPMux
	alloc     *SessionIDAllocator
	closed    bool

	// udpMuxRebuilds counts dead-mux evictions, reported by the
	// udp-mux-rebuild DIAG event. A steadily climbing value means the relay
	// stream keeps dying and is being rebuilt per dial.
	udpMuxRebuilds atomic.Uint64
}

// WireStats returns a snapshot of TCP-WS transport metrics.
//
// TCP-WS has no congestion-control stats, so the CC fields stay zero. The UDP
// downlink drop counters are real, though: this used to return a bare
// WireStats{Transport: "tcpws"}, which meant a heartbeat taken while QUIC was
// falling back reported udpRxDrops=0 no matter how much the mux was
// discarding — a structural zero in exactly the state where a user is most
// likely to be stuttering. UDPTxDrops stays 0 by construction, not by
// omission: smux writes fail loudly instead of dropping (see WireStats).
func (c *TCPWSClient) WireStats() WireStats {
	c.mu.Lock()
	mux := c.udpMux
	c.mu.Unlock()

	s := WireStats{Transport: "tcpws"}
	if mux != nil {
		s.UDPRxDrops = mux.udpRxDrops.Load()
		s.UDPRxDropsNoSession = mux.udpRxDropsNoSession.Load()
	}
	return s
}

var (
	_ StatsProvider = (*TCPWSClient)(nil)
)

// NewTCPWSClient creates a new TCP-WS client transport. jar is the
// per-outbound CookieJar shared with the sibling QUIC client (nil disables
// cookies).
func NewTCPWSClient(cfg Config, jar *CookieJar) *TCPWSClient {
	dialHost := cfg.Host
	if cfg.DialAddr != "" {
		dialHost = cfg.DialAddr
	}
	return &TCPWSClient{
		cfg:        cfg,
		jar:        jar,
		serverAddr: net.JoinHostPort(dialHost, strconv.Itoa(cfg.Port)),
	}
}

// SetTestConn sets a test connection that bypasses TCP+TLS dial.
func (c *TCPWSClient) SetTestConn(conn net.Conn) {
	c.testConn = conn
}

// SetTLSConfig overrides the TLS config for testing (bypasses BuildClientTLSConfig).
func (c *TCPWSClient) SetTLSConfig(cfg *tls.Config) {
	c.tlsConfig = cfg
}

// SetDirectDialer sets a physical-interface-bound dialer for bypassing TUN.
func (c *TCPWSClient) SetDirectDialer(d DirectDialer) {
	c.directDialer = d
}

// connect establishes a smux session if not already connected.
//
// Concurrency model: multiple concurrent callers are de-duplicated via
// singleflight — only one actually runs the dial, the rest block on the
// shared result channel. This replaces the previous connectMu-based
// serialization, which held a mutex across the full dial I/O. Under pf
// blackhole + multiple in-flight dials, N queued callers each waited
// tcpwsConnectTimeout (10s) serially; the 4th+ waiter exceeded go-deadlock's
// 30s threshold and fired POTENTIAL DEADLOCK → FATAL (v0.4.3 production,
// Phase-A fallback on blocked tunnel IP). singleflight.Group's internal
// mutex is held only for the map lookup, never across I/O, so no waiter
// ever blocks for more than one dial's duration regardless of fan-in.
//
// Caveat (intentional): waiters cannot cancel via their own ctx — only the
// leader's ctx governs the in-flight dial. A waiter with a shorter deadline
// than the leader will still block until the leader's dial settles
// (bounded by tcpwsConnectTimeout), then receive the leader's result.
// This is acceptable because (a) TM/Router callers use comparable ctx
// deadlines, (b) channel waits are invisible to go-deadlock, so no FATAL.
func (c *TCPWSClient) connect(ctx context.Context) (*smux.Session, error) {
	// Fast path: return cached live session or fail fast on closed.
	if sess, closed := c.checkCachedSession(); closed {
		return nil, ErrNotConnected
	} else if sess != nil {
		return sess, nil
	}

	// Slow path: single-flight the actual dial. All concurrent callers
	// join one attempt; the callback runs at most once per collapsed batch.
	v, err, _ := c.connectSF.Do("dial", func() (any, error) {
		// Re-check: Close() may have been called while we were entering the
		// group, OR an earlier batch winner may have populated c.sess. Both
		// must short-circuit *before* the 10s blocking dial.
		if sess, closed := c.checkCachedSession(); closed {
			return nil, ErrNotConnected
		} else if sess != nil {
			return sess, nil
		}

		c.mu.Lock()
		testConn := c.testConn
		c.mu.Unlock()

		// --- All blocking I/O below — NO lock held ---
		var conn net.Conn
		if testConn != nil {
			conn = testConn
		} else {
			var dialErr error
			conn, dialErr = c.connectTLSWebSocket(ctx)
			if dialErr != nil {
				return nil, dialErr
			}
		}

		sess, smuxErr := smux.Client(conn, DefaultSmuxConfig())
		if smuxErr != nil {
			conn.Close()
			return nil, fmt.Errorf("wire: smux client: %w", smuxErr)
		}

		// Shared metadata orchestration (metadata_client.go). Best-effort for
		// transport errors; an explicit rejection fails the connect — TCP-WS
		// used to drop ok/errCode entirely, which is the divergence the shared
		// sendMetadata removes.
		mdResp, mdErr := sendMetadata(ctx, smuxMetaTransport{sess: sess}, MetadataFrame{
			UDID:    c.cfg.UDID,
			Token:   c.cfg.Token,
			Mode:    c.cfg.Mode,
			Version: c.cfg.Version,
			Cookie:  c.jar.Get(c.cfg.Pin), // nil-safe; "" = cold auth

			SmuxHalfClose: true,
		})
		if mdErr != nil {
			if isMetadataReject(mdErr) {
				sess.Close()
				conn.Close()
				// ProxyLayerError: the wire transport worked (stream opened, server
				// responded with an explicit rejection) — TCP-WS fallback to the
				// same server/credentials cannot help. Matches the established
				// convention at :482 for the proxy-stream rejection path.
				return nil, &ProxyLayerError{Err: fmt.Errorf("%w: %w", ErrMetadataRejected, mdErr)}
			}
			slog.Debug("TCPWSClient: metadata stream failed (best-effort)", "err", mdErr)
		}
		// newCookie is non-empty only right after a Center cold auth (spec
		// §6.3); jar.Put is a no-op on empty. Never log cookie/newCookie/
		// meta.Token (spec §6.2/§9).
		c.jar.Put(c.cfg.Pin, mdResp.Cookie)
		// Old servers don't advertise it, and a failed metadata stream leaves
		// it unknown: both keep upstream FIN semantics for this session.
		sess.SetPeerHalfClose(mdResp.SmuxHalfClose)

		// --- Store result under lock ---
		// concurrency:allow — closing locally-owned sess not yet stored in struct
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			sess.Close()
			return nil, ErrNotConnected
		}
		c.sess = sess
		c.alloc = NewSessionIDAllocator()
		c.mu.Unlock()
		return sess, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*smux.Session), nil
}

// checkCachedSession inspects the cached smux session under mu and evicts
// dead sessions (closing resources OUTSIDE the lock per Rule #4).
//
// Returns (sess, closed):
//   - (liveSess, false) → caller returns sess directly
//   - (nil, true)       → client is Close()d; caller returns ErrNotConnected
//   - (nil, false)      → no usable session; caller proceeds to dial
func (c *TCPWSClient) checkCachedSession() (*smux.Session, bool) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, true
	}
	if c.sess != nil {
		if !c.sess.IsClosed() {
			sess := c.sess
			c.mu.Unlock()
			return sess, false
		}
		slog.Debug("TCPWSClient: clearing dead smux session")
		mux, deadSess := c.extractSessionLocked()
		c.mu.Unlock()
		if mux != nil {
			mux.close()
		}
		if deadSess != nil {
			closeSmuxSessionSafe(deadSess)
		}
		return nil, false
	}
	c.mu.Unlock()
	return nil, false
}

// smuxCloseTimeout bounds smux.Session.Close. TLS connections in IO wait
// state may block Close indefinitely when the network is dead. 5s is 50x
// normal close time.
const smuxCloseTimeout = 5 * time.Second

// closeSmuxSessionSafe closes a smux session with a bounded timeout.
// If Close blocks (underlying TLS conn stuck in IO wait on dead network),
// the goroutine is orphaned and the function returns after timeout.
func closeSmuxSessionSafe(sess *smux.Session) {
	done := make(chan struct{})
	go func() {
		sess.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(smuxCloseTimeout):
		slog.Warn("wire: smux session close timed out, orphaning goroutine")
	}
}

// tcpwsConnectTimeout bounds the total TCP+TLS+WebSocket dial sequence.
// Without this, TCP SYN retransmit timeout (75-127s on macOS) can hold
// connectMu beyond go-deadlock's 30s threshold. Aligned with QUIC's
// reconnectBudget (8s), with extra margin for TLS+WS overhead.
const tcpwsConnectTimeout = 10 * time.Second

// connectTLSWebSocket establishes TCP → uTLS → WebSocket connection.
// Returns a net.Conn-compatible WebSocket connection ready for smux.
func (c *TCPWSClient) connectTLSWebSocket(ctx context.Context) (net.Conn, error) {
	connectCtx, cancel := context.WithTimeout(ctx, tcpwsConnectTimeout)
	defer cancel()

	connectStart := time.Now()
	slog.Debug("TCPWSClient: connectTLSWebSocket start", "serverAddr", c.serverAddr, "hasDirectDialer", c.directDialer != nil)
	// 1. TCP dial (use DirectDialer if set, to bypass TUN)
	// Both paths use Timeout in addition to context deadline for kernel-level
	// enforcement — context cancel alone doesn't reliably interrupt macOS TCP
	// SYN retransmit (can block 75s despite 10s context timeout).
	var conn net.Conn
	var err error
	if c.directDialer != nil {
		slog.Debug("TCPWSClient: using directDialer for TCP dial")
		conn, err = c.directDialer.DialContext(connectCtx, "tcp", c.serverAddr)
	} else {
		slog.Debug("TCPWSClient: using default dialer for TCP dial")
		d := net.Dialer{Timeout: tcpwsConnectTimeout}
		conn, err = d.DialContext(connectCtx, "tcp", c.serverAddr)
	}
	if err != nil {
		slog.Debug("TCPWSClient: TCP dial failed", "serverAddr", c.serverAddr, "err", err)
		return nil, fmt.Errorf("wire: TCP dial %s: %w", c.serverAddr, err)
	}
	slog.Debug("TCPWSClient: TCP dial success", "local", conn.LocalAddr(), "remote", conn.RemoteAddr())

	// 2. uTLS handshake
	var tlsCfg *tls.Config
	var clientHelloID tls.ClientHelloID

	if c.tlsConfig != nil {
		// Test mode with override TLS config
		tlsCfg = c.tlsConfig.Clone()
		clientHelloID = tls.HelloChrome_120_PQ // Default for tests
	} else {
		// Production mode: build from Config
		tlsCfg, clientHelloID, err = BuildClientTLSConfig(c.cfg)
		if err != nil {
			conn.Close()
			return nil, err
		}
	}

	// tlsCfg goes to uTLS as-is. It used to be copied field by field into a
	// fresh config here, and the copy carried InsecureSkipVerify but not
	// VerifyPeerCertificate: with pin= set, BuildClientTLSConfig turns off
	// chain verification precisely because it installs a pin verifier, so the
	// TCP-WS leg ended up accepting any certificate. The default path lost
	// RootCAs (the embedded Kaitu CA) and the blocked-CA check the same way.
	// Don't reintroduce a copy — every field it forgets fails open.

	// Create uTLS connection with fingerprint
	slog.Debug("TCPWSClient: starting uTLS handshake", "sni", tlsCfg.ServerName, "fingerprint", fmt.Sprintf("%v", clientHelloID))
	uconn := tls.UClient(conn, tlsCfg, clientHelloID)
	if err := uconn.HandshakeContext(connectCtx); err != nil {
		conn.Close()
		slog.Debug("TCPWSClient: uTLS handshake failed", "err", err)
		return nil, fmt.Errorf("wire: uTLS handshake: %w", err)
	}
	slog.Debug("wire: TCP-WS TLS details",
		"tlsVersion", uconn.ConnectionState().Version,
		"cipherSuite", uconn.ConnectionState().CipherSuite,
		"negotiatedProto", uconn.ConnectionState().NegotiatedProtocol,
	)

	// 3. WebSocket upgrade over TLS connection
	slog.Debug("TCPWSClient: starting WebSocket upgrade", "host", c.cfg.Host)
	wsConn, err := c.upgradeWebSocket(connectCtx, uconn)
	if err != nil {
		uconn.Close()
		slog.Debug("TCPWSClient: WebSocket upgrade failed", "err", err)
		return nil, fmt.Errorf("wire: WebSocket upgrade: %w", err)
	}
	slog.Info("wire: TCP-WS connected",
		"totalMs", time.Since(connectStart).Milliseconds(),
		"fingerprint", fmt.Sprintf("%v", clientHelloID),
		"sni", tlsCfg.ServerName,
		"hasECH", len(tlsCfg.EncryptedClientHelloConfigList) > 0,
		"remoteAddr", conn.RemoteAddr().String(),
	)

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
func (c *TCPWSClient) DialTCP(ctx context.Context, addr string, earlyData []byte) (net.Conn, error) {
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

	// Write early data before waiting for server response (same as QUIC path).
	if len(earlyData) > 0 {
		if _, err := stream.Write(earlyData); err != nil {
			stream.Close()
			return nil, fmt.Errorf("wire: write early data: %w", err)
		}
	}

	// Past this point, the request has been sent to the server. The wire
	// transport is proven working. All errors are proxy-layer.
	br := bufio.NewReader(stream)
	ok, errCode, errMsg, err := ReadStreamResponse(br)
	if err != nil {
		stream.Close()
		return nil, readResponseError(err)
	}
	if !ok {
		stream.Close()
		if errCode > 0 {
			return nil, &StreamError{Code: errCode, Message: errMsg}
		}
		return nil, &ProxyLayerError{Err: fmt.Errorf("%w: %s", ErrStreamRejected, errMsg)}
	}

	return &smuxStreamConn{Stream: stream, br: br}, nil
}

// RunEchoProbe implements EchoProber for TCP-WS data-path verification
// (mirrors QUIC's EchoProbe, wire/echo.go). Reuses the cached smux session
// via connect() exactly like DialTCP — a probe must not force a fresh
// connection when a live session is already cached. Unlike QUIC, no H3 frame
// type marker precedes the stream header: DialTCP's smux stream-opening
// sequence is WriteStreamHeader directly, and this matches it — a mismatched
// preamble would desync the server's reader and poison the muxed session.
func (c *TCPWSClient) RunEchoProbe(ctx context.Context) error {
	sess, err := c.connect(ctx)
	if err != nil {
		return fmt.Errorf("echo: connect: %w", err)
	}

	stream, err := sess.OpenStream()
	if err != nil {
		return fmt.Errorf("echo: open stream: %w", err)
	}
	defer stream.Close()

	if err := WriteStreamHeader(stream, StreamTypeEcho, ""); err != nil {
		return fmt.Errorf("echo: write header: %w", err)
	}

	var nonce [echoNonceLen]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return fmt.Errorf("echo: generate nonce: %w", err)
	}
	if _, err := stream.Write(nonce[:]); err != nil {
		return fmt.Errorf("echo: write nonce: %w", err)
	}

	// Set read deadline from context so reads respect cancellation, matching
	// QUIC's EchoProbe — smux.Stream.SetReadDeadline exists on the concrete
	// type returned by OpenStream (confirmed via smux/v2 stream.go).
	if deadline, ok := ctx.Deadline(); ok {
		stream.SetReadDeadline(deadline)
	}

	var resp [echoNonceLen]byte
	if _, err := io.ReadFull(stream, resp[:]); err != nil {
		return fmt.Errorf("echo: read response: %w", err)
	}

	if nonce != resp {
		return errors.New("echo: nonce mismatch")
	}
	return nil
}

// DialUDP opens a UDP proxy session through a shared smux UDP stream.
// Returns a PacketConn backed by SmuxUDPFrame messages.
// Uses check-release-act-reacquire to avoid holding mu during blocking I/O.
func (c *TCPWSClient) DialUDP(ctx context.Context, addr string) (net.PacketConn, error) {
	slog.Debug("TCPWSClient.DialUDP", "addr", addr)
	sess, err := c.connect(ctx)
	if err != nil {
		slog.Warn("TCPWSClient.DialUDP: connect failed", "addr", addr, "err", err)
		return nil, err
	}

	// Fast path: a live mux is already initialized.
	c.mu.Lock()
	if c.alloc == nil {
		c.mu.Unlock()
		return nil, ErrNotConnected
	}
	live, evicted := c.takeLiveUDPMuxLocked()
	alloc := c.alloc
	c.mu.Unlock()
	c.discardDeadUDPMux(evicted)
	if live != nil {
		return c.newSmuxUDPConn(live, alloc, addr), nil
	}

	// Slow path: initialize UDP mux. Serialized by initUDPMu.
	c.initUDPMu.Lock()
	defer c.initUDPMu.Unlock()

	// Re-check: another goroutine may have initialized while we waited.
	c.mu.Lock()
	if c.alloc == nil {
		c.mu.Unlock()
		return nil, ErrNotConnected
	}
	live, evicted = c.takeLiveUDPMuxLocked()
	alloc = c.alloc
	c.mu.Unlock()
	c.discardDeadUDPMux(evicted)
	if live != nil {
		return c.newSmuxUDPConn(live, alloc, addr), nil
	}

	// Blocking I/O — no lock held.
	stream, err := sess.OpenStream()
	if err != nil {
		return nil, fmt.Errorf("wire: open UDP stream: %w", err)
	}
	if err := WriteStreamHeader(stream, StreamTypeUDP, ""); err != nil {
		stream.Close()
		return nil, fmt.Errorf("wire: write UDP stream header: %w", err)
	}

	// Store result under lock. Re-check for concurrent resetConnection.
	// The session itself may also have been swapped while we dialed
	// (checkCachedSession evicts a dead session, connect() installs a fresh
	// one) — caching a mux built on the superseded session would hand out a
	// stream that can never carry a frame.
	c.mu.Lock()
	if c.alloc == nil || c.sess != sess {
		c.mu.Unlock()
		stream.Close()
		return nil, ErrNotConnected
	}
	c.udpMux = newSmuxUDPMux(stream)
	mux := c.udpMux
	alloc = c.alloc
	c.mu.Unlock()

	return c.newSmuxUDPConn(mux, alloc, addr), nil
}

// takeLiveUDPMuxLocked returns the cached UDP mux if it is still usable, and
// evicts it if it has died. A dead mux comes back as `evicted`; the caller
// must dispose of it via discardDeadUDPMux with no lock held.
//
// Why eviction has to live here: the cache is otherwise only cleared when the
// whole smux *session* dies (checkCachedSession / resetConnection / Close).
// A stream-level death — writeFrame's 5s deadline, or the peer closing the
// relay stream — leaves the session alive, so before this the dead mux was
// handed out for the rest of the session's life and every UDP send failed
// instantly while TCP proxying and the heartbeat stayed healthy (#3420).
//
// Caller must hold c.mu.
func (c *TCPWSClient) takeLiveUDPMuxLocked() (live, evicted *smuxUDPMux) {
	if c.udpMux == nil {
		return nil, nil
	}
	if !c.udpMux.dead.Load() {
		return c.udpMux, nil
	}
	evicted = c.udpMux
	c.udpMux = nil
	return nil, evicted
}

// discardDeadUDPMux disposes of an evicted mux and records the rebuild.
// Must be called with no lock held — close() touches smux internals.
func (c *TCPWSClient) discardDeadUDPMux(m *smuxUDPMux) {
	if m == nil {
		return
	}
	// close() also closes m.done, releasing any smuxUDPConn still parked in
	// ReadFrom on the evicted mux. writeFrame's failure path only closes the
	// stream, so this is what actually unblocks those consumers.
	m.close()
	slog.Warn("DIAG: udp-mux-rebuild",
		"transport", "tcpws",
		"reason", m.deathReason(),
		"rebuilds", c.udpMuxRebuilds.Add(1),
	)
}

// DialStreamUDP opens a stream-backed UDP session.
// TCPWSClient already uses smux streams for all UDP, so this is identical to DialUDP.
func (c *TCPWSClient) DialStreamUDP(ctx context.Context, addr string) (net.PacketConn, error) {
	return c.DialUDP(ctx, addr)
}

// newSmuxUDPConn creates a smuxUDPConn with a new session ID.
func (c *TCPWSClient) newSmuxUDPConn(mux *smuxUDPMux, alloc *SessionIDAllocator, addr string) *smuxUDPConn {
	sessionID := alloc.Next()
	readCh, reaped := mux.register(sessionID)
	slog.Debug("TCPWSClient.DialUDP: session ready", "addr", addr, "sessionID", sessionID)
	return &smuxUDPConn{
		mux:        mux,
		sessionID:  sessionID,
		targetAddr: addr,
		readCh:     readCh,
		reaped:     reaped,
		done:       make(chan struct{}),
	}
}

// extractSessionLocked nils all session resource fields and returns them.
// Caller must hold c.mu. Resources must be closed OUTSIDE the lock.
func (c *TCPWSClient) extractSessionLocked() (mux *smuxUDPMux, sess *smux.Session) {
	mux = c.udpMux
	c.udpMux = nil
	sess = c.sess
	c.sess = nil
	c.alloc = nil
	return
}

// resetConnection closes cached session resources without marking client as closed.
// After reset, the next DialTCP/DialUDP will establish a fresh connection.
func (c *TCPWSClient) resetConnection() {
	c.mu.Lock()
	mux, sess := c.extractSessionLocked()
	c.mu.Unlock()
	// NOTE: do NOT set c.closed = true — client must remain usable
	if mux != nil {
		mux.close()
	}
	if sess != nil {
		closeSmuxSessionSafe(sess)
	}
}

// Reset closes cached connections and clears session state.
// Called by TransportManager.Reset() on network change.
// After Reset(), the next DialTCP/DialUDP will establish a fresh connection.
func (c *TCPWSClient) Reset() {
	c.resetConnection()
}

// Close closes the smux session and underlying connection.
func (c *TCPWSClient) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	mux, sess := c.extractSessionLocked()
	c.mu.Unlock()
	if mux != nil {
		mux.close()
	}
	if sess != nil {
		closeSmuxSessionSafe(sess)
	}
	return nil
}

// ============================================================================
// TCPWSServer — implements wire.Listener
// ============================================================================

// TCPWSServer is the TCP-WS + smux server transport.
type TCPWSServer struct {
	// Device-auth state, mirroring QUICServer. The metadata stream (StreamType
	// 0x10) authenticates the session; under enforce, unauthenticated proxy
	// streams are rejected. Both are immutable after construction/SetX (called
	// before HandleConn), so no lock is needed on the read path.
	validate Validator
	adm      admissionPolicy // device-auth admission; see admission.go

	// cookieSigner enables the local cookie fast path; nil = cold auth only
	// (legacy TLS mode without cert_dir, k2p). Immutable after SetCookieSigner
	// (called before HandleConn), no lock needed on the read path. Mirrors
	// QUICServer.cookieSigner.
	cookieSigner *CookieSigner

	// sessions is the live-connection registry + revalidation sweeper (spec
	// §6.5); nil = disabled (tests, k2p). Immutable after EnableRevalidation
	// (called before HandleConn/HandleSession). Mirrors QUICServer.sessions.
	sessions *authSessions

	// rollout is the Phase 3 enforce-rollout counter set; set by
	// SetRolloutStats, immutable after (called before HandleConn/HandleSession).
	// nil-safe (all Record* are nil-receiver safe). Mirrors QUICServer.rollout.
	rollout *RolloutStats

	mu    deadlock.Mutex
	tcpCh chan tcpAcceptResult
	// udpH is an egress sink: reach it only through admitUDP, never directly.
	udpH UDPHandler
	done chan struct{}
}

// NewTCPWSServer creates a new TCP-WS server transport.
func NewTCPWSServer() *TCPWSServer {
	return &TCPWSServer{
		adm:   admissionPolicy{authWait: authWaitDefault},
		tcpCh: make(chan tcpAcceptResult, 64),
		done:  make(chan struct{}),
	}
}

// SetValidator sets the Validator used by metadata stream auth. Must be
// called before HandleConn/HandleSession. Mirrors QUICServer.SetValidator so a
// node authenticates identically on both transports.
func (s *TCPWSServer) SetValidator(v Validator) {
	s.validate = v
}

// CloseConnections closes every live smux session (TLS close_notify + FIN), so
// clients see the server go away at once. Call it on shutdown; see
// QUICServer.CloseConnections.
func (s *TCPWSServer) CloseConnections() int {
	if s.sessions == nil {
		return 0
	}
	return s.sessions.closeAll(closeShutdown)
}

// SetEnforce toggles TCP-WS device-auth enforcement. k2s (server.New) always
// sets true on both transports — enforcement must cover BOTH, else an attacker
// forces TCP-WS fallback to bypass QUIC auth. When true, proxy streams on a
// session that failed (or never sent) metadata auth are rejected. False (the
// zero value, permissive) remains for wire-level tests: an unauthenticated
// session is served, attributed to the empty-udid bucket.
func (s *TCPWSServer) SetEnforce(v bool) {
	s.adm.enforce = v
}

// SetRolloutStats mirrors QUICServer.SetRolloutStats — both transports feed
// the SAME counter set so the DIAG: auth-rollout summary covers the node.
func (s *TCPWSServer) SetRolloutStats(r *RolloutStats) { s.rollout = r }

// SetCookieSigner enables node-local cookie issue/verify (spec §6). Must be
// called before HandleConn/HandleSession. Mirrors QUICServer.SetCookieSigner.
func (s *TCPWSServer) SetCookieSigner(cs *CookieSigner) { s.cookieSigner = cs }

// EnableRevalidation creates the connection registry and starts the periodic
// revalidation sweeper against v (pass the same cached validator chain used
// for cold auth so Center load is shared). Must be called before
// HandleConn/HandleSession; Close() tears the sweeper down. Mirrors
// QUICServer.EnableRevalidation.
func (s *TCPWSServer) EnableRevalidation(v Validator) {
	s.sessions = newAuthSessions(v, authSweepInterval)
}

// admitTCP is the only route from an accepted proxy stream to AcceptTCP, and so
// the whole of the TCP admission gate — the TCP-WS twin of
// QUICServer.admitTCP. See admission.go and TestAdmissionGate_EgressSinksAreGated.
func (s *TCPWSServer) admitTCP(auth *connAuth, stream *smux.Stream, br *bufio.Reader, remote net.Addr, dev *deviceIDHolder, addr string, connLog *slog.Logger) {
	if !s.adm.admit(auth) {
		connLog.Warn("TCPWSServer: TCP rejected (enforce: unauthenticated)", "addr", addr)
		WriteStreamResponse(stream, fmt.Errorf("unauthorized"))
		stream.Close()
		return
	}
	s.sendTCP(tcpAcceptResult{
		conn: &smuxStreamConn{Stream: stream, br: br, remoteAddr: remote, dev: dev},
		addr: addr,
	})
}

// admitUDP is the only reader of s.udpH, and so the whole of the UDP admission
// gate — the TCP-WS twin of QUICServer.admitUDP. ok=false means the session
// failed admission; a nil handler with ok=true means ServeUDP has not been
// called yet, which is not an auth condition.
func (s *TCPWSServer) admitUDP(auth *connAuth) (handler UDPHandler, ok bool) {
	if !s.adm.admit(auth) {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.udpH, true
}

// HandleConn processes a new TCP-WS client connection.
// Creates a smux session and starts accepting streams.
// Called by the server daemon for each authenticated client connection.
func (s *TCPWSServer) HandleConn(conn net.Conn) {
	remote := conn.RemoteAddr()
	connLog := slog.With("remote", remote)
	sess, err := smux.Server(conn, ServerSmuxConfig())
	if err != nil {
		conn.Close()
		return
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				connLog.Error("TCPWSServer: panic in handleSession", "panic", r)
			}
		}()
		s.handleSession(sess, connLog, remote)
	}()
}

// HandleSession processes streams from a smux session, blocking until it ends.
// Used by handleHTTP for WebSocket connections where the caller must block
// to keep the HTTP handler (and hijacked connection) alive.
func (s *TCPWSServer) HandleSession(sess *smux.Session) {
	remote := sess.RemoteAddr()
	connLog := slog.With("remote", remote)
	s.handleSession(sess, connLog, remote)
}

// tcpwsMaxStreams caps concurrently handled streams per TCP-WS session. A
// k2 client opens at most core's maxTunnelConns (4096 on desktop) plus a few
// control streams, and streams it has closed may still be finishing here
// (pipe's half-close grace), so the cap is twice that; anything past it is
// not a k2 client, and each stream
// costs the server a goroutine. QUIC has the same cap via
// MaxIncomingStreams, where the peer queues instead of being refused.
// var, not const, so tests can lower it.
var tcpwsMaxStreams int32 = 8192

func (s *TCPWSServer) handleSession(sess *smux.Session, connLog *slog.Logger, remote net.Addr) {
	defer sess.Close()
	dev := &deviceIDHolder{}
	auth := newConnAuth()
	s.rollout.RecordConn()
	defer func() {
		// !settled() alone is not enough to decide "count as legacy": the
		// metadata goroutine may still be resolving its verdict (see
		// connAuth.rolloutCounted). claimRolloutCount() is the actual
		// tie-breaker — only the winner of that race increments a counter.
		if !auth.settled() && auth.claimRolloutCount() {
			s.rollout.RecordLegacyNoMetadata()
		}
	}()
	var as *authSession
	if s.sessions != nil {
		as = newAuthSession(auth, func(closeReason) { sess.Close() })
		unregister := s.sessions.register(as)
		defer unregister()
	}
	var active atomic.Int32
	refused := false
	for {
		stream, err := sess.AcceptStream()
		if err != nil {
			return
		}
		if active.Add(1) > tcpwsMaxStreams {
			active.Add(-1)
			if !refused {
				refused = true
				connLog.Warn("DIAG: tcpws-stream-cap", "max", tcpwsMaxStreams)
			}
			stream.Close()
			continue
		}
		go func() {
			defer active.Add(-1)
			defer func() {
				if r := recover(); r != nil {
					connLog.Error("TCPWSServer: panic in handleStream", "panic", r)
				}
			}()
			s.handleStream(sess, stream, connLog, remote, dev, auth, as)
		}()
	}
}

func (s *TCPWSServer) handleStream(sess *smux.Session, stream *smux.Stream, connLog *slog.Logger, remote net.Addr, dev *deviceIDHolder, auth *connAuth, as *authSession) {
	br := bufio.NewReader(stream)

	stream.SetReadDeadline(time.Now().Add(streamHeaderTimeout))
	streamType, addr, err := ReadStreamHeader(br)
	if err != nil {
		stream.Close()
		return
	}
	if streamType != StreamTypeMeta { // metadata: the deadline covers its frame too
		stream.SetReadDeadline(time.Time{})
	}

	switch streamType {
	case StreamTypeMeta:
		s.handleMetadataStream(sess, stream, br, connLog, dev, auth, as)
	case StreamTypeTCP:
		s.admitTCP(auth, stream, br, remote, dev, addr, connLog)
	case StreamTypeUDP:
		s.handleUDPStream(stream, br, connLog, remote, dev, auth)
	case StreamTypeEcho:
		handleEcho(stream, br)
		stream.Close()
	default:
		stream.Close()
	}
}

// handleMetadataStream processes a TCP-WS connection metadata stream
// (StreamType 0x10). Mirrors QUICServer.handleMetadataStream.
//
// Rollout counting (spec §7.2): recordVerdict fires once, right after
// auth.settle(st.authed), covering the enforce-reject, permissive-accept, and
// success branches below. A pre-parse ReadMetadataFrame failure (bad
// metadata) returns before that point — the deferred auth.settle(false)
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
// auth.rolloutCounted via CAS and only proceed on a win — so every
// connection contributes at most one rollout-counter increment, no matter
// which of those two paths gets there first or how many metadata streams the
// client opens.
//
// Attribution follows the SERVER-side verdict, never the self-reported value:
// only a validated connection gets dev.set (a Center-validated udid is
// trustworthy — Center rejects device.UDID != udid), and a failed or absent
// validation lands in the empty-udid bucket (spec §5.3 udid 收口).
//
// Response contract (spec §5.2): permissive ALWAYS answers OK — rollout must
// never fail a legacy-credential client. Under enforce, an explicit rejection
// (Code 401/402/403 — only Center issues these) answers with the coded 0x02
// form so the client can classify without string matching; "cannot determine"
// and parse failures answer the legacy 0x01 form (no invented code). The
// verdict is always settled; the deferred settle is the fail-closed backstop.
func (s *TCPWSServer) handleMetadataStream(sess *smux.Session, stream *smux.Stream, br *bufio.Reader, connLog *slog.Logger, dev *deviceIDHolder, auth *connAuth, as *authSession) {
	defer auth.settle(false)

	meta, err := ReadMetadataFrame(br)
	if err != nil {
		connLog.Warn("TCPWSServer: metadata stream parse failed", "err", err)
		WriteStreamResponse(stream, fmt.Errorf("bad metadata"))
		stream.Close()
		return
	}

	connLog.Info("TCPWSServer: metadata stream", "udid", meta.UDID, "mode", meta.Mode, "version", meta.Version)
	if meta.SmuxHalfClose {
		// Set before any verdict: the client opens proxy streams only after
		// our response, so none of them predate it.
		sess.SetPeerHalfClose(true)
	}

	// Single decision point shared verbatim with QUIC (spec §9 two-transport
	// consistency): a valid cookie short-circuits the validator chain entirely
	// and issues no new cookie; otherwise this runs cold auth through
	// s.validate exactly as before, now also minting a cookie on success.
	st := resolveMetadataAuth(s.cookieSigner, s.validate, meta, s.adm.enforce, time.Now())

	// Attribution from the SERVER-determined identity (spec §5.3): cookie's
	// udid on a hit, Center-confirmed udid on cold OK, "" otherwise (empty-udid
	// bucket). TCP-WS has no ctx.mode (unlike QUIC) — mirrors QUICServer's
	// unconditional dev.set.
	dev.set(st.udid)
	dev.setSession(st.sessionID)
	auth.settle(st.authed)
	if auth.claimRolloutCount() {
		recordVerdict(s.rollout, st.cookieHit, st.authed)
	}

	if !st.authed {
		connLog.Warn("TCPWSServer: metadata auth rejected", "udid", meta.UDID, "code", st.code, "enforce", s.adm.enforce)
		if s.adm.enforce {
			if st.code != 0 {
				WriteStreamResponse(stream, &StreamError{Code: st.code, Message: "auth rejected"})
			} else {
				WriteStreamResponse(stream, fmt.Errorf("unauthorized"))
			}
			stream.Close()
			sess.Close() // tear the session, mirroring QUIC's CloseWithError
			return
		}
		// Permissive: accept, no attribution (empty-udid bucket), never block.
		WriteMetadataResponse(stream, "", true)
		stream.Close()
		return
	}

	// Publish the identity to the sweeper before answering: once the client
	// has its OK it may use the session, and a sweep that ran before
	// markAuthed would skip it as never authenticated (a revocation missed).
	if as != nil {
		as.markAuthed(st.udid, st.token, st.mode, st.authAt, st.svcExp)
	}
	// newCookie is non-empty ONLY after a Center cold auth (spec §6.3) — a
	// cookie hit answers with an empty kv and the client keeps its copy.
	WriteMetadataResponse(stream, st.newCookie, true)
	stream.Close()
}

// runSmuxUDPHandler invokes handler.HandleUDP and removes the session from
// the per-stream map when it returns. ProxyHandler.HandleUDP returns within
// ~60s of target idle (server/handler.go:286 SetReadDeadline), so without
// this cleanup the map grows monotonically and eventually hits
// maxServerUDPSessions (4096), silently dropping new flows.
//
// closeOnce in smuxServerUDPConn.close() makes the cleanup idempotent vs the
// stream-shutdown path that calls close on every remaining session.
func runSmuxUDPHandler(
	sessions map[uint32]*smuxServerUDPConn,
	mu *deadlock.Mutex,
	sid uint32,
	sess *smuxServerUDPConn,
	handler UDPHandler,
	addr string,
) {
	defer func() {
		if r := recover(); r != nil {
			sess.log.Error("TCPWSServer: panic in HandleUDP", "panic", r, "addr", addr)
		}
		mu.Lock()
		delete(sessions, sid)
		mu.Unlock()
		sess.close()
	}()
	handler.HandleUDP(sess, addr)
}

func (s *TCPWSServer) handleUDPStream(stream *smux.Stream, br *bufio.Reader, connLog *slog.Logger, remote net.Addr, dev *deviceIDHolder, auth *connAuth) {
	mux := &serverSmuxWriter{stream: stream}
	sessions := make(map[uint32]*smuxServerUDPConn)
	var sessionsMu deadlock.Mutex
	var remoteStr string
	if remote != nil {
		remoteStr = remote.String()
	}

	for {
		frame, err := ReadSmuxUDPFrame(br)
		if err != nil {
			// Close all sessions
			sessionsMu.Lock()
			for _, sess := range sessions {
				sess.close()
			}
			sessionsMu.Unlock()
			return
		}

		sessionsMu.Lock()
		sess, ok := sessions[frame.SessionID]
		if !ok {
			if len(sessions) >= maxServerUDPSessions {
				sessionsMu.Unlock()
				continue // at capacity, drop frame
			}
			// Admission gate: on denial no session is created, and the stream is
			// torn down rather than left to re-ask on every new session id —
			// a session that never sends metadata would otherwise pay the
			// admission wait again per frame.
			//
			// admit() may wait, so release the map lock first — runSmuxUDPHandler
			// needs it to reap finished sessions. Only this loop creates
			// sessions, so nothing can add one while the lock is released.
			sessionsMu.Unlock()
			handler, admitted := s.admitUDP(auth)
			if !admitted {
				connLog.Warn("TCPWSServer: UDP stream rejected (enforce: unauthenticated)",
					"sid", frame.SessionID, "addr", frame.Addr)
				sessionsMu.Lock()
				for _, sess := range sessions {
					sess.close()
				}
				sessionsMu.Unlock()
				stream.Close()
				return
			}
			sessionsMu.Lock()
			sessLog := connLog.With("sid", frame.SessionID, "addr", frame.Addr)
			sess = &smuxServerUDPConn{
				writer:     mux,
				sessionID:  frame.SessionID,
				addr:       frame.Addr,
				remoteAddr: remoteStr,
				log:        sessLog,
				readCh:     make(chan *UDPFrame, 32),
				done:       make(chan struct{}),
				dev:        dev,
			}
			sessions[frame.SessionID] = sess

			if handler != nil {
				addr := frame.Addr
				sid := frame.SessionID
				go runSmuxUDPHandler(sessions, &sessionsMu, sid, sess, handler, addr)
			}
		}
		sessionsMu.Unlock()

		sess.deliver(frame)
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

// Close stops the server and tears down the revalidation sweeper (if
// enabled). sessions.Close() calls wg.Wait() internally, which must never run
// under s.mu (Rule #3 — waiting under lock risks deadlock if the goroutine
// being waited on ever needs that same lock), so it runs after s.mu is
// released. Mirrors QUICServer.Close.
func (s *TCPWSServer) Close() error {
	s.mu.Lock()
	select {
	case <-s.done:
		s.mu.Unlock()
		return nil
	default:
		close(s.done)
	}
	s.mu.Unlock()
	if s.sessions != nil {
		s.sessions.Close() // wg.Wait — no lock held (Rule #3)
	}
	return nil
}

// ============================================================================
// Client-side shared UDP stream mux
// ============================================================================

// smuxUDPMux is the client-side multiplexer over a single smux.Stream that
// carries all UDP relay frames for a TCPWSClient. There is exactly one mux
// per TCPWSClient instance — all UDP sessions share one mux — so a stall on
// writeMu freezes the entire UDP data plane for that client.
//
// Design (mirror of serverSmuxWriter for stream-level isolation; see
// commit 17e06b1 for jp01 May 2026 incident and rationale):
//
//  1. writeMu is sync.Mutex (NOT deadlock.Mutex). It is a serialization
//     lock that legitimately holds across smux.Stream.Write, which may
//     block under peer-window flow control when the server's
//     handleUDPStream main loop stalls (server-side fix in deliver()
//     mitigates the most common cause but does not eliminate it).
//     deadlock.Mutex's 30/60s watchdog would mis-detect this as a
//     deadlock and trigger os.Exit(2) on desktop production builds (which
//     do NOT carry -tags=deadlock_disable) — the v0.4.4 daemon-FATAL
//     root cause this fix exists for.
//
//  2. Each Write has a deadline (streamWriteDeadline = 5s). If the write
//     does not complete in that window, the stream is treated as dead:
//     smux.Stream is Close()d, future writeFrame calls fast-fail via the
//     dead flag without queueing on the mutex.
//
//  3. Failure stays at mux level. Stream.Close cascades through smux:
//     ReadSmuxUDPFrame returns ErrClosedPipe → readLoop exits and closes
//     m.done → smuxUDPConn.ReadFrom returns net.ErrClosed. Consumers
//     naturally unwind; TCPWSClient rebuilds a fresh mux on next DialUDP.
//
//     The rebuild half lives in takeLiveUDPMuxLocked — do not assume it is
//     implicit. From 907f724 (2026-05) until #3420 (2026-08) this paragraph
//     was the ONLY place the rebuild existed: DialUDP kept handing out the
//     dead mux, so one write failure killed all proxy UDP (and therefore all
//     proxy DNS) for the rest of the smux session while TCP and the
//     heartbeat stayed healthy. Any new cache read of udpMux must go through
//     takeLiveUDPMuxLocked.
type smuxUDPMux struct {
	stream      *smux.Stream
	br          *bufio.Reader
	writeMu     sync.Mutex // serialization, may hold across I/O — see (1)
	streamOnce  sync.Once  // guards stream.Close (called from close() + writeFrame failure)
	dead        atomic.Bool
	deadReason  atomic.Value // string; written once, by whoever first marks dead
	mu          deadlock.Mutex
	sessions    map[uint32]chan *UDPFrame
	sessionDone map[uint32]chan struct{} // closed by reaper to notify smuxUDPConn
	lastActive  map[uint32]*atomic.Int64
	done        chan struct{}
	closeOnce   sync.Once
	started     bool

	// Downlink drop counters, same split and same semantics as quicUDPMux's:
	// udpRxDrops is a LIVE session losing packets, udpRxDropsNoSession is the
	// benign tail of frames arriving for a closed or reaped session.
	// Deliberately no udpTxDrops: this mux never silently discards an uplink
	// packet — writeFrame either writes the frame or returns an error that
	// tears the session down. See WireStats for what 0 means on which path.
	udpRxDrops          atomic.Int64 // downlink frames dropped (session channel full)
	udpRxDropsNoSession atomic.Int64 // downlink frames for an unknown session
}

func newSmuxUDPMux(stream *smux.Stream) *smuxUDPMux {
	return &smuxUDPMux{
		stream:      stream,
		br:          bufio.NewReader(stream),
		sessions:    make(map[uint32]chan *UDPFrame),
		sessionDone: make(map[uint32]chan struct{}),
		lastActive:  make(map[uint32]*atomic.Int64),
		done:        make(chan struct{}),
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
	go m.reapIdleSessions()
}

func (m *smuxUDPMux) closeDone() {
	m.closeOnce.Do(func() { close(m.done) })
}

func (m *smuxUDPMux) readLoop() {
	defer m.closeDone()
	for {
		frame, err := ReadSmuxUDPFrame(m.br)
		if err != nil {
			// The relay stream is gone. Mark the mux dead so the next
			// DialUDP rebuilds instead of reusing it — otherwise writes keep
			// being accepted into a stream that no longer exists until one of
			// them finally errors (22s of silently dropped DNS in #3420).
			// Deliberately no closeStreamOnce here: this is the read-exit
			// path and Close may block; eviction closes the stream.
			m.markDeadOnce(deathReasonPeerClosed)
			slog.Warn("smuxUDPMux: readLoop exiting", "err", err)
			return
		}
		m.dispatchFrame(frame)
	}
}

// dispatchFrame delivers a decoded frame to its session's channel, dropping
// it if the session is unknown (counted as udpRxDropsNoSession) or its
// channel is full (counted as udpRxDrops). Both used to be silent: the
// fallback transport reported no drops at all, so a heartbeat read of
// udpRxDrops=0 during a TCP-WS fallback meant "not instrumented", not "not
// dropping" — and fallback is precisely when a user is most likely to be
// stuttering. Split out of readLoop so the drop paths are unit-testable
// without a live smux session.
func (m *smuxUDPMux) dispatchFrame(frame *UDPFrame) {
	m.mu.Lock()
	ch, ok := m.sessions[frame.SessionID]
	m.mu.Unlock()
	if !ok {
		m.udpRxDropsNoSession.Add(1)
		slog.Debug("smuxUDPMux: no session for frame", "sessionID", frame.SessionID,
			"addr", frame.Addr, "payloadLen", len(frame.Payload))
		return
	}
	m.touchSession(frame.SessionID)
	select {
	case ch <- frame:
	default:
		m.udpRxDrops.Add(1)
		slog.Warn("smuxUDPMux: channel full, dropping frame", "sessionID", frame.SessionID,
			"addr", frame.Addr, "payloadLen", len(frame.Payload))
	}
}

func (m *smuxUDPMux) writeFrame(sessionID uint32, addr string, payload []byte) error {
	// Fast-path bypass: once dead, do not queue on the mutex. This drains any
	// backlog of waiting writers immediately so cleanup propagates fast.
	if m.dead.Load() {
		return m.deadErr()
	}

	m.writeMu.Lock()
	// Re-check after acquiring the lock: a peer goroutine that just timed
	// out may have marked the mux dead while we were waiting on Lock.
	if m.dead.Load() {
		m.writeMu.Unlock()
		return m.deadErr()
	}

	_ = m.stream.SetWriteDeadline(time.Now().Add(streamWriteDeadline))
	start := time.Now()
	err := WriteSmuxUDPFrame(m.stream, sessionID, addr, payload)
	if err != nil {
		// Store dead under writeMu so any subsequent writer that acquires
		// the lock sees dead=true immediately and fast-fails — no head-of-
		// line queue keeps growing while we tear down.
		m.markDeadOnce(deathReasonWriteFail)
	}
	m.writeMu.Unlock()

	if err != nil {
		// Stream cleanup outside writeMu so we never call Close (which may
		// touch smux internal locks) under our serialization lock.
		slog.Warn("DIAG: stream-killed-on-write-fail",
			"transport", "tcpws", "side", "client", "sessionID", sessionID,
			"len", len(payload), "writeMs", time.Since(start).Milliseconds(), "err", err)
		m.closeStreamOnce()
	}
	return err
}

// Reasons a UDP relay mux died, surfaced in errStreamDead and in the
// udp-mux-rebuild DIAG event. Knowing which one fired is the difference
// between "our write stalled" and "the peer hung up".
const (
	deathReasonWriteFail  = "write-fail"  // our Write hit the 5s deadline or errored
	deathReasonPeerClosed = "peer-closed" // readLoop saw EOF/reset on the relay stream
	deathReasonClosed     = "closed"      // local teardown (close/reset)
)

// markDeadOnce sets the dead flag and records why. The first caller wins, so
// the recorded reason is the one that actually killed the mux rather than a
// later teardown that merely observed it.
func (m *smuxUDPMux) markDeadOnce(reason string) {
	if m.dead.CompareAndSwap(false, true) {
		m.deadReason.Store(reason)
	}
}

// deathReason reports why the mux died, or "" while it is still alive.
func (m *smuxUDPMux) deathReason() string {
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
func (m *smuxUDPMux) deadErr() error {
	if r := m.deathReason(); r != "" {
		return fmt.Errorf("%w (%s)", errStreamDead, r)
	}
	return errStreamDead
}

// markDead transitions the mux to the dead state and closes the underlying
// stream once. Safe to call from any goroutine; idempotent across concurrent
// callers via streamOnce. Used by close() to pre-mark the mux so concurrent
// writeFrame calls fast-fail without emitting a false-positive DIAG warning.
func (m *smuxUDPMux) markDead() {
	m.markDeadOnce(deathReasonClosed)
	m.closeStreamOnce()
}

func (m *smuxUDPMux) closeStreamOnce() {
	m.streamOnce.Do(func() {
		if m.stream != nil {
			_ = m.stream.Close()
		}
	})
}

func (m *smuxUDPMux) register(sessionID uint32) (chan *UDPFrame, chan struct{}) {
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

func (m *smuxUDPMux) unregister(sessionID uint32) {
	m.mu.Lock()
	delete(m.sessions, sessionID)
	delete(m.sessionDone, sessionID)
	delete(m.lastActive, sessionID)
	m.mu.Unlock()
}

func (m *smuxUDPMux) touchSession(sessionID uint32) {
	m.mu.Lock()
	ts := m.lastActive[sessionID]
	m.mu.Unlock()
	if ts != nil {
		ts.Store(time.Now().Unix())
	}
}

func (m *smuxUDPMux) reapIdleSessions() {
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

// reapOnce runs one pass of idle session cleanup. Extracted for testability.
func (m *smuxUDPMux) reapOnce() {
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
			slog.Debug("smuxUDPMux: reaped idle session", "sessionID", id)
		}
	}
	m.mu.Unlock()
}

func (m *smuxUDPMux) close() {
	// markDead first so any concurrent writeFrame fast-fails without
	// emitting a false-positive DIAG warning during graceful shutdown.
	m.markDead()
	m.closeDone()
}

// ============================================================================
// Client-side PacketConn over smux UDP stream
// ============================================================================

type smuxUDPConn struct {
	mux        *smuxUDPMux
	sessionID  uint32
	targetAddr string
	readCh     chan *UDPFrame
	reaped     chan struct{} // closed by mux reaper when session is idle-reaped
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
		slog.Warn("smuxUDPConn: ReadFrom timeout", "sessionID", c.sessionID,
			"target", c.targetAddr)
		return 0, nil, os.ErrDeadlineExceeded
	}
}

func (c *smuxUDPConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	dest := c.targetAddr
	if addr != nil {
		dest = addr.String()
	}
	if err := c.mux.writeFrame(c.sessionID, dest, b); err != nil {
		if errors.Is(err, io.ErrClosedPipe) || errors.Is(err, net.ErrClosed) {
			slog.Debug("smuxUDPConn: WriteTo failed (closed)", "sessionID", c.sessionID,
				"target", dest, "err", err)
		} else {
			slog.Warn("smuxUDPConn: WriteTo failed", "sessionID", c.sessionID,
				"target", dest, "len", len(b), "err", err)
		}
		return 0, err
	}
	slog.Debug("smuxUDPConn: WriteTo ok", "sessionID", c.sessionID,
		"target", dest, "len", len(b))
	return len(b), nil
}

func (c *smuxUDPConn) Close() error {
	c.closeOnce.Do(func() {
		close(c.done)
		c.mux.unregister(c.sessionID)
	})
	return nil
}

func (c *smuxUDPConn) LocalAddr() net.Addr               { return c.mux.stream.LocalAddr() }
func (c *smuxUDPConn) SetDeadline(t time.Time) error     { c.rd.set(t); return nil }
func (c *smuxUDPConn) SetReadDeadline(t time.Time) error { c.rd.set(t); return nil }
func (c *smuxUDPConn) SetWriteDeadline(time.Time) error  { return nil }

// ============================================================================
// Server-side smux UDP stream writer (shared, mutex-protected)
// ============================================================================
//
// Design (stream-level isolation):
//
//   1. writeMu is a serialization lock — sync.Mutex (not deadlock.Mutex)
//      because it legitimately holds across smux.Stream.Write, which can
//      block under peer-window flow control when a client app stops reading
//      its UDP relay. deadlock.Mutex would mis-detect this as a deadlock
//      and trigger os.Exit(2), crashing the whole k2v5 process (jp01 May 2026).
//
//   2. Each Write has a deadline (streamWriteDeadline). If a write doesn't
//      complete within that window, the stream is treated as dead: the
//      smux.Stream is Close()d, future writeFrame calls fast-fail via the
//      `dead` flag without queueing on the mutex.
//
//   3. Failure stays at stream level. Close() cascades through smux:
//      ReadSmuxUDPFrame() returns ErrClosedPipe → handleUDPStream main loop
//      tears down all sessions on this stream. Other streams on the same
//      smux session, other clients, and the process are all unaffected.

// streamWriteDeadline bounds the maximum time the writer mutex is held.
// Healthy smux.Stream.Write completes in microseconds; 5s is generous slack
// for transient slow networks. Past this, peer is presumed unable to read
// (stream is effectively dead) and we proactively kill the stream.
const streamWriteDeadline = 5 * time.Second

// errStreamDead is returned by writeFrame after the stream has been killed.
// Producers see this and abandon the UDP session cleanly.
var errStreamDead = errors.New("wire: UDP relay stream is dead")

type serverSmuxWriter struct {
	stream    *smux.Stream
	writeMu   sync.Mutex
	closeOnce sync.Once
	dead      atomic.Bool
}

func (w *serverSmuxWriter) writeFrame(sessionID uint32, addr string, payload []byte) error {
	// Fast-path bypass: once the stream is dead, don't queue on the mutex —
	// release any waiting producer immediately so cleanup propagates fast.
	if w.dead.Load() {
		return errStreamDead
	}

	w.writeMu.Lock()
	// Re-check after acquiring the lock: a peer goroutine that just timed out
	// may have marked the stream dead while we were waiting.
	if w.dead.Load() {
		w.writeMu.Unlock()
		return errStreamDead
	}

	_ = w.stream.SetWriteDeadline(time.Now().Add(streamWriteDeadline))
	start := time.Now()
	err := WriteSmuxUDPFrame(w.stream, sessionID, addr, payload)
	if err != nil {
		// Set dead under writeMu so any subsequent writer that acquires the
		// lock immediately sees dead=true and fast-fails — no head-of-line
		// queue keeps growing while we tear down.
		w.dead.Store(true)
	}
	w.writeMu.Unlock()

	if err != nil {
		// Stream cleanup outside the lock so we never call Close (which may
		// touch internal locks of smux) under writeMu.
		writeMs := time.Since(start).Milliseconds()
		slog.Warn("DIAG: stream-killed-on-write-fail",
			"transport", "tcpws", "sessionID", sessionID,
			"len", len(payload), "writeMs", writeMs, "err", err)
		w.closeStreamOnce()
	}
	return err
}

// markDead is the external entry point (e.g., from connection cleanup).
// It is safe to call from any goroutine — closeOnce gates the underlying
// Close to exactly one invocation.
func (w *serverSmuxWriter) markDead() {
	w.dead.Store(true)
	w.closeStreamOnce()
}

func (w *serverSmuxWriter) closeStreamOnce() {
	w.closeOnce.Do(func() {
		_ = w.stream.Close()
	})
}

// ============================================================================
// Server-side PacketConn for smux UDP sessions
// ============================================================================

type smuxServerUDPConn struct {
	writer     *serverSmuxWriter
	sessionID  uint32
	addr       string
	remoteAddr string
	log        *slog.Logger
	readCh     chan *UDPFrame
	done       chan struct{}
	closeOnce  sync.Once
	rd         readDeadline
	dev        *deviceIDHolder
}

var _ net.PacketConn = (*smuxServerUDPConn)(nil)
var _ UDPSessionInfo = (*smuxServerUDPConn)(nil)

func (c *smuxServerUDPConn) UDPSessionID() uint32  { return c.sessionID }
func (c *smuxServerUDPConn) UDPRemoteAddr() string { return c.remoteAddr }
func (c *smuxServerUDPConn) Logger() *slog.Logger  { return c.log }

// DeviceID implements ConnDeviceID. Returns "" if no holder is wired
// (defensive) or if the metadata stream hasn't landed yet.
func (c *smuxServerUDPConn) DeviceID() string {
	if c.dev == nil {
		return ""
	}
	return c.dev.get()
}

// SessionID implements ConnSessionID. "" until the metadata verdict lands.
func (c *smuxServerUDPConn) SessionID() string {
	if c.dev == nil {
		return ""
	}
	return c.dev.session()
}

func (c *smuxServerUDPConn) deliver(frame *UDPFrame) {
	select {
	case c.readCh <- frame:
	case <-c.done:
	default:
		// Drop: handler exited or too slow. Protocol-correct for unreliable UDP.
		// Without default, a full readCh blocks handleUDPStream main loop forever
		// — freezing smux peer-window flow control and triggering client-side
		// writeMu deadlock FATAL (desktop) or silent UDP freeze (mobile).
		// Mirrors serverUDPConn.deliver (quic.go) datagram-path protection.
		slog.Debug("DIAG: udp-deliver-drop", "transport", "tcpws",
			"sessionID", c.sessionID, "addr", c.addr, "len", len(frame.Payload))
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

func (c *smuxServerUDPConn) WriteTo(b []byte, addr net.Addr) (int, error) {
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

func (c *smuxServerUDPConn) close() {
	c.closeOnce.Do(func() { close(c.done) })
}

func (c *smuxServerUDPConn) Close() error {
	c.close()
	return nil
}

func (c *smuxServerUDPConn) LocalAddr() net.Addr               { return c.writer.stream.LocalAddr() }
func (c *smuxServerUDPConn) SetDeadline(t time.Time) error     { c.rd.set(t); return nil }
func (c *smuxServerUDPConn) SetReadDeadline(t time.Time) error { c.rd.set(t); return nil }
func (c *smuxServerUDPConn) SetWriteDeadline(time.Time) error  { return nil }

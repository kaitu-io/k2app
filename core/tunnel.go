package core

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sasha-s/go-deadlock"

	"github.com/kaitu-io/k2rule"
	"github.com/kaitu-io/k2/provider"
	"github.com/kaitu-io/k2/sniff"
	"github.com/kaitu-io/k2/wire"
	"golang.org/x/sync/semaphore"
)

// closeTimeout is the maximum time to wait for handler goroutines to exit
// after all tracked connections have been forcefully closed.
const closeTimeout = 5 * time.Second

// closeWriter is implemented by connections that support half-close (write-only FIN).
// pipe() uses this to send FIN without killing the read direction,
// allowing the peer to finish sending data before full close.
type closeWriter interface {
	CloseWrite() error
}

// connTracker tracks active connections for forceful close during shutdown.
type connTracker struct {
	mu    deadlock.Mutex
	conns map[io.Closer]struct{}
}

func newConnTracker() *connTracker {
	return &connTracker{conns: make(map[io.Closer]struct{})}
}

// track registers a connection. The returned function removes it.
func (ct *connTracker) track(c io.Closer) func() {
	ct.mu.Lock()
	ct.conns[c] = struct{}{}
	ct.mu.Unlock()
	return func() {
		ct.mu.Lock()
		delete(ct.conns, c)
		ct.mu.Unlock()
	}
}

// closeAll forcefully closes every tracked connection to unblock
// any blocking Read/Write in pipe/pipePacket.
func (ct *connTracker) closeAll() {
	ct.mu.Lock()
	conns := ct.conns
	ct.conns = make(map[io.Closer]struct{})
	ct.mu.Unlock()
	for c := range conns {
		c.Close()
	}
}

// WireErrorReporter receives runtime wire transport errors from the tunnel.
// Implemented by engine.Engine to surface errors in the status API.
type WireErrorReporter interface {
	ReportWireError(err error)
	ClearWireError()
}

// ClientTunnel connects a Provider to wire.Dialer, implementing provider.ConnectionHandler.
// It routes each TCP/UDP connection through the Router, then either direct (net.Dial)
// or proxy (wire.DialTCP/DialUDP).
type ClientTunnel struct {
	provider     provider.Provider
	dialer       wire.Dialer
	router       *Router
	directDialer wire.DirectDialer
	wireReporter WireErrorReporter
	sniffBT   bool // sniff BitTorrent and force direct (disabled in global mode)
	sniffSNI  bool // sniff TLS/QUIC SNI and upgrade DIRECT→PROXY (disabled in global mode)
	blockQUIC bool // block QUIC protocol going through proxy (prevents double-QUIC)

	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	conns   *connTracker
	connSem *semaphore.Weighted // limits concurrent handler goroutines

	txBytes atomic.Int64 // total bytes sent (local→remote)
	rxBytes atomic.Int64 // total bytes received (remote→local)
}

var _ provider.ConnectionHandler = (*ClientTunnel)(nil)

// ClientTunnelConfig holds dependencies for creating a ClientTunnel.
type ClientTunnelConfig struct {
	Provider     provider.Provider
	Dialer       wire.Dialer
	Router       *Router
	DirectDialer wire.DirectDialer
	WireReporter WireErrorReporter
	SniffBT   bool // enable BitTorrent sniffing (should be false in global mode)
	SniffSNI  bool // enable TLS/QUIC SNI sniffing for DIRECT→PROXY upgrade (should be false in global mode)
	BlockQUIC bool // block QUIC protocol going through proxy
}

// NewClientTunnel creates a new L4 client tunnel.
func NewClientTunnel(cfg ClientTunnelConfig) *ClientTunnel {
	return &ClientTunnel{
		provider:     cfg.Provider,
		dialer:       cfg.Dialer,
		router:       cfg.Router,
		directDialer: cfg.DirectDialer,
		wireReporter: cfg.WireReporter,
		sniffBT:   cfg.SniffBT,
		sniffSNI:  cfg.SniffSNI,
		blockQUIC: cfg.BlockQUIC,
		conns:        newConnTracker(),
		connSem:      semaphore.NewWeighted(int64(maxTunnelConns)),
	}
}

// Start starts the provider, which will call HandleTCP/HandleUDP for each connection.
func (t *ClientTunnel) Start(ctx context.Context) error {
	return t.StartWithHandler(ctx, t)
}

// StartWithHandler starts the provider with a custom ConnectionHandler.
// This allows inserting middleware (e.g. DNS interception) between the
// provider and the tunnel.
func (t *ClientTunnel) StartWithHandler(ctx context.Context, handler provider.ConnectionHandler) error {
	t.ctx, t.cancel = context.WithCancel(ctx)
	return t.provider.Start(t.ctx, handler)
}

// Close drains all handler goroutines by forcefully closing tracked connections.
// It does NOT close provider or dialer — those are owned by the caller (engine).
func (t *ClientTunnel) Close() error {
	if t.cancel != nil {
		t.cancel()
	}
	t.conns.closeAll()

	done := make(chan struct{})
	go func() {
		t.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(closeTimeout):
		slog.Warn("tunnel: Close timed out waiting for goroutines", "timeout", closeTimeout)
	}
	return nil
}

// TrafficBytes returns cumulative TX (local→remote) and RX (remote→local) byte counts.
func (t *ClientTunnel) TrafficBytes() (tx, rx int64) {
	return t.txBytes.Load(), t.rxBytes.Load()
}

// acquireConnSlot reserves a connection slot. On mobile (nonBlockingAdmit=true),
// returns false immediately if at capacity — prevents goroutine pile-up from
// gVisor-spawned waiters. On desktop, blocks until a slot is available or ctx is canceled.
func (t *ClientTunnel) acquireConnSlot() bool {
	if nonBlockingAdmit {
		return t.connSem.TryAcquire(1)
	}
	return t.connSem.Acquire(t.ctx, 1) == nil
}

// HandleTCP processes a TCP connection from the provider.
func (t *ClientTunnel) HandleTCP(conn net.Conn, dest string) {
	if !t.acquireConnSlot() {
		conn.Close()
		return
	}
	defer func() { t.connSem.Release(1); onConnRelease() }()

	t.wg.Add(1)
	defer t.wg.Done()
	defer conn.Close()
	untrack := t.conns.track(conn)
	defer untrack()

	host, port, _ := net.SplitHostPort(dest)
	action := t.route(dest)

	var earlyData []byte // peeked bytes sent as wire early data for PROXY path

	// Single peek for all sniffers (BT + SNI)
	if action != k2rule.TargetReject && (t.sniffBT || t.sniffSNI) {
		if peeked := peekTCP(conn); len(peeked) > 0 {
			earlyData = peeked
			conn = &prefixConn{Conn: conn, reader: io.MultiReader(bytes.NewReader(peeked), conn)}

			// BT: downgrade PROXY → DIRECT
			if t.sniffBT && action == k2rule.TargetProxy {
				if sniff.IsBitTorrent(bytes.NewReader(peeked)) {
					action = k2rule.TargetDirect
					slog.Debug("tunnel: BitTorrent detected, forcing direct", "dest", dest)
				}
			}

			// SNI / HTTP host detection (single parse)
			var sni string
			if t.sniffSNI {
				if port == "443" {
					sni = sniff.SniffTLS(peeked)
				} else {
					sni = sniff.SniffHTTP(peeked)
				}
			}

			// SNI: upgrade DIRECT → PROXY
			if sni != "" && t.router != nil && action == k2rule.TargetDirect {
				if sniAction := t.router.RouteAddr(sni); sniAction == k2rule.TargetProxy {
					action = k2rule.TargetProxy
					dest = net.JoinHostPort(sni, port)
					k2rule.SetTmpRule(host, k2rule.TargetProxy)
					slog.Debug("tunnel: SNI override", "dest", dest, "sni", sni)
				}
			}

			// IPv6→hostname: server may lack IPv6, give it a resolvable hostname
			if sni != "" && action == k2rule.TargetProxy && strings.Contains(host, ":") {
				if newDest := net.JoinHostPort(sni, port); dest != newDest {
					dest = newDest
					slog.Debug("tunnel: IPv6 dest rewrite", "original", host, "sni", sni)
				}
			}
		}
	}

	// IPv6→hostname rewrite (global mode): sniffBT/sniffSNI disabled, but IPv6 PROXY
	// dests still need hostname rewrite so server resolves via Happy Eyeballs
	if !(t.sniffBT || t.sniffSNI) && action == k2rule.TargetProxy && port == "443" && strings.Contains(host, ":") {
		if peeked := peekTCP(conn); len(peeked) > 0 {
			earlyData = peeked
			conn = &prefixConn{Conn: conn, reader: io.MultiReader(bytes.NewReader(peeked), conn)}
			if sni := sniff.SniffTLS(peeked); sni != "" {
				dest = net.JoinHostPort(sni, port)
				slog.Debug("tunnel: IPv6 dest rewrite", "original", host, "sni", sni)
			}
		}
	}

	slog.Debug("tunnel: route", "dest", dest, "action", action)

	switch action {
	case k2rule.TargetDirect:
		t.handleTCPDirect(conn, dest)
	case k2rule.TargetProxy:
		// For PROXY: unwrap prefixConn — peeked bytes go as wire early data,
		// not replayed via pipe (which would double-send them).
		if pc, ok := conn.(*prefixConn); ok && len(earlyData) > 0 {
			conn = pc.Conn
		}
		t.handleTCPProxy(conn, dest, earlyData)
	case k2rule.TargetReject:
		// Silently drop
	}
}

// HandleUDP processes a UDP connection from the provider.
func (t *ClientTunnel) HandleUDP(conn net.PacketConn, dest string) {
	if !t.acquireConnSlot() {
		conn.Close()
		return
	}
	defer func() { t.connSem.Release(1); onConnRelease() }()

	t.wg.Add(1)
	defer t.wg.Done()
	defer conn.Close()
	untrack := t.conns.track(conn)
	defer untrack()

	host, port, _ := net.SplitHostPort(dest)
	action := t.route(dest)

	// Peek first packet for protocol detection + domain extraction.
	// Triggers: sniffBT (BT/WebRTC), sniffSNI (domain upgrade), blockQUIC (QUIC reject).
	if action != k2rule.TargetReject && (t.sniffBT || t.sniffSNI || t.blockQUIC) {
		if packet, addr := peekUDP(conn); packet != nil {
			origConn := conn // save before wrapping — prefixPacketConn hides UDPRejecter
			conn = &prefixPacketConn{PacketConn: conn, first: packet, firstAddr: addr}

			var meta sniff.Result

			// 1. BT/WebRTC: PROXY → DIRECT
			if t.sniffBT && action == k2rule.TargetProxy {
				if sniff.IsUTP(packet) || sniff.IsUDPTracker(packet) {
					meta.Protocol = sniff.ProtoBitTorrent
					action = k2rule.TargetDirect
					slog.Debug("tunnel: BitTorrent UDP detected, forcing direct", "dest", dest)
				} else if sniff.IsSTUN(packet) || sniff.IsDTLS(packet) {
					if sniff.IsSTUN(packet) {
						meta.Protocol = sniff.ProtoSTUN
					} else {
						meta.Protocol = sniff.ProtoDTLS
					}
					action = k2rule.TargetDirect
					slog.Debug("tunnel: WebRTC detected, forcing direct", "dest", dest)
				}
			}

			// 2. QUIC protocol detection (not port-based)
			if meta.Protocol == "" && sniff.IsQUIC(packet) {
				meta.Protocol = sniff.ProtoQUIC
				// Fast path: QUIC already routed to proxy → block immediately.
				// Skips expensive SniffQUIC (HKDF + AES decrypt + ~5KB alloc).
				if t.blockQUIC && action == k2rule.TargetProxy {
					slog.Debug("tunnel: QUIC blocked, sending ICMP unreachable", "dest", dest)
					if r, ok := origConn.(provider.UDPRejecter); ok {
						r.RejectHandshake()
					}
					return
				}
				// Extract domain for routing upgrade or IPv6 hostname rewrite.
				// SniffQUIC is expensive (QUIC Initial decryption), only call when needed.
				needDomain := t.sniffSNI || (action == k2rule.TargetProxy && strings.Contains(host, ":"))
				if needDomain {
					meta.Domain, _ = sniff.SniffQUIC(packet)
				}
			}

			// 3. Domain-based routing: DIRECT → PROXY upgrade
			if t.sniffSNI && meta.Domain != "" && t.router != nil && action == k2rule.TargetDirect {
				if sniAction := t.router.RouteAddr(meta.Domain); sniAction == k2rule.TargetProxy {
					action = k2rule.TargetProxy
					dest = net.JoinHostPort(meta.Domain, port)
					k2rule.SetTmpRule(host, k2rule.TargetProxy)
					slog.Debug("tunnel: QUIC SNI override", "dest", dest, "sni", meta.Domain)
				}
			}

			// 4. Block QUIC going through proxy (prevents double-QUIC tunneling).
			// Browsers fall back to TCP/TLS which uses the tunnel's stream path.
			if t.blockQUIC && meta.Protocol == sniff.ProtoQUIC && action == k2rule.TargetProxy {
				slog.Debug("tunnel: QUIC blocked, sending ICMP unreachable", "dest", dest, "domain", meta.Domain)
				if r, ok := origConn.(provider.UDPRejecter); ok {
					r.RejectHandshake()
				}
				return
			}

			// 5. IPv6 → hostname rewrite (server resolves via Happy Eyeballs)
			if meta.Domain != "" && action == k2rule.TargetProxy && strings.Contains(host, ":") {
				if newDest := net.JoinHostPort(meta.Domain, port); dest != newDest {
					dest = newDest
					slog.Debug("tunnel: IPv6 dest rewrite", "original", host, "sni", meta.Domain)
				}
			}

		}
	}

	switch action {
	case k2rule.TargetDirect:
		t.handleUDPDirect(conn, dest)
	case k2rule.TargetProxy:
		t.handleUDPProxy(conn, dest)
	case k2rule.TargetReject:
		// Silently drop
	}
}

func (t *ClientTunnel) route(dest string) k2rule.Target {
	if t.router == nil {
		return k2rule.TargetProxy
	}
	host, _, err := net.SplitHostPort(dest)
	if err != nil {
		return k2rule.TargetProxy
	}
	return t.router.RouteAddr(host)
}

func (t *ClientTunnel) handleTCPDirect(local net.Conn, dest string) {
	var remote net.Conn
	var err error
	if t.directDialer != nil {
		remote, err = t.directDialer.DialContext(t.ctx, "tcp", dest)
	} else {
		remote, err = net.Dial("tcp", dest)
	}
	if err != nil {
		slog.Warn("tunnel: direct TCP dial failed", "dest", dest, "err", err)
		return
	}
	defer remote.Close()
	untrack := t.conns.track(remote)
	defer untrack()
	pipe(local, remote, &t.txBytes, &t.rxBytes)
}

func (t *ClientTunnel) handleTCPProxy(local net.Conn, dest string, earlyData []byte) {
	remote, err := t.dialer.DialTCP(t.ctx, dest, earlyData)
	if err != nil {
		slog.Warn("tunnel: proxy TCP dial failed", "dest", dest, "err", err)
		if t.wireReporter != nil {
			t.wireReporter.ReportWireError(err)
		}
		return
	}
	if t.wireReporter != nil {
		t.wireReporter.ClearWireError()
	}
	defer remote.Close()
	untrack := t.conns.track(remote)
	defer untrack()
	pipe(local, remote, &t.txBytes, &t.rxBytes)
}

func (t *ClientTunnel) handleUDPDirect(local net.PacketConn, dest string) {
	var remote net.PacketConn
	var err error
	if t.directDialer != nil {
		remote, err = t.directDialer.ListenPacket("udp", "")
	} else {
		remote, err = net.ListenPacket("udp", "")
	}
	if err != nil {
		slog.Warn("tunnel: direct UDP listen failed", "err", err)
		return
	}
	defer remote.Close()
	untrack := t.conns.track(remote)
	defer untrack()

	destAddr, err := net.ResolveUDPAddr("udp", dest)
	if err != nil {
		slog.Warn("tunnel: resolve UDP addr failed", "dest", dest, "err", err)
		return
	}
	pipePacket(local, remote, destAddr, &t.txBytes, &t.rxBytes)
}

func (t *ClientTunnel) handleUDPProxy(local net.PacketConn, dest string) {
	remote, err := t.dialer.DialUDP(t.ctx, dest)
	if err != nil {
		slog.Warn("tunnel: proxy UDP dial failed", "dest", dest, "err", err)
		if t.wireReporter != nil {
			t.wireReporter.ReportWireError(err)
		}
		return
	}
	if t.wireReporter != nil {
		t.wireReporter.ClearWireError()
	}
	defer remote.Close()
	untrack := t.conns.track(remote)
	defer untrack()
	pipePacket(local, remote, nil, &t.txBytes, &t.rxBytes)
}

// Buffer pools to avoid per-connection allocations.
var (
	tcpBufPool  = sync.Pool{New: func() any { b := make([]byte, tcpBufSize); return &b }}
	tcpPeekPool = sync.Pool{New: func() any { b := make([]byte, 4096); return &b }}
	udpBufPool  = sync.Pool{New: func() any { b := make([]byte, 4096); return &b }}
)

// sniffTimeout is the max time to wait for first bytes when sniffing protocol.
// BT clients send handshake immediately, so this is a generous upper bound.
const sniffTimeout = 200 * time.Millisecond

// prefixConn wraps a net.Conn to prepend previously-read bytes.
type prefixConn struct {
	net.Conn
	reader io.Reader
}

func (c *prefixConn) Read(b []byte) (int, error) {
	return c.reader.Read(b)
}

// peekTCP reads first bytes from conn for protocol sniffing.
// Returns the peeked bytes (must be prepended back via prefixConn).
func peekTCP(conn net.Conn) []byte {
	bp := tcpPeekPool.Get().(*[]byte)
	buf := *bp
	conn.SetReadDeadline(time.Now().Add(sniffTimeout))
	n, _ := conn.Read(buf)
	conn.SetReadDeadline(time.Time{})
	if n == 0 {
		tcpPeekPool.Put(bp)
		return nil
	}
	peeked := make([]byte, n)
	copy(peeked, buf[:n])
	tcpPeekPool.Put(bp)
	return peeked
}

// prefixPacketConn wraps a net.PacketConn to replay the first read packet.
type prefixPacketConn struct {
	net.PacketConn
	first     []byte
	firstAddr net.Addr
	replayed  bool
}

func (c *prefixPacketConn) ReadFrom(b []byte) (int, net.Addr, error) {
	if !c.replayed {
		c.replayed = true
		n := copy(b, c.first)
		return n, c.firstAddr, nil
	}
	return c.PacketConn.ReadFrom(b)
}

// peekUDP reads the first UDP packet for protocol sniffing.
// Returns the packet and source addr (must be replayed via prefixPacketConn).
func peekUDP(conn net.PacketConn) (packet []byte, addr net.Addr) {
	bp := udpBufPool.Get().(*[]byte)
	buf := *bp
	defer udpBufPool.Put(bp)
	conn.SetReadDeadline(time.Now().Add(sniffTimeout))
	n, from, err := conn.ReadFrom(buf)
	conn.SetReadDeadline(time.Time{})
	if err != nil || n == 0 {
		return nil, nil
	}
	packet = make([]byte, n)
	copy(packet, buf[:n])
	return packet, from
}

// pipe copies data bidirectionally between two connections.
// Each direction uses CloseWrite (half-close) to send FIN without killing
// the other direction. Full close is the caller's responsibility via defer.
// Optional tx/rx counters accumulate bytes per direction (nil = no counting).
func pipe(a, b net.Conn, tx, rx *atomic.Int64) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		bp := tcpBufPool.Get().(*[]byte)
		n, _ := io.CopyBuffer(b, a, *bp) // a→b = TX
		tcpBufPool.Put(bp)
		if tx != nil {
			tx.Add(n)
		}
		// Half-close: send FIN only, let peer finish sending data.
		// Full close delegated to caller's defer.
		if cw, ok := b.(closeWriter); ok {
			cw.CloseWrite()
		} else {
			b.Close()
		}
	}()
	bp := tcpBufPool.Get().(*[]byte)
	n, _ := io.CopyBuffer(a, b, *bp) // b→a = RX
	tcpBufPool.Put(bp)
	if rx != nil {
		rx.Add(n)
	}
	if cw, ok := a.(closeWriter); ok {
		cw.CloseWrite()
	} else {
		a.Close()
	}
	<-done
}

// pipePacket forwards packets between two PacketConns.
// destAddr is the target for local→remote; if nil, WriteTo uses nil addr.
// Optional tx/rx counters accumulate bytes per direction (nil = no counting).
// On mobile platforms, idle connections are closed after udpIdleTimeoutSec.
//
// Uses 2 goroutines (spawned + caller), matching pipe()'s pattern.
// Each direction's exit closes the peer to unblock the other side.
func pipePacket(local, remote net.PacketConn, destAddr net.Addr, tx, rx *atomic.Int64) {
	var idleTimeout time.Duration
	if udpIdleTimeoutSec > 0 {
		idleTimeout = time.Duration(udpIdleTimeoutSec) * time.Second
	}

	done := make(chan struct{})

	// local → remote (TX) in spawned goroutine
	go func() {
		bp := udpBufPool.Get().(*[]byte)
		buf := *bp
		for {
			if idleTimeout > 0 {
				local.SetReadDeadline(time.Now().Add(idleTimeout))
			}
			n, _, err := local.ReadFrom(buf)
			if err != nil {
				break
			}
			if _, err := remote.WriteTo(buf[:n], destAddr); err != nil {
				break
			}
			if tx != nil {
				tx.Add(int64(n))
			}
		}
		udpBufPool.Put(bp)
		remote.Close() // unblock remote.ReadFrom in caller
		close(done)
	}()

	// remote → local (RX) in caller goroutine
	bp := udpBufPool.Get().(*[]byte)
	buf := *bp
	for {
		if idleTimeout > 0 {
			remote.SetReadDeadline(time.Now().Add(idleTimeout))
		}
		n, from, err := remote.ReadFrom(buf)
		if err != nil {
			if !os.IsTimeout(err) {
				slog.Debug("pipePacket: remote→local ReadFrom done", "err", err)
			}
			break
		}
		if _, err := local.WriteTo(buf[:n], from); err != nil {
			slog.Debug("pipePacket: remote→local WriteTo failed", "from", from, "len", n, "err", err)
			break
		}
		if rx != nil {
			rx.Add(int64(n))
		}
	}
	udpBufPool.Put(bp)
	local.Close() // unblock local.ReadFrom in goroutine
	<-done
}

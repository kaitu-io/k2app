package core

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sasha-s/go-deadlock"

	"github.com/kaitu-io/k2/provider"
	"github.com/kaitu-io/k2/rule"
	"github.com/kaitu-io/k2/sniff"
	"golang.org/x/sync/semaphore"
)

// closeTimeout is the maximum time to wait for handler goroutines to exit
// after all tracked connections have been forcefully closed.
const closeTimeout = 5 * time.Second

// pipeWatchdogGrace bounds how long pipe() will wait for one direction to
// exit after the other has already completed cleanly via CloseWrite().
// The half-close only releases *our* write direction; the peer goroutine
// still blocks on Read until the remote peer voluntarily closes. Some apps
// (long-lived idle push connections, half-broken servers) never do, which
// leaks the tunnel conn slot indefinitely. After this grace period, the
// watchdog force-closes both conns to guarantee the peer goroutine exits.
//
// var (not const) so tests can shorten the grace without changing production
// behavior. Production value only mutated once at init.
var pipeWatchdogGrace = 30 * time.Second

// closeWriter is implemented by connections that support half-close (write-only FIN).
// pipe() uses this on clean EOF exits (preserving the other direction) while
// error exits fall back to full Close() to unblock the peer goroutine.
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

// ClientTunnel connects a Provider to Outbound table, implementing provider.ConnectionHandler.
// It routes each TCP/UDP connection through the Router, then dispatches to the
// matching Outbound (direct, wire, or nil=reject).
type ClientTunnel struct {
	provider        provider.Provider
	outbounds       []Outbound    // indexed by rule.Target value; nil entry = reject
	directTarget    rule.Target   // outbound index for direct (used by sniffing downgrade)
	defaultTarget   rule.Target   // fallback outbound when router is nil
	router          *Router
	processSearcher provider.ProcessSearcher
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

	activeTCP atomic.Int64 // currently active TCP handler goroutines
	activeUDP atomic.Int64 // currently active UDP handler goroutines
}

var _ provider.ConnectionHandler = (*ClientTunnel)(nil)

// ClientTunnelConfig holds dependencies for creating a ClientTunnel.
type ClientTunnelConfig struct {
	Provider        provider.Provider
	Outbounds       []Outbound    // indexed by rule.Target; nil entry = reject
	DirectTarget    rule.Target   // outbound index for direct connections
	DefaultTarget   rule.Target   // fallback outbound when router is nil (zero = direct)
	Router          *Router
	ProcessSearcher provider.ProcessSearcher // optional: nil = no process matching
	SniffBT   bool // enable BitTorrent sniffing (should be false in global mode)
	SniffSNI  bool // enable TLS/QUIC SNI sniffing for DIRECT→PROXY upgrade (should be false in global mode)
	BlockQUIC bool // block QUIC protocol going through proxy
}

// NewClientTunnel creates a new L4 client tunnel.
func NewClientTunnel(cfg ClientTunnelConfig) *ClientTunnel {
	return &ClientTunnel{
		provider:        cfg.Provider,
		outbounds:       cfg.Outbounds,
		directTarget:    cfg.DirectTarget,
		defaultTarget:   cfg.DefaultTarget,
		router:          cfg.Router,
		processSearcher: cfg.ProcessSearcher,
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

// ActiveConns returns the number of currently active TCP and UDP handler goroutines.
func (t *ClientTunnel) ActiveConns() (tcp, udp int64) {
	return t.activeTCP.Load(), t.activeUDP.Load()
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
	t.activeTCP.Add(1)
	defer t.activeTCP.Add(-1)
	defer t.wg.Done()
	defer conn.Close()
	untrack := t.conns.track(conn)
	defer untrack()

	host, port, _ := net.SplitHostPort(dest)

	// Block IPv6 destinations: k2 server doesn't support IPv6,
	// fast-fail so Happy Eyeballs falls back to IPv4.
	if addr, err := netip.ParseAddr(host); err == nil && addr.Is6() {
		slog.Debug("tunnel: reject IPv6 dest", "dest", dest)
		return // defer conn.Close() → gVisor sends RST
	}

	srcAddr := provider.SourceAddr(conn)
	if srcAddr == nil {
		// Proxy mode: conn.RemoteAddr() is the local client source.
		if ta, ok := conn.RemoteAddr().(*net.TCPAddr); ok {
			srcAddr = &net.UDPAddr{IP: ta.IP, Port: ta.Port}
		}
	}
	action, connMeta := t.routeConn(dest, "tcp", srcAddr)

	var earlyData []byte // peeked bytes sent as wire early data for PROXY path

	// Single peek for all sniffers (BT + SNI)
	if t.outbound(action) != nil && (t.sniffBT || t.sniffSNI) {
		if peeked := peekTCP(conn); len(peeked) > 0 {
			earlyData = peeked
			conn = &prefixConn{Conn: conn, reader: io.MultiReader(bytes.NewReader(peeked), conn)}

			// BT: downgrade PROXY → DIRECT
			if t.sniffBT && action != t.directTarget {
				if sniff.IsBitTorrent(bytes.NewReader(peeked)) {
					action = t.directTarget
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

			// SNI: upgrade DIRECT → PROXY, but preserve meta-based bypass.
			// Pass connMeta so a process_name match at routeConn time is honored
			// by the SNI re-route: a Chrome-bypass connection stays direct even
			// when the SNI domain would otherwise route to proxy.
			if sni != "" && t.router != nil && action == t.directTarget {
				if sniAction := t.router.RouteConn(sni, connMeta); sniAction != t.directTarget {
					action = sniAction
					dest = net.JoinHostPort(sni, port)
					t.router.SetTmpRule(host, sniAction)
					slog.Debug("tunnel: SNI override", "dest", dest, "sni", sni)
				}
			}

			// IPv6→hostname: server may lack IPv6, give it a resolvable hostname
			if sni != "" && action != t.directTarget && strings.Contains(host, ":") {
				if newDest := net.JoinHostPort(sni, port); dest != newDest {
					dest = newDest
					slog.Debug("tunnel: IPv6 dest rewrite", "original", host, "sni", sni)
				}
			}
		}
	}

	// IPv6→hostname rewrite (global mode): sniffBT/sniffSNI disabled, but IPv6 PROXY
	// dests still need hostname rewrite so server resolves via Happy Eyeballs
	if !(t.sniffBT || t.sniffSNI) && action != t.directTarget && port == "443" && strings.Contains(host, ":") {
		if peeked := peekTCP(conn); len(peeked) > 0 {
			earlyData = peeked
			conn = &prefixConn{Conn: conn, reader: io.MultiReader(bytes.NewReader(peeked), conn)}
			if sni := sniff.SniffTLS(peeked); sni != "" {
				dest = net.JoinHostPort(sni, port)
				slog.Debug("tunnel: IPv6 dest rewrite", "original", host, "sni", sni)
			}
		}
	}

	slog.Debug("tunnel: route", "dest", dest, "action", action, "srcAddr", srcAddr, "srcIPValid", connMeta.SrcIP.IsValid(), "srcIP", connMeta.SrcIP)

	outbound := t.outbound(action)
	if outbound == nil {
		return // reject: silently drop
	}

	// Wire outbounds send earlyData via wire protocol (separate from conn stream).
	// Direct outbound replays peeked bytes through prefixConn Read.
	pipeConn := conn
	if action != t.directTarget {
		// Peek for early data if not already done (e.g. global mode skips sniffing).
		if len(earlyData) == 0 {
			earlyData = peekTCP(conn)
		}
		// Unwrap prefixConn — peeked bytes go as wire early data,
		// not replayed via pipe (which would double-send them).
		if pc, ok := conn.(*prefixConn); ok && len(earlyData) > 0 {
			pipeConn = pc.Conn
		}
	}

	remote, err := outbound.DialTCP(t.ctx, dest, earlyData)
	if err != nil {
		slog.Warn("tunnel: TCP dial failed", "dest", dest, "outbound", outbound.Name(), "err", err)
		return
	}
	defer remote.Close()
	untrackRemote := t.conns.track(remote)
	defer untrackRemote()
	pipe(pipeConn, remote, &t.txBytes, &t.rxBytes)
}

// HandleUDP processes a UDP connection from the provider.
func (t *ClientTunnel) HandleUDP(conn net.PacketConn, dest string) {
	if !t.acquireConnSlot() {
		conn.Close()
		return
	}
	defer func() { t.connSem.Release(1); onConnRelease() }()

	t.wg.Add(1)
	t.activeUDP.Add(1)
	defer t.activeUDP.Add(-1)
	defer t.wg.Done()
	defer conn.Close()
	untrack := t.conns.track(conn)
	defer untrack()

	host, port, _ := net.SplitHostPort(dest)

	// Drop IPv6 destinations: k2 server doesn't support IPv6.
	// Silent drop lets Happy Eyeballs fall back to IPv4 via timeout.
	// (Sending ICMPv6 Port Unreachable here races against gVisor buffer
	// chunk refcounts and panics under load.)
	if addr, err := netip.ParseAddr(host); err == nil && addr.Is6() {
		slog.Debug("tunnel: drop IPv6 dest", "dest", dest)
		return
	}

	action, connMeta := t.routeConn(dest, "udp", provider.SourcePacketAddr(conn))
	slog.Debug("tunnel: route UDP", "dest", dest, "action", action)

	// Peek first packet for protocol detection + domain extraction.
	// Triggers: sniffBT (BT/WebRTC), sniffSNI (domain upgrade), blockQUIC (QUIC reject).
	if t.outbound(action) != nil && (t.sniffBT || t.sniffSNI || t.blockQUIC) {
		if packet, addr := peekUDP(conn); packet != nil {
			conn = &prefixPacketConn{PacketConn: conn, first: packet, firstAddr: addr}

			var meta sniff.Result

			// 1. BitTorrent: PROXY → DIRECT.
			// STUN/DTLS are detected (so the QUIC sniff below is skipped for a
			// WebRTC first packet) but NOT forced direct. Forcing WebRTC direct
			// sent Telegram/KakaoTalk call setup (STUN/DTLS ICE) straight out
			// the local interface — fatal for users behind the GFW, whose direct
			// egress to a foreign reflector is blocked, so calls never connected
			// while text (TCP, proxied) worked. WebRTC is low-bandwidth and
			// belongs on the tunnel; only BitTorrent needs to stay off it.
			// See support ticket #3051.
			if t.sniffBT && action != t.directTarget {
				pktHead := packet
				if len(pktHead) > 20 {
					pktHead = pktHead[:20]
				}
				slog.Debug("tunnel: UDP sniff peek", "dest", dest, "len", len(packet), "head", hex.EncodeToString(pktHead))

				if sniff.IsSTUN(packet) {
					meta.Protocol = sniff.ProtoSTUN
					slog.Debug("tunnel: STUN detected, keeping proxy route", "dest", dest)
				} else if sniff.IsDTLS(packet) {
					meta.Protocol = sniff.ProtoDTLS
					slog.Debug("tunnel: DTLS detected, keeping proxy route", "dest", dest)
				} else if sniff.IsUTP(packet) || sniff.IsUDPTracker(packet) {
					meta.Protocol = sniff.ProtoBitTorrent
					action = t.directTarget
					slog.Debug("tunnel: BitTorrent UDP detected, forcing direct", "dest", dest)
				}
				if meta.Protocol == "" {
					slog.Debug("tunnel: UDP sniff no match", "dest", dest)
				}
			}

			// 2. QUIC protocol detection (not port-based)
			if meta.Protocol == "" && sniff.IsQUIC(packet) {
				meta.Protocol = sniff.ProtoQUIC
				// Fast path: QUIC already routed to proxy → block immediately.
				// Skips expensive SniffQUIC (HKDF + AES decrypt + ~5KB alloc).
				if t.blockQUIC && action != t.directTarget {
					slog.Debug("tunnel: drop QUIC", "dest", dest)
					return
				}
				// Extract domain for routing upgrade or IPv6 hostname rewrite.
				// SniffQUIC is expensive (QUIC Initial decryption), only call when needed.
				needDomain := t.sniffSNI || (action != t.directTarget && strings.Contains(host, ":"))
				if needDomain {
					meta.Domain, _ = sniff.SniffQUIC(packet)
				}
			}

			// 3. Domain-based routing: DIRECT → PROXY upgrade.
			// Pass connMeta so a process_name bypass at routeConn time is honored
			// — a Chrome-bypass QUIC flow stays direct even when the SNI domain
			// would otherwise route to proxy.
			if t.sniffSNI && meta.Domain != "" && t.router != nil && action == t.directTarget {
				if sniAction := t.router.RouteConn(meta.Domain, connMeta); sniAction != t.directTarget {
					action = sniAction
					dest = net.JoinHostPort(meta.Domain, port)
					t.router.SetTmpRule(host, sniAction)
					slog.Debug("tunnel: QUIC SNI override", "dest", dest, "sni", meta.Domain)
				}
			}

			// 4. Block QUIC going through proxy (prevents double-QUIC tunneling).
			// Browsers fall back to TCP/TLS which uses the tunnel's stream path.
			if t.blockQUIC && meta.Protocol == sniff.ProtoQUIC && action != t.directTarget {
				slog.Debug("tunnel: drop QUIC", "dest", dest, "domain", meta.Domain)
				return
			}

			// 5. IPv6 → hostname rewrite (server resolves via Happy Eyeballs)
			if meta.Domain != "" && action != t.directTarget && strings.Contains(host, ":") {
				if newDest := net.JoinHostPort(meta.Domain, port); dest != newDest {
					dest = newDest
					slog.Debug("tunnel: IPv6 dest rewrite", "original", host, "sni", meta.Domain)
				}
			}

			// 6. Protocol-aware re-route. A protocol rule wins; without one we
			// keep the historical defaults set above: BitTorrent forced direct,
			// STUN and DTLS kept on the tunnel (ticket #3051 — forcing WebRTC
			// direct broke calls for users behind the GFW).
			if meta.Protocol != "" && t.router != nil {
				protoMeta := connMeta
				protoMeta.Protocol = meta.Protocol
				if protoAction, matched := t.router.RouteConnProto(host, protoMeta); matched {
					action = protoAction
					slog.Debug("tunnel: protocol rule override", "dest", dest,
						"protocol", meta.Protocol, "action", action)
				}
			}

		}
	}

	outbound := t.outbound(action)
	if outbound == nil {
		return // reject: silently drop
	}
	remote, destAddr, err := outbound.DialUDP(t.ctx, dest)
	if err != nil {
		slog.Warn("tunnel: UDP dial failed", "dest", dest, "outbound", outbound.Name(), "err", err)
		return
	}
	defer remote.Close()
	untrackRemote := t.conns.track(remote)
	defer untrackRemote()
	pipePacket(conn, remote, destAddr, &t.txBytes, &t.rxBytes)
}

// outbound returns the Outbound for the given target, or nil (reject/out of range).
func (t *ClientTunnel) outbound(target rule.Target) Outbound {
	idx := int(target)
	if idx >= len(t.outbounds) {
		return nil
	}
	return t.outbounds[idx] // nil entry = reject
}

// routeConn builds ConnMeta from source addr + process lookup, then routes.
// Returns both the matched target AND the ConnMeta so callers (sniffer-based
// SNI upgrade) can re-route the connection by domain while preserving the same
// per-connection identity — otherwise a process_name bypass match would be
// silently overridden when the sniffer re-routes by SNI without meta.
func (t *ClientTunnel) routeConn(dest, network string, srcAddr *net.UDPAddr) (rule.Target, rule.ConnMeta) {
	if t.router == nil {
		return t.defaultTarget, rule.ConnMeta{}
	}
	host, portStr, err := net.SplitHostPort(dest)
	if err != nil {
		return t.directTarget, rule.ConnMeta{}
	}

	meta := rule.ConnMeta{Network: network}
	if p, perr := strconv.ParseUint(portStr, 10, 16); perr == nil {
		meta.Port = uint16(p)
	}
	if srcAddr != nil {
		if srcIP, ok := netip.AddrFromSlice(srcAddr.IP); ok {
			// net.IPv4 / net.ParseIP return 16-byte IPv4-mapped IPv6, which makes
			// netip.AddrFromSlice produce an addr whose Is4() reports false. Both
			// Darwin and Linux ProcessSearchers early-return when Is4() is false,
			// so attribution never fires. Unmap() strips the ::ffff: prefix so
			// downstream searchers see a true 4-byte IPv4 addr. Multi-slot gateway
			// routing also keys off this same unmapped address (ConnMeta.SrcIP).
			meta.SrcIP = srcIP.Unmap()
		}
	}
	if t.processSearcher != nil && srcAddr != nil && meta.SrcIP.IsValid() {
		meta.ProcessName, meta.PackageName, meta.InstallerPackage = t.processSearcher.FindProcess(network, meta.SrcIP, uint16(srcAddr.Port))
	}

	return t.router.RouteConn(host, meta), meta
}



// isProxyLayerError returns true if the error originated from the server's
// application layer (the server received the request and responded). This means
// the wire transport is healthy — the error is about the specific target, not
// the transport. Uses the same IsProxyLayer() interface as TransportManager.
func isProxyLayerError(err error) bool {
	var ple interface{ IsProxyLayer() bool }
	return errors.As(err, &ple) && ple.IsProxyLayer()
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
// Returns the packet and dest addr (must be replayed via prefixPacketConn).
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
//
// Exit policy per direction:
//   - Clean EOF (io.CopyBuffer err == nil): the read side sent FIN. Half-close
//     the write side of the peer via CloseWrite() so the other direction can
//     still finish draining (preserves download correctness for apps that
//     shutdown(SHUT_WR) after sending a request).
//   - Error (err != nil): the transport is broken (Read or Write failed).
//     Full-close BOTH conns so the peer goroutine's blocked Read/Write
//     returns immediately instead of leaking until the app voluntarily
//     closes its end. This is the critical fix for the slot-leak regression
//     introduced by commit 2068d24.
//
// Watchdog: even on clean exits, the peer goroutine may still block on Read
// if the remote peer doesn't reciprocate the FIN (long-lived idle services
// are common on macOS). A watchdog goroutine force-closes both conns after
// pipeWatchdogGrace to cap the worst-case leak.
//
// Optional tx/rx counters accumulate bytes per direction (nil = no counting).
func pipe(a, b net.Conn, tx, rx *atomic.Int64) {
	g1done := make(chan struct{})
	g2done := make(chan struct{})
	watchdogDone := make(chan struct{})

	// Watchdog: after the first direction exits, the second has
	// pipeWatchdogGrace to finish. Force-closes both on timeout.
	go func() {
		defer close(watchdogDone)
		pipeWatchdog(a, b, g1done, g2done)
	}()

	// G1 — TX direction: a → b.
	go func() {
		defer close(g1done)
		bp := tcpBufPool.Get().(*[]byte)
		n, err := io.CopyBuffer(b, a, *bp)
		tcpBufPool.Put(bp)
		if tx != nil {
			tx.Add(n)
		}
		pipeDirExit(b, a, err)
	}()

	// G2 — RX direction: b → a (caller goroutine).
	bp := tcpBufPool.Get().(*[]byte)
	n, err := io.CopyBuffer(a, b, *bp)
	tcpBufPool.Put(bp)
	if rx != nil {
		rx.Add(n)
	}
	pipeDirExit(a, b, err)
	close(g2done)

	<-g1done
	// Wait for the watchdog goroutine to exit so callers (including tests
	// that swap pipeWatchdogGrace in defers) don't race its final read of
	// pipeWatchdogGrace. In the normal fast path the watchdog has already
	// returned by this point, so this is effectively free.
	<-watchdogDone
}

// pipeDirExit closes the conns after one direction of a pipe finishes.
//
// dst is where this direction was writing; src is where it was reading.
// On clean EOF (copyErr == nil), only the write side of dst is half-closed so
// the other direction can continue. On error, BOTH conns are fully closed to
// immediately unblock the peer goroutine's in-flight Read/Write.
func pipeDirExit(dst, src net.Conn, copyErr error) {
	if copyErr == nil {
		if cw, ok := dst.(closeWriter); ok {
			cw.CloseWrite()
			return
		}
		dst.Close()
		return
	}
	// Error path: full-close both sides. Without this, a blocked Read on
	// the peer goroutine can leak indefinitely.
	dst.Close()
	src.Close()
}

// pipeWatchdog force-closes both pipe conns if the second direction does not
// exit within pipeWatchdogGrace after the first. This catches the residual
// leak where one direction exited cleanly (half-closed the peer) but the
// peer goroutine remains blocked on Read because the remote peer hasn't
// voluntarily closed its own send direction.
func pipeWatchdog(a, b net.Conn, g1done, g2done <-chan struct{}) {
	// Wait for whichever direction exits first.
	var firstDir string
	var other <-chan struct{}
	select {
	case <-g1done:
		firstDir = "tx"
		other = g2done
	case <-g2done:
		firstDir = "rx"
		other = g1done
	}
	// Grace window for the second direction.
	select {
	case <-other:
		return
	case <-time.After(pipeWatchdogGrace):
		slog.Warn("DIAG: pipe-watchdog",
			"firstExitDir", firstDir,
			"graceMs", pipeWatchdogGrace.Milliseconds(),
		)
		a.Close()
		b.Close()
	}
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

	// The first packet's destination defines the session. It is written by the
	// TX goroutine below and read by the RX loop in the caller goroutine
	// (repairSourceAddr), so it is an atomic handoff rather than a local: a
	// plain variable here is a data race that -race catches.
	//
	// It is stored at most once per session, and the TX loop is careful to
	// keep it that cheap — one allocation for the whole session, none per
	// packet. BenchmarkPipePacketUplink pins that at 0 allocs/op; see the
	// comment on the store below for the trap.
	var primary atomic.Pointer[net.Addr]

	// local → remote (TX) in spawned goroutine
	go func() {
		bp := udpBufPool.Get().(*[]byte)
		buf := *bp
		for {
			if idleTimeout > 0 {
				local.SetReadDeadline(time.Now().Add(idleTimeout))
			}
			n, to, err := local.ReadFrom(buf)
			if err != nil {
				break
			}
			// Packets to the session's first destination keep using destAddr so
			// an outbound that resolved a domain (SNI upgrade, IPv6 rewrite)
			// keeps its hostname label; packets to any OTHER destination are
			// forwarded verbatim. Games open one socket and talk to the game
			// server, the matchmaker and a STUN reflector through it — pinning
			// every packet to the first destination silently misdelivers the
			// rest.
			dst := destAddr
			if to != nil {
				p := primary.Load()
				if p == nil {
					// Copy into a variable declared INSIDE this branch before
					// taking its address. Writing &to instead makes `to` escape,
					// and since `to` is redeclared by the := above on every
					// iteration, that is a 16-byte heap allocation on every
					// packet for a value stored once per session. `first` only
					// escapes on the one iteration that takes this branch.
					first := to
					primary.Store(&first)
					p = &first
				}
				if !sameUDPAddr(to, *p) {
					dst = to
				}
			}
			if _, err := remote.WriteTo(buf[:n], dst); err != nil {
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
		from = repairSourceAddr(from, primary.Load())
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

// repairSourceAddr replaces a downlink source that is not an IP literal with
// the session's primary destination.
//
// A server that predates per-packet source addressing sends every downlink
// frame with addr_len == 0; the wire layer then falls back to the session's
// target string verbatim (quicUDPConn/quicStreamUDPConn/smuxUDPConn ReadFrom).
// When ClientTunnel.handleUDP upgraded that target to a hostname — QUIC SNI
// upgrade or IPv6→hostname rewrite, both of which JoinHostPort a *domain* —
// the fallback is a hostname. sing-tun rejects a non-IP write destination with
// os.ErrInvalid (stack_gvisor_udp.go: `if !destination.IsIP()`), and the RX
// loop treats a WriteTo error as fatal, so a single downlink packet tears down
// the whole session including its uplink. New client + not-yet-upgraded node +
// SNI-upgraded UDP session is exactly the mid-rollout combination, so this is
// not a corner case.
//
// primary is the first destination the app actually addressed, straight off
// the TUN stack, so it is always a real IP:port. Handing that back is also
// more correct than re-resolving the hostname per packet: a fresh DNS answer
// may be an IP the app never wrote to, and the app's connected socket would
// discard the reply.
//
// primary is nil only if a downlink packet beats the session's first uplink
// packet. That cannot happen for a client-initiated session — the server has
// no session to reply on until the first uplink frame arrives — so this is
// unreachable in production. If it ever does happen the address is passed
// through unchanged: failing exactly as before is better than inventing a
// destination the app never addressed.
func repairSourceAddr(from net.Addr, primary *net.Addr) net.Addr {
	if from == nil || primary == nil || isIPAddrPort(from) {
		return from
	}
	return *primary
}

// isIPAddrPort reports whether addr is an IP:port literal rather than a
// hostname:port. The *net.UDPAddr case is handled without String() because
// that allocates, and the RX loop runs it on every downlink packet; the wire
// layer's rawAddr is already a string, so its String() is free.
func isIPAddrPort(addr net.Addr) bool {
	if u, ok := addr.(*net.UDPAddr); ok {
		return u.IP != nil
	}
	_, err := netip.ParseAddrPort(addr.String())
	return err == nil
}

// sameUDPAddr reports whether two addresses denote the same UDP endpoint.
// Compared by string because the two sides can be different concrete types
// (a *net.UDPAddr from the TUN stack vs a wire-layer raw address).
func sameUDPAddr(a, b net.Addr) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.String() == b.String()
}

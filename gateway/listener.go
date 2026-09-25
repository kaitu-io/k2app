//go:build linux

package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/sasha-s/go-deadlock"
)

// Linux socket options not exported by syscall package.
const (
	ipv6Transparent     = 75 // IPV6_TRANSPARENT
	ipv6RecvOrigDstAddr = 74 // IPV6_RECVORIGDSTADDR
)

// tproxyTCPControl sets IP_TRANSPARENT for both IPv4 and IPv6.
func tproxyTCPControl(network, address string, c syscall.RawConn) error {
	return c.Control(func(fd uintptr) {
		syscall.SetsockoptInt(int(fd), syscall.SOL_IP, syscall.IP_TRANSPARENT, 1)
		syscall.SetsockoptInt(int(fd), syscall.SOL_IPV6, ipv6Transparent, 1)
	})
}

// tproxyUDPControl sets IP_TRANSPARENT + IP_RECVORIGDSTADDR for both IPv4 and IPv6.
func tproxyUDPControl(network, address string, c syscall.RawConn) error {
	return c.Control(func(fd uintptr) {
		syscall.SetsockoptInt(int(fd), syscall.SOL_IP, syscall.IP_TRANSPARENT, 1)
		syscall.SetsockoptInt(int(fd), syscall.SOL_IP, syscall.IP_RECVORIGDSTADDR, 1)
		syscall.SetsockoptInt(int(fd), syscall.SOL_IPV6, ipv6Transparent, 1)
		syscall.SetsockoptInt(int(fd), syscall.SOL_IPV6, ipv6RecvOrigDstAddr, 1)
	})
}

func newTPROXYTCPListeners(ctx context.Context, port int) (v4 net.Listener, v6 net.Listener, err error) {
	lc := net.ListenConfig{Control: tproxyTCPControl}
	v4, err = lc.Listen(ctx, "tcp4", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		return nil, nil, fmt.Errorf("tcp4: %w", err)
	}
	v6, err = lc.Listen(ctx, "tcp6", fmt.Sprintf("[::]:%d", port))
	if err != nil {
		v4.Close()
		return nil, nil, fmt.Errorf("tcp6: %w", err)
	}
	return v4, v6, nil
}

func newTPROXYUDPListeners(ctx context.Context, port int) (v4, v6 *net.UDPConn, err error) {
	lc := net.ListenConfig{Control: tproxyUDPControl}
	c4, err := lc.ListenPacket(ctx, "udp4", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		return nil, nil, fmt.Errorf("udp4: %w", err)
	}
	c6, err := lc.ListenPacket(ctx, "udp6", fmt.Sprintf("[::]:%d", port))
	if err != nil {
		c4.Close()
		return nil, nil, fmt.Errorf("udp6: %w", err)
	}
	return c4.(*net.UDPConn), c6.(*net.UDPConn), nil
}

// readUDPOrigDst reads a UDP packet and extracts the original destination
// from the IP_RECVORIGDSTADDR ancillary message.
func readUDPOrigDst(conn *net.UDPConn, buf []byte) (n int, src, dst *net.UDPAddr, err error) {
	oob := make([]byte, 128)
	n, oobn, _, srcAddr, err := conn.ReadMsgUDP(buf, oob)
	if err != nil {
		return 0, nil, nil, err
	}
	msgs, err := syscall.ParseSocketControlMessage(oob[:oobn])
	if err != nil {
		return 0, nil, nil, fmt.Errorf("parse control message: %w", err)
	}
	for _, msg := range msgs {
		if msg.Header.Level == syscall.SOL_IP && msg.Header.Type == syscall.IP_RECVORIGDSTADDR {
			if len(msg.Data) >= 8 {
				port := int(msg.Data[2])<<8 | int(msg.Data[3])
				ip := net.IPv4(msg.Data[4], msg.Data[5], msg.Data[6], msg.Data[7])
				return n, srcAddr, &net.UDPAddr{IP: ip, Port: port}, nil
			}
		}
		if msg.Header.Level == syscall.SOL_IPV6 && msg.Header.Type == ipv6RecvOrigDstAddr {
			if len(msg.Data) >= 28 {
				port := int(msg.Data[2])<<8 | int(msg.Data[3])
				ip := make(net.IP, 16)
				copy(ip, msg.Data[8:24])
				return n, srcAddr, &net.UDPAddr{IP: ip, Port: port}, nil
			}
		}
	}
	return n, srcAddr, srcAddr, nil
}

// udpWriter is the write half of a reply socket.
type udpWriter interface {
	WriteTo(b []byte, addr net.Addr) (int, error)
}

// udpReplier hands out a socket whose packets leave with dst as their source
// address — the original destination the LAN client sent to.
type udpReplier interface {
	replyConn(dst *net.UDPAddr) (udpWriter, error)
}

// transparentBind binds an IP_TRANSPARENT UDP socket to dst, which is not a
// local address: this is how a TPROXY reply is put on the wire with the
// original destination (216.239.35.0:123) as its source. Needs CAP_NET_ADMIN.
func transparentBind(dst *net.UDPAddr) (*net.UDPConn, error) {
	network := "udp4"
	if dst.IP.To4() == nil {
		network = "udp6"
	}
	lc := net.ListenConfig{Control: func(_, _ string, c syscall.RawConn) error {
		var sockErr error
		ctlErr := c.Control(func(fd uintptr) {
			syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
			if network == "udp6" {
				sockErr = syscall.SetsockoptInt(int(fd), syscall.SOL_IPV6, ipv6Transparent, 1)
			} else {
				sockErr = syscall.SetsockoptInt(int(fd), syscall.SOL_IP, syscall.IP_TRANSPARENT, 1)
			}
		})
		if ctlErr != nil {
			return ctlErr
		}
		return sockErr
	}}
	pc, err := lc.ListenPacket(context.Background(), network, dst.String())
	if err != nil {
		return nil, err
	}
	return pc.(*net.UDPConn), nil
}

// replySocketCache keeps one transparent reply socket per original
// destination so a busy flow (NTP poll, QUIC connection, game session) does
// not bind a fresh socket per packet. Entries idle longer than
// replySocketIdle are closed by the sweeper.
type replySocketCache struct {
	bind func(dst *net.UDPAddr) (*net.UDPConn, error)

	mu      deadlock.Mutex
	socks   map[string]*replySocket
	warned  atomic.Bool
	stopped atomic.Bool
}

type replySocket struct {
	conn     *net.UDPConn
	lastUsed atomic.Int64 // unix nanos
}

const (
	replySocketIdle       = 2 * time.Minute
	replySocketSweepEvery = 30 * time.Second
)

func newReplySocketCache(bind func(dst *net.UDPAddr) (*net.UDPConn, error)) *replySocketCache {
	return &replySocketCache{bind: bind, socks: make(map[string]*replySocket)}
}

// replyConn implements udpReplier.
func (c *replySocketCache) replyConn(dst *net.UDPAddr) (udpWriter, error) {
	key := dst.String()
	now := time.Now().UnixNano()

	c.mu.Lock()
	// concurrency:allow map lookup + one bind syscall under lock; bind is
	// local (no network I/O) and serialising it prevents two flows to the
	// same dst from racing to bind the same address.
	defer c.mu.Unlock()
	if c.stopped.Load() {
		return nil, net.ErrClosed
	}
	if s, ok := c.socks[key]; ok {
		s.lastUsed.Store(now)
		return s.conn, nil
	}
	conn, err := c.bind(dst)
	if err != nil {
		return nil, err
	}
	s := &replySocket{conn: conn}
	s.lastUsed.Store(now)
	c.socks[key] = s
	return conn, nil
}

// sweep closes sockets idle for longer than idle.
func (c *replySocketCache) sweep(idle time.Duration) {
	cutoff := time.Now().Add(-idle).UnixNano()
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, s := range c.socks {
		if s.lastUsed.Load() <= cutoff {
			s.conn.Close()
			delete(c.socks, key)
		}
	}
}

func (c *replySocketCache) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.socks)
}

// close shuts every cached socket; later replyConn calls fail with
// net.ErrClosed so a straggling flow falls back to the listener socket.
func (c *replySocketCache) close() {
	c.stopped.Store(true)
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, s := range c.socks {
		s.conn.Close()
		delete(c.socks, key)
	}
}

// runSweeper sweeps idle sockets until done is closed.
func (c *replySocketCache) runSweeper(done <-chan struct{}) {
	t := time.NewTicker(replySocketSweepEvery)
	defer t.Stop()
	for {
		select {
		case <-done:
			return
		case <-t.C:
			c.sweep(replySocketIdle)
		}
	}
}

// tproxyUDPConn wraps a single UDP packet as a net.PacketConn.
type tproxyUDPConn struct {
	conn    *net.UDPConn // listener socket — fallback reply path only
	replier udpReplier
	src     *net.UDPAddr
	dst     *net.UDPAddr
	initial []byte
	read    atomic.Bool
	done    chan struct{}
}

func newTProxyUDPConn(conn *net.UDPConn, replier udpReplier, src, dst *net.UDPAddr, initial []byte) *tproxyUDPConn {
	data := make([]byte, len(initial))
	copy(data, initial)
	return &tproxyUDPConn{
		conn: conn, replier: replier, src: src, dst: dst,
		initial: data, done: make(chan struct{}),
	}
}

func (c *tproxyUDPConn) ReadFrom(b []byte) (int, net.Addr, error) {
	if c.read.CompareAndSwap(false, true) {
		n := copy(b, c.initial)
		return n, c.src, nil
	}
	<-c.done
	return 0, nil, net.ErrClosed
}

// WriteTo sends the response back to the original LAN source address, with
// the original destination as the packet's source (transparent reply
// socket). The addr parameter is intentionally ignored — TPROXY wraps each
// UDP packet as a single-session conn, so all responses go to the
// originating LAN device.
//
// If no transparent socket can be bound the reply still goes out through the
// listener socket, i.e. from <router>:ListenPort. Unconnected client sockets
// accept that; connect()ed ones (NTP, QUIC, games) drop it in the kernel — so
// the fallback is logged once rather than silently taken.
func (c *tproxyUDPConn) WriteTo(b []byte, _ net.Addr) (int, error) {
	if c.replier != nil {
		w, err := c.replier.replyConn(c.dst)
		if err == nil {
			return w.WriteTo(b, c.src)
		}
		if cache, ok := c.replier.(*replySocketCache); ok && cache.warned.CompareAndSwap(false, true) {
			slog.Warn("gateway: transparent reply socket unavailable, replying from listener socket "+
				"(connected-UDP clients will drop these replies)", "dst", c.dst, "err", err)
		}
	}
	return c.conn.WriteTo(b, c.src)
}

func (c *tproxyUDPConn) Close() error {
	select {
	case <-c.done:
	default:
		close(c.done)
	}
	return nil
}

// PacketSourceAddr implements provider.PacketSourcer — lets the tunnel's
// UDP path populate ConnMeta.SrcIP for multi-slot per-slot routing.
func (c *tproxyUDPConn) PacketSourceAddr() *net.UDPAddr { return c.src }

func (c *tproxyUDPConn) LocalAddr() net.Addr                { return c.dst }
func (c *tproxyUDPConn) SetDeadline(t time.Time) error      { return nil }
func (c *tproxyUDPConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *tproxyUDPConn) SetWriteDeadline(t time.Time) error { return nil }

// dnsUDPConn wraps a single REDIRECTed UDP packet for the DNS listener path.
// Unlike tproxyUDPConn this is strictly single-shot: dnsHandler.serveDNS does
// a ReadFrom loop, processes the query, replies via WriteTo, then loops; the
// second ReadFrom returns net.ErrClosed so serveDNS exits cleanly and its
// goroutine doesn't leak. Each incoming packet gets its own dnsUDPConn +
// goroutine, so concurrency is provided at the listener accept layer.
//
// Source-address preservation is automatic: REDIRECT only rewrites the
// destination, so conntrack reverses our reply back to the original sender
// (e.g. dnsmasq, or the LAN client) without any per-packet bookkeeping in
// userspace.
type dnsUDPConn struct {
	conn    *net.UDPConn
	src     net.Addr
	initial []byte
	read    atomic.Bool
	closed  atomic.Bool
}

func newDNSUDPConn(conn *net.UDPConn, src net.Addr, initial []byte) *dnsUDPConn {
	data := make([]byte, len(initial))
	copy(data, initial)
	return &dnsUDPConn{conn: conn, src: src, initial: data}
}

func (c *dnsUDPConn) ReadFrom(b []byte) (int, net.Addr, error) {
	if c.read.CompareAndSwap(false, true) {
		n := copy(b, c.initial)
		return n, c.src, nil
	}
	return 0, nil, net.ErrClosed
}

// WriteTo writes the response back to the original sender. The addr parameter
// is ignored — single-shot conn only ever replies to one peer, the one
// captured at construction.
func (c *dnsUDPConn) WriteTo(b []byte, _ net.Addr) (int, error) {
	if c.closed.Load() {
		return 0, net.ErrClosed
	}
	return c.conn.WriteTo(b, c.src)
}

// PacketSourceAddr implements provider.PacketSourcer — lets the tunnel's
// UDP path populate ConnMeta.SrcIP even for DNS queries forwarded through
// DNSForwardUpstream (multi-slot fail-closed DNS keys off this).
func (c *dnsUDPConn) PacketSourceAddr() *net.UDPAddr {
	ua, _ := c.src.(*net.UDPAddr)
	return ua
}

func (c *dnsUDPConn) Close() error                       { c.closed.Store(true); return nil }
func (c *dnsUDPConn) LocalAddr() net.Addr                { return c.conn.LocalAddr() }
func (c *dnsUDPConn) SetDeadline(t time.Time) error      { return nil }
func (c *dnsUDPConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *dnsUDPConn) SetWriteDeadline(t time.Time) error { return nil }

// newDNSTCPListeners binds plain TCP listeners (no TPROXY socket options) on
// the DNS port for v4 and v6. REDIRECT-delivered packets arrive as normal
// local connections to 127.0.0.1:DNSPort; IP_TRANSPARENT is unnecessary.
func newDNSTCPListeners(ctx context.Context, port int) (v4, v6 net.Listener, err error) {
	var lc net.ListenConfig
	v4, err = lc.Listen(ctx, "tcp4", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		return nil, nil, fmt.Errorf("dns tcp4: %w", err)
	}
	v6, err = lc.Listen(ctx, "tcp6", fmt.Sprintf("[::]:%d", port))
	if err != nil {
		v4.Close()
		return nil, nil, fmt.Errorf("dns tcp6: %w", err)
	}
	return v4, v6, nil
}

func newDNSUDPListeners(ctx context.Context, port int) (v4, v6 *net.UDPConn, err error) {
	var lc net.ListenConfig
	c4, err := lc.ListenPacket(ctx, "udp4", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		return nil, nil, fmt.Errorf("dns udp4: %w", err)
	}
	c6, err := lc.ListenPacket(ctx, "udp6", fmt.Sprintf("[::]:%d", port))
	if err != nil {
		c4.Close()
		return nil, nil, fmt.Errorf("dns udp6: %w", err)
	}
	return c4.(*net.UDPConn), c6.(*net.UDPConn), nil
}

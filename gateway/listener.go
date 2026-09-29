//go:build linux

package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"sync"
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
	var stale []*net.UDPConn
	c.mu.Lock()
	for key, s := range c.socks {
		if s.lastUsed.Load() <= cutoff {
			stale = append(stale, s.conn)
			delete(c.socks, key)
		}
	}
	c.mu.Unlock()
	for _, conn := range stale {
		conn.Close()
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
	socks := c.socks
	c.socks = make(map[string]*replySocket)
	c.mu.Unlock()
	for _, s := range socks {
		s.conn.Close()
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

// udpFlowQueue bounds the uplink packets queued for one flow: enough to ride
// out the tunnel dial and a scheduling stall at a few thousand packets per
// second (a 20 Mbit/s upload overflowed 64 in the lab). When it is full the
// packet is dropped rather than stalling the read loop every other flow
// shares.
const udpFlowQueue = 256

// maxQueuedUDPPackets bounds the packets queued across all flows, so memory
// stays bounded (about 12 MB of full-size packets) even if every flow stalls
// at once, e.g. while the tunnel has no free slot.
const maxQueuedUDPPackets = 8192

// maxUDPFlows bounds the flow table. It matches the tunnel's connection
// budget: flows beyond it could only wait for a slot while their queues fill.
const maxUDPFlows = 4096

// udpFlowKey identifies a flow: one LAN client socket talking to one
// destination. Keying on the destination as well keeps routing, sniffing and
// the reply source per destination, exactly as they were decided per packet
// before flows existed.
type udpFlowKey struct {
	src netip.AddrPort
	dst netip.AddrPort
}

// tproxyUDPConn is one UDP flow — the packets a LAN client socket sends to
// one destination — presented to the tunnel as a net.PacketConn. One flow is
// one tunnel session, so the remote end sees a stable source port for the
// whole exchange and an exchange costs one session, not one per packet.
type tproxyUDPConn struct {
	conn    *net.UDPConn // listener socket — fallback reply path only
	replier udpReplier
	src     *net.UDPAddr
	dst     *net.UDPAddr
	table   *udpFlowTable
	key     udpFlowKey

	readCh    chan []byte
	done      chan struct{}
	closeOnce sync.Once
	rd        readDeadline
	drops     atomic.Int64 // uplink packets dropped because readCh was full
}

var _ net.PacketConn = (*tproxyUDPConn)(nil)

// newTProxyUDPConn creates a flow whose first packet is initial (nil for
// none). The flow is not registered anywhere — udpFlowTable does that.
func newTProxyUDPConn(conn *net.UDPConn, replier udpReplier, src, dst *net.UDPAddr, initial []byte) *tproxyUDPConn {
	c := &tproxyUDPConn{
		conn: conn, replier: replier, src: src, dst: dst,
		readCh: make(chan []byte, udpFlowQueue),
		done:   make(chan struct{}),
	}
	if initial != nil {
		c.deliver(initial)
	}
	return c
}

// deliver queues one uplink packet. It reports false when the flow is closed,
// so the caller can open a fresh flow for the packet; a full queue drops the
// packet and still reports true.
func (c *tproxyUDPConn) deliver(payload []byte) bool {
	if c.closed() {
		return false
	}
	if !c.table.reserve() {
		c.dropped("queue budget exhausted")
		return true
	}
	data := make([]byte, len(payload))
	copy(data, payload)
	select {
	case c.readCh <- data:
	default:
		c.table.release()
		c.dropped("flow queue full")
		return true
	}
	// The flow may have closed while the packet was being queued; nobody
	// will read it then. Report it undelivered — a duplicate is the worst
	// case, and UDP tolerates that better than a loss.
	if c.closed() {
		c.drain()
		return false
	}
	return true
}

// dropped counts an uplink packet that was not queued; the first one per flow
// is logged.
func (c *tproxyUDPConn) dropped(reason string) {
	if c.drops.Add(1) == 1 {
		slog.Warn("gateway: dropping uplink UDP packets", "reason", reason,
			"src", c.src, "dst", c.dst, "queue", udpFlowQueue)
	}
}

func (c *tproxyUDPConn) closed() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

// ReadFrom returns the next uplink packet. The address is the packet's
// destination — what pipePacket forwards to, and what the TUN and SOCKS5
// providers return here too.
func (c *tproxyUDPConn) ReadFrom(b []byte) (int, net.Addr, error) {
	// Fast path: a busy flow always has a packet waiting, so skip the timer.
	select {
	case pkt := <-c.readCh:
		c.table.release()
		return copy(b, pkt), c.dst, nil
	default:
	}
	timerCh, timer, expired := c.rd.makeTimer()
	if expired != nil {
		return 0, nil, expired
	}
	if timer != nil {
		defer timer.Stop()
	}
	select {
	case pkt := <-c.readCh:
		c.table.release()
		return copy(b, pkt), c.dst, nil
	case <-c.done:
		return 0, nil, net.ErrClosed
	case <-timerCh:
		return 0, nil, os.ErrDeadlineExceeded
	}
}

// WriteTo sends the response back to the original LAN source address, with
// the original destination as the packet's source (transparent reply
// socket). The addr parameter is intentionally ignored — a flow has exactly
// one destination, so every response is attributed to it.
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
	c.closeOnce.Do(func() {
		close(c.done)
		if c.table != nil {
			c.table.remove(c.key, c)
		}
		// Give back the budget of packets nobody will read. A packet that
		// slips in after this drain is given back by deliver, which
		// re-checks closed once it has queued.
		c.drain()
	})
	return nil
}

// drain discards the queued packets of a closed flow.
func (c *tproxyUDPConn) drain() {
	for {
		select {
		case <-c.readCh:
			c.table.release()
		default:
			return
		}
	}
}

// PacketSourceAddr implements provider.PacketSourcer — lets the tunnel's
// UDP path populate ConnMeta.SrcIP for multi-slot per-slot routing.
func (c *tproxyUDPConn) PacketSourceAddr() *net.UDPAddr { return c.src }

func (c *tproxyUDPConn) LocalAddr() net.Addr                { return c.dst }
func (c *tproxyUDPConn) SetDeadline(t time.Time) error      { c.rd.set(t); return nil }
func (c *tproxyUDPConn) SetReadDeadline(t time.Time) error  { c.rd.set(t); return nil }
func (c *tproxyUDPConn) SetWriteDeadline(t time.Time) error { return nil }

// readDeadline is a read deadline for a channel-backed PacketConn. The zero
// value means no deadline. Local copy of the provider/wire helper, kept here
// so gateway does not reach into another package's internals.
type readDeadline struct {
	mu deadlock.Mutex
	t  time.Time
}

func (d *readDeadline) set(t time.Time) {
	d.mu.Lock()
	d.t = t
	d.mu.Unlock()
}

func (d *readDeadline) makeTimer() (<-chan time.Time, *time.Timer, error) {
	d.mu.Lock()
	t := d.t
	d.mu.Unlock()
	if t.IsZero() {
		return nil, nil, nil
	}
	dur := time.Until(t)
	if dur <= 0 {
		return nil, nil, os.ErrDeadlineExceeded
	}
	timer := time.NewTimer(dur)
	return timer.C, timer, nil
}

// udpFlowTable maps (LAN client socket, destination) to its live flow, so the
// packets of one exchange share one tunnel session instead of each opening
// its own.
type udpFlowTable struct {
	mu      deadlock.Mutex
	flows   map[udpFlowKey]*tproxyUDPConn
	stopped bool
	refused atomic.Int64 // packets dropped because the table was full
	queued  atomic.Int64 // packets waiting in flow queues, all flows
}

// reserve claims room for one queued packet. A nil table (a flow that was
// never registered) has no budget.
func (t *udpFlowTable) reserve() bool {
	if t == nil {
		return true
	}
	if t.queued.Add(1) > maxQueuedUDPPackets {
		t.queued.Add(-1)
		return false
	}
	return true
}

func (t *udpFlowTable) release() {
	if t != nil {
		t.queued.Add(-1)
	}
}

func newUDPFlowTable() *udpFlowTable {
	return &udpFlowTable{flows: make(map[udpFlowKey]*tproxyUDPConn)}
}

// dispatch hands one uplink packet to its flow, opening the flow (and calling
// start on it, outside the lock) when there is none. A flow that closed
// between lookup and delivery is replaced, so the packet that raced the close
// is not lost.
func (t *udpFlowTable) dispatch(conn *net.UDPConn, replier udpReplier, src, dst *net.UDPAddr, payload []byte, start func(*tproxyUDPConn)) {
	key := newUDPFlowKey(src, dst)
	for attempt := 0; attempt < 2; attempt++ {
		flow, created := t.lookup(key, conn, replier, src, dst)
		if flow == nil {
			return
		}
		if t.feed(flow, created, payload, start) {
			return
		}
	}
}

// feed delivers payload to flow and starts the flow if it was just created.
// It reports false, and takes the flow out of the table, when the flow turned
// out to be closed; such a flow is never started — nobody would close it.
func (t *udpFlowTable) feed(flow *tproxyUDPConn, created bool, payload []byte, start func(*tproxyUDPConn)) bool {
	if !flow.deliver(payload) {
		t.remove(flow.key, flow)
		return false
	}
	if created {
		start(flow)
	}
	return true
}

// lookup returns the live flow for key, creating it when absent. It returns
// nil when the table is stopped or full.
func (t *udpFlowTable) lookup(key udpFlowKey, conn *net.UDPConn, replier udpReplier, src, dst *net.UDPAddr) (flow *tproxyUDPConn, created bool) {
	t.mu.Lock()
	flow, ok := t.flows[key]
	full := !ok && len(t.flows) >= maxUDPFlows
	if t.stopped {
		flow, ok = nil, true
	} else if !ok && !full {
		flow = newTProxyUDPConn(conn, replier, src, dst, nil)
		flow.table, flow.key = t, key
		t.flows[key] = flow
	}
	t.mu.Unlock()

	if full && t.refused.Add(1) == 1 {
		slog.Warn("gateway: UDP flow table full, dropping packets for new flows",
			"flows", maxUDPFlows, "src", src, "dst", dst)
	}
	return flow, !ok && !full
}

// remove drops flow from the table unless a newer flow already took its key.
func (t *udpFlowTable) remove(key udpFlowKey, flow *tproxyUDPConn) {
	t.mu.Lock()
	if t.flows[key] == flow {
		delete(t.flows, key)
	}
	t.mu.Unlock()
}

func (t *udpFlowTable) len() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.flows)
}

// closeAll closes every live flow, unblocking their readers, and refuses new
// ones from then on.
func (t *udpFlowTable) closeAll() {
	t.mu.Lock()
	t.stopped = true
	flows := make([]*tproxyUDPConn, 0, len(t.flows))
	for _, f := range t.flows {
		flows = append(flows, f)
	}
	t.mu.Unlock()
	for _, f := range flows {
		f.Close()
	}
}

// newUDPFlowKey unmaps IPv4-mapped addresses so one socket never yields two
// keys.
func newUDPFlowKey(src, dst *net.UDPAddr) udpFlowKey {
	unmap := func(a *net.UDPAddr) netip.AddrPort {
		ap := a.AddrPort()
		return netip.AddrPortFrom(ap.Addr().Unmap(), ap.Port())
	}
	return udpFlowKey{src: unmap(src), dst: unmap(dst)}
}

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

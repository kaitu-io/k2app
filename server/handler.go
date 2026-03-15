package server

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	k2dns "github.com/kaitu-io/k2/core/dns"
	"github.com/miekg/dns"
	"github.com/sasha-s/go-deadlock"

	"github.com/kaitu-io/k2/wire"
)

// Buffer pools to avoid per-connection allocations.
var (
	tcpBufPool = sync.Pool{New: func() any { b := make([]byte, 32*1024); return &b }}
	udpBufPool = sync.Pool{New: func() any { b := make([]byte, 4096); return &b }}
)

// cgn is the Carrier-Grade NAT range (100.64.0.0/10), not covered by net.IP.IsPrivate().
var cgn = net.IPNet{
	IP:   net.IP{100, 64, 0, 0},
	Mask: net.CIDRMask(10, 32),
}

// isReservedIP returns true if the IP is in a reserved range that should not be
// reachable through the proxy (private, loopback, link-local, CGN, unspecified).
func isReservedIP(ip net.IP) bool {
	return ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() || cgn.Contains(ip)
}

// checkAddr resolves addr (host:port) and returns an error if the target IP is reserved.
func checkAddr(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid address %q: %w", addr, err)
	}

	// Fast path: host is an IP literal.
	if ip := net.ParseIP(host); ip != nil {
		if isReservedIP(ip) {
			return fmt.Errorf("blocked: %s is a reserved IP", host)
		}
		return nil
	}

	// Host is a domain — resolve and check all IPs.
	ips, err := net.DefaultResolver.LookupHost(context.TODO(), host)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", host, err)
	}
	for _, ipStr := range ips {
		if ip := net.ParseIP(ipStr); ip != nil && isReservedIP(ip) {
			return fmt.Errorf("blocked: %s resolves to reserved IP %s", host, ipStr)
		}
	}
	return nil
}

// connTracker tracks active connections for forceful close during shutdown.
type connTracker struct {
	mu    deadlock.Mutex
	conns map[io.Closer]struct{}
}

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

func (ct *connTracker) closeAll() {
	ct.mu.Lock()
	conns := ct.conns
	ct.conns = make(map[io.Closer]struct{})
	ct.mu.Unlock()
	for c := range conns {
		c.Close()
	}
}

// ProxyHandler processes TCP and UDP proxy requests from k2v5 clients.
type ProxyHandler struct {
	DialTimeout    time.Duration
	AllowPrivateIP bool // for testing only; default false blocks reserved IPs
	conns          connTracker
	dnsCache       *k2dns.DNSCache // shared DNS cache for port-53 UDP
}

// NewProxyHandler creates a proxy handler with default settings.
func NewProxyHandler() *ProxyHandler {
	return &ProxyHandler{
		DialTimeout: 5 * time.Second,
		conns:       connTracker{conns: make(map[io.Closer]struct{})},
		dnsCache: k2dns.NewDNSCache(k2dns.DNSCacheConfig{
			MinTTL:     5 * time.Minute,
			MaxTTL:     4 * time.Hour,
			MaxEntries: 10000,
			StaleFor:   30 * time.Minute,
		}),
	}
}

// isDNSPort returns true if addr targets port 53.
func isDNSPort(addr string) bool {
	_, port, err := net.SplitHostPort(addr)
	return err == nil && port == "53"
}

// Close forcefully closes all active connections, unblocking handlers.
func (h *ProxyHandler) Close() {
	h.conns.closeAll()
}

// HandleTCP proxies a TCP stream to the target address.
// Writes a stream response, then pipes data bidirectionally.
func (h *ProxyHandler) HandleTCP(conn net.Conn, addr string) {
	defer conn.Close()
	untrackConn := h.conns.track(conn)
	defer untrackConn()
	slog.Info("server: HandleTCP", "addr", addr)

	if !h.AllowPrivateIP {
		if err := checkAddr(addr); err != nil {
			wire.WriteStreamResponse(conn, &wire.StreamError{Code: 403, Message: err.Error()})
			slog.Error("server: TCP proxy blocked", "addr", addr, "err", err)
			return
		}
	}

	slog.Debug("server: HandleTCP dialing target", "addr", addr)
	dialStart := time.Now()
	target, err := net.DialTimeout("tcp4", addr, h.DialTimeout)
	if err != nil {
		wire.WriteStreamResponse(conn, &wire.StreamError{Code: 502, Message: err.Error()})
		slog.Error("server: TCP proxy dial", "addr", addr, "err", err, "elapsed", time.Since(dialStart))
		return
	}
	defer target.Close()
	untrackTarget := h.conns.track(target)
	defer untrackTarget()
	slog.Debug("server: HandleTCP target connected", "addr", addr, "elapsed", time.Since(dialStart))

	if err := wire.WriteStreamResponse(conn, nil); err != nil {
		slog.Warn("server: HandleTCP write response failed", "addr", addr, "err", err)
		return
	}

	slog.Debug("server: HandleTCP piping", "addr", addr)
	pipe(conn, target)
}

// HandleUDP proxies a UDP session to the target address.
// Implements wire.UDPHandler.
func (h *ProxyHandler) HandleUDP(conn net.PacketConn, addr string) {
	slog.Info("server: HandleUDP start", "addr", addr)
	untrackConn := h.conns.track(conn)
	defer untrackConn()
	if !h.AllowPrivateIP {
		if err := checkAddr(addr); err != nil {
			slog.Error("server: UDP proxy blocked", "addr", addr, "err", err)
			conn.Close()
			return
		}
	}

	// DNS interception: cache responses for port 53 to reduce upstream load.
	if h.dnsCache != nil && isDNSPort(addr) {
		h.handleDNSUDP(conn, addr)
		return
	}

	targetAddr, err := net.ResolveUDPAddr("udp4", addr)
	if err != nil {
		slog.Error("server: UDP resolve", "addr", addr, "err", err)
		conn.Close()
		return
	}

	target, err := net.DialUDP("udp4", nil, targetAddr)
	if err != nil {
		slog.Error("server: UDP dial", "addr", addr, "err", err)
		conn.Close()
		return
	}
	untrackTarget := h.conns.track(target)
	defer untrackTarget()

	done := make(chan struct{})

	// client -> target
	go func() {
		defer close(done)
		defer target.Close() // unblock target→client Read when client is gone
		bp := udpBufPool.Get().(*[]byte)
		buf := *bp
		for {
			n, _, err := conn.ReadFrom(buf)
			if err != nil {
				slog.Debug("server: HandleUDP client->target ReadFrom done", "addr", addr, "err", err)
				break
			}
			slog.Debug("server: HandleUDP client->target forwarding", "addr", addr, "len", n)
			if _, err := target.Write(buf[:n]); err != nil {
				slog.Warn("server: HandleUDP client->target Write failed", "addr", addr, "err", err)
				break
			}
		}
		udpBufPool.Put(bp)
	}()

	// target -> client
	bp := udpBufPool.Get().(*[]byte)
	buf := *bp
	for {
		target.SetReadDeadline(time.Now().Add(60 * time.Second))
		n, err := target.Read(buf)
		if err != nil {
			slog.Debug("server: HandleUDP target->client Read done", "addr", addr, "err", err)
			break
		}
		slog.Debug("server: HandleUDP target->client forwarding", "addr", addr, "len", n)
		if _, err := conn.WriteTo(buf[:n], nil); err != nil {
			slog.Warn("server: HandleUDP target->client WriteTo failed", "addr", addr, "err", err)
			break
		}
	}
	udpBufPool.Put(bp)

	conn.Close()
	target.Close()
	<-done
	slog.Debug("server: HandleUDP done", "addr", addr)
}

// handleDNSUDP handles DNS UDP sessions with transparent caching.
// Cache hit: respond directly without upstream query.
// Cache miss: forward to upstream, cache response, respond to client.
func (h *ProxyHandler) handleDNSUDP(conn net.PacketConn, addr string) {
	defer conn.Close()

	targetAddr, err := net.ResolveUDPAddr("udp4", addr)
	if err != nil {
		slog.Error("server: DNS UDP resolve", "addr", addr, "err", err)
		return
	}

	target, err := net.DialUDP("udp4", nil, targetAddr)
	if err != nil {
		slog.Error("server: DNS UDP dial", "addr", addr, "err", err)
		return
	}
	defer target.Close()
	untrackTarget := h.conns.track(target)
	defer untrackTarget()

	type pendingQuery struct {
		domain  string
		qtype   uint16
		sentAt  time.Time
	}
	var pendingMu sync.Mutex
	pending := make(map[uint16]*pendingQuery)

	// writeToMu serializes writes to conn — cache-hit path (client→target goroutine)
	// and upstream-response path (target→client goroutine) both call conn.WriteTo.
	var writeToMu sync.Mutex

	done := make(chan struct{})

	// client → cache/target
	go func() {
		defer close(done)
		defer target.Close() // unblock target→client Read
		bp := udpBufPool.Get().(*[]byte)
		buf := *bp
		for {
			n, _, err := conn.ReadFrom(buf)
			if err != nil {
				break
			}

			var query dns.Msg
			if err := query.Unpack(buf[:n]); err != nil {
				// Not valid DNS — forward as-is (defensive).
				target.Write(buf[:n])
				continue
			}

			if len(query.Question) == 0 {
				target.Write(buf[:n])
				continue
			}

			domain := query.Question[0].Name
			qtype := query.Question[0].Qtype

			// Cache lookup.
			if packed, found := h.dnsCache.GetPacked(domain, qtype, query.Id); found {
				writeToMu.Lock()
				conn.WriteTo(packed, nil)
				writeToMu.Unlock()
				slog.Info("DIAG: dns-cache-hit", "domain", domain, "qtype", qtype)
				continue
			}

			// Cache miss — track and forward.
			slog.Info("DIAG: dns-cache-miss", "domain", domain, "qtype", qtype, "upstream", addr)
			pendingMu.Lock()
			pending[query.Id] = &pendingQuery{domain: domain, qtype: qtype, sentAt: time.Now()}
			pendingMu.Unlock()

			if _, err := target.Write(buf[:n]); err != nil {
				pendingMu.Lock()
				delete(pending, query.Id)
				pendingMu.Unlock()
				break
			}
		}
		udpBufPool.Put(bp)
	}()

	// target → cache + client
	bp := udpBufPool.Get().(*[]byte)
	buf := *bp
	for {
		target.SetReadDeadline(time.Now().Add(60 * time.Second))
		n, err := target.Read(buf)
		if err != nil {
			break
		}

		var resp dns.Msg
		if err := resp.Unpack(buf[:n]); err == nil {
			pendingMu.Lock()
			pq, ok := pending[resp.Id]
			if ok {
				delete(pending, resp.Id)
			}
			pendingMu.Unlock()

			// Cache response (skip truncated — incomplete data).
			if ok && !resp.Truncated {
				h.dnsCache.Add(pq.domain, pq.qtype, &resp)
				rttMs := time.Since(pq.sentAt).Milliseconds()
				slog.Info("DIAG: dns-upstream-ok", "domain", pq.domain, "qtype", pq.qtype, "rcode", resp.Rcode, "rttMs", rttMs, "upstream", addr)
				if rttMs > 500 {
					slog.Warn("DIAG: dns-upstream-slow", "domain", pq.domain, "qtype", pq.qtype, "rttMs", rttMs, "upstream", addr)
				}
			}
		}

		writeToMu.Lock()
		_, werr := conn.WriteTo(buf[:n], nil)
		writeToMu.Unlock()
		if werr != nil {
			break
		}
	}
	udpBufPool.Put(bp)

	// conn.Close() handled by defer at top of function.
	// Explicit close here would be redundant (wire PacketConn uses sync.Once,
	// so it's safe but unnecessary).
	<-done
	slog.Debug("server: DNS UDP done", "addr", addr)
}

// closeWriter is implemented by connections that support half-close (write-only FIN).
type closeWriter interface {
	CloseWrite() error
}

// pipe copies data bidirectionally between two connections.
// Each direction uses CloseWrite (half-close) to send FIN without killing
// the other direction. Full close is the caller's responsibility via defer.
func pipe(a, b net.Conn) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		bp := tcpBufPool.Get().(*[]byte)
		io.CopyBuffer(b, a, *bp)
		tcpBufPool.Put(bp)
		if cw, ok := b.(closeWriter); ok {
			cw.CloseWrite()
		} else {
			b.Close()
		}
	}()
	bp := tcpBufPool.Get().(*[]byte)
	io.CopyBuffer(a, b, *bp)
	tcpBufPool.Put(bp)
	if cw, ok := a.(closeWriter); ok {
		cw.CloseWrite()
	} else {
		a.Close()
	}
	<-done
}

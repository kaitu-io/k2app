package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
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
func (h *ProxyHandler) checkAddr(addr string) error {
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

	// Host is a domain — resolve via our DNS cache + upstream.
	ips, err := h.resolver.LookupHost(context.TODO(), host)
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

// resolveAddr resolves the host portion of addr if it's a domain name.
// Returns addr unchanged if host is already an IP literal.
func (h *ProxyHandler) resolveAddr(addr string) (string, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr, nil
	}
	if net.ParseIP(host) != nil {
		return addr, nil
	}
	ips, err := h.resolver.LookupHost(context.TODO(), host)
	if err != nil {
		return "", err
	}
	return net.JoinHostPort(ips[0], port), nil
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
	resolver       *Resolver
	dnsUpstream    string // system DNS upstream (default: Docker resolver)
}

// Global DNS servers for server-side domain resolution.
// 3 providers × 2 servers, covering all node regions (HK, JP, SG, US, CA, AU, KR).
var defaultResolverServers = []string{
	"8.8.8.8:53", "8.8.4.4:53",         // Google
	"1.1.1.1:53", "1.0.0.1:53",         // Cloudflare
	"9.9.9.9:53", "149.112.112.112:53", // Quad9
}

// NewProxyHandler creates a proxy handler with default settings.
func NewProxyHandler() *ProxyHandler {
	cache := k2dns.NewDNSCache(k2dns.DNSCacheConfig{
		MinTTL:     5 * time.Minute,
		MaxTTL:     4 * time.Hour,
		MaxEntries: 10000,
		StaleFor:   30 * time.Minute,
	})
	return &ProxyHandler{
		DialTimeout: 5 * time.Second,
		conns:       connTracker{conns: make(map[io.Closer]struct{})},
		dnsCache:    cache,
		resolver:    NewResolver(cache, k2dns.NewDirectDNSClient(defaultResolverServers)),
		dnsUpstream: "127.0.0.11:53",
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
	log := slog.With("remote", conn.RemoteAddr(), "addr", addr)
	defer conn.Close()
	untrackConn := h.conns.track(conn)
	defer untrackConn()
	log.Info("server: HandleTCP")

	if !h.AllowPrivateIP {
		if err := h.checkAddr(addr); err != nil {
			wire.WriteStreamResponse(conn, &wire.StreamError{Code: 403, Message: err.Error()})
			log.Error("server: TCP proxy blocked", "err", err)
			return
		}
	}

	// Resolve domain to IP before dialing (uses shared DNS cache).
	dialAddr, err := h.resolveAddr(addr)
	if err != nil {
		wire.WriteStreamResponse(conn, &wire.StreamError{Code: 502, Message: err.Error()})
		log.Error("server: TCP proxy resolve", "err", err)
		return
	}

	log.Debug("server: HandleTCP dialing target", "resolved", dialAddr)
	dialStart := time.Now()
	target, err := net.DialTimeout("tcp4", dialAddr, h.DialTimeout)
	if err != nil {
		wire.WriteStreamResponse(conn, &wire.StreamError{Code: 502, Message: err.Error()})
		log.Error("server: TCP proxy dial", "err", err, "elapsed", time.Since(dialStart))
		return
	}
	defer target.Close()
	untrackTarget := h.conns.track(target)
	defer untrackTarget()
	log.Debug("server: HandleTCP target connected", "elapsed", time.Since(dialStart))

	if err := wire.WriteStreamResponse(conn, nil); err != nil {
		log.Warn("server: HandleTCP write response failed", "err", err)
		return
	}

	log.Debug("server: HandleTCP piping")
	pipe(conn, target)
}

// HandleUDP proxies a UDP session to the target address.
// Implements wire.UDPHandler.
func (h *ProxyHandler) HandleUDP(conn net.PacketConn, addr string) {
	startTime := time.Now()

	// Use scoped logger from wire layer (carries remote + sid + addr).
	// Falls back to basic logger if conn doesn't implement UDPSessionInfo.
	log := slog.With("addr", addr)
	if info, ok := conn.(wire.UDPSessionInfo); ok {
		log = info.Logger()
	}

	log.Info("server: HandleUDP start")
	untrackConn := h.conns.track(conn)
	defer untrackConn()
	if !h.AllowPrivateIP {
		if err := h.checkAddr(addr); err != nil {
			log.Error("server: UDP proxy blocked", "err", err)
			conn.Close()
			return
		}
	}

	// DNS interception: cache responses for port 53 to reduce upstream load.
	if h.dnsCache != nil && isDNSPort(addr) {
		h.handleDNSUDP(conn, addr, log)
		return
	}

	// Resolve domain to IP before dialing (uses shared DNS cache).
	resolvedAddr, err := h.resolveAddr(addr)
	if err != nil {
		log.Error("server: UDP resolve domain", "err", err)
		conn.Close()
		return
	}

	targetAddr, err := net.ResolveUDPAddr("udp4", resolvedAddr)
	if err != nil {
		log.Error("server: UDP resolve", "resolved", resolvedAddr, "err", err)
		conn.Close()
		return
	}

	target, err := net.DialUDP("udp4", nil, targetAddr)
	if err != nil {
		log.Error("server: UDP dial", "err", err)
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
				log.Debug("server: HandleUDP client->target ReadFrom done", "err", err)
				break
			}
			log.Debug("server: HandleUDP client->target forwarding", "len", n)
			if _, err := target.Write(buf[:n]); err != nil {
				log.Warn("server: HandleUDP client->target Write failed", "err", err)
				break
			}
		}
		udpBufPool.Put(bp)
	}()

	// target -> client
	var exitReason string
	bp := udpBufPool.Get().(*[]byte)
	buf := *bp
	for {
		target.SetReadDeadline(time.Now().Add(60 * time.Second))
		n, err := target.Read(buf)
		if err != nil {
			if errors.Is(err, os.ErrDeadlineExceeded) {
				exitReason = "target-timeout"
			} else {
				exitReason = "target-read-error"
			}
			log.Debug("server: HandleUDP target->client Read done", "err", err)
			break
		}
		log.Debug("server: HandleUDP target->client forwarding", "len", n)
		if _, err := conn.WriteTo(buf[:n], nil); err != nil {
			exitReason = "writeto-fail"
			log.Warn("server: HandleUDP target->client WriteTo failed", "err", err)
			break
		}
	}
	udpBufPool.Put(bp)

	conn.Close()
	target.Close()
	<-done
	log.Info("server: HandleUDP done",
		"exitReason", exitReason, "durationS", int(time.Since(startTime).Seconds()))
}

// handleDNSUDP handles DNS UDP sessions with transparent caching.
// Cache hit: respond directly without upstream query.
// Cache miss: forward to upstream, cache response, respond to client.
func (h *ProxyHandler) handleDNSUDP(conn net.PacketConn, _ string, log *slog.Logger) {
	defer conn.Close()

	// Use system resolver instead of client-specified upstream.
	upstream := h.dnsUpstream
	targetAddr, err := net.ResolveUDPAddr("udp4", upstream)
	if err != nil {
		log.Error("server: DNS UDP resolve", "upstream", upstream, "err", err)
		return
	}

	target, err := net.DialUDP("udp4", nil, targetAddr)
	if err != nil {
		log.Error("server: DNS UDP dial", "upstream", upstream, "err", err)
		return
	}
	defer target.Close()
	untrackTarget := h.conns.track(target)
	defer untrackTarget()

	type pendingQuery struct {
		domain string
		qtype  uint16
		sentAt time.Time
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
				log.Info("DIAG: dns-cache-hit", "domain", domain, "qtype", qtype)
				continue
			}

			// Cache miss — track and forward.
			log.Info("DIAG: dns-cache-miss", "domain", domain, "qtype", qtype, "upstream", upstream)
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
				log.Info("DIAG: dns-upstream-ok", "domain", pq.domain, "qtype", pq.qtype, "rcode", resp.Rcode, "rttMs", rttMs, "upstream", upstream)
				if rttMs > 500 {
					log.Warn("DIAG: dns-upstream-slow", "domain", pq.domain, "qtype", pq.qtype, "rttMs", rttMs, "upstream", upstream)
				}
			}
		}

		writeToMu.Lock()
		_, werr := conn.WriteTo(buf[:n], nil)
		writeToMu.Unlock()
		if werr != nil {
			log.Warn("DIAG: dns-stream-write-fail", "err", werr, "len", n)
			break
		}
		log.Debug("DIAG: dns-stream-write-ok", "len", n)
	}
	udpBufPool.Put(bp)

	// conn.Close() handled by defer at top of function.
	// Explicit close here would be redundant (wire PacketConn uses sync.Once,
	// so it's safe but unnecessary).
	<-done
	log.Debug("server: DNS UDP done")
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

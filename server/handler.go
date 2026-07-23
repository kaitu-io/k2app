package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
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
	resolver       *Resolver       // racing upstream resolver (defaultResolverServers or K2_DNS_SERVERS)
	dnsMaxInflight int             // per-session in-flight DNS query cap for handleDNSUDP

	// Node-level traffic metering (Plan 3-k2s, A1). Cumulative across all
	// sessions since process start or the last ResetTraffic(). Direction
	// convention: rx = uplink read from the client; tx = downlink read from
	// the target. The Center quota uses rx+tx, so exact labeling isn't
	// critical, but the convention is kept consistent across TCP and UDP.
	rxBytes atomic.Int64
	txBytes atomic.Int64

	// Per-device pending deltas (user-level accounting, spec 2026-07-22).
	// Same observation points as rxBytes/txBytes; drained by the device
	// traffic reporter each cycle.
	devTraffic deviceTraffic

	// Accepting gate (Plan 3-k2s, A2). false = refuse NEW connections
	// (Center verdict=stop). MUST default true — see NewProxyHandler.
	// Established connections are never force-killed; stop = refuse new only.
	accepting atomic.Bool
}

// TrafficBytes returns node-level cumulative rx/tx since process start or last reset.
func (h *ProxyHandler) TrafficBytes() (rx, tx int64) { return h.rxBytes.Load(), h.txBytes.Load() }

// ResetTraffic zeroes the counters (sidecar triggers on a new Center epoch).
func (h *ProxyHandler) ResetTraffic() { h.rxBytes.Store(0); h.txBytes.Store(0) }

// TakeDeviceTraffic drains the per-device pending byte deltas.
func (h *ProxyHandler) TakeDeviceTraffic() map[string]DeviceDelta { return h.devTraffic.Take() }

// SetAccepting toggles whether the node accepts NEW connections.
func (h *ProxyHandler) SetAccepting(v bool) { h.accepting.Store(v) }

// Accepting reports whether the node currently accepts NEW connections.
func (h *ProxyHandler) Accepting() bool { return h.accepting.Load() }

// Default DNS upstream servers for server-side resolution.
// 3 providers × 2 servers, covering all node regions (HK, JP, SG, US, CA, AU, KR).
// Operators can override per-node via the K2_DNS_SERVERS env var
// (comma-separated "ip:port,ip:port,..." — port defaults to 53 when omitted).
var defaultResolverServers = []string{
	"8.8.8.8:53", "8.8.4.4:53", // Google
	"1.1.1.1:53", "1.0.0.1:53", // Cloudflare
	"9.9.9.9:53", "149.112.112.112:53", // Quad9
}

// parseResolverList parses a comma-separated "ip:port,ip:port,..." string.
// Port defaults to 53 when omitted. Whitespace around entries is trimmed.
// Returns nil for empty input or all-empty entries.
func parseResolverList(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, _, err := net.SplitHostPort(part); err != nil {
			part = net.JoinHostPort(part, "53")
		}
		out = append(out, part)
	}
	return out
}

// NewProxyHandler creates a proxy handler with default settings.
// Reads K2_DNS_SERVERS env var to override the upstream DNS server list;
// falls back to defaultResolverServers when unset or empty.
func NewProxyHandler() *ProxyHandler {
	cache := k2dns.NewDNSCache(k2dns.DNSCacheConfig{
		MinTTL:     5 * time.Minute,
		MaxTTL:     4 * time.Hour,
		MaxEntries: 10000,
		StaleFor:   30 * time.Minute,
	})

	servers := defaultResolverServers
	if env := os.Getenv("K2_DNS_SERVERS"); env != "" {
		if parsed := parseResolverList(env); len(parsed) > 0 {
			servers = parsed
			slog.Info("DIAG: dns-servers-override", "servers", servers)
		} else {
			slog.Warn("K2_DNS_SERVERS set but empty after parse; using defaults", "raw", env)
		}
	}

	h := &ProxyHandler{
		DialTimeout:    5 * time.Second,
		conns:          connTracker{conns: make(map[io.Closer]struct{})},
		dnsCache:       cache,
		resolver:       NewResolver(cache, k2dns.NewDirectDNSClient(servers)),
		dnsMaxInflight: 32,
	}
	// Default must SERVE: accept new connections until a Center stop verdict.
	h.accepting.Store(true)
	return h
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
	h.pipe(conn, target)
}

// HandleUDP proxies a UDP session to the target address.
// Implements wire.UDPHandler.
func (h *ProxyHandler) HandleUDP(conn net.PacketConn, addr string) {
	// Accepting gate (A2): refuse a NEW UDP session on a Center stop verdict.
	// Established sessions are not force-killed.
	if !h.accepting.Load() {
		conn.Close()
		return
	}

	udid := connDeviceID(conn)
	var sessRx, sessTx int64
	defer func() { h.devTraffic.Add(udid, atomic.LoadInt64(&sessRx), sessTx) }()

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
			h.rxBytes.Add(int64(n))            // uplink read from client (rx)
			atomic.AddInt64(&sessRx, int64(n)) // cross-goroutine write, must be atomic
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
		h.txBytes.Add(int64(n)) // downlink read from target (tx)
		sessTx += int64(n)      // main goroutine only, plain add is fine
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

// handleDNSUDP handles DNS UDP sessions with transparent caching and a
// racing upstream resolver. Per query: cache lookup → on miss, dispatch
// resolver.Client().Query (races top-N healthy upstreams in parallel) →
// cache the response → respond to client.
//
// Concurrent queries within a session run in bounded goroutines (dnsMaxInflight).
// When the session ends, in-flight queries are cancelled via context.
func (h *ProxyHandler) handleDNSUDP(conn net.PacketConn, _ string, log *slog.Logger) {
	defer conn.Close()

	// Per-session context: cancels in-flight resolver.Query when the session ends.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// writeToMu serializes writes to conn across per-query goroutines.
	var writeToMu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, h.dnsMaxInflight)

	bp := udpBufPool.Get().(*[]byte)
	buf := *bp
	defer udpBufPool.Put(bp)

	for {
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			log.Debug("server: DNS UDP read done", "err", err)
			break
		}

		// Copy because buf is reused on the next iteration.
		queryBytes := make([]byte, n)
		copy(queryBytes, buf[:n])

		// Bounded in-flight: blocks the read loop when 32 queries are in flight
		// (natural backpressure — VPN client will retry on its own timeout).
		// Aborts immediately if the session is being torn down.
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			return
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			h.handleSingleDNSQuery(ctx, conn, queryBytes, &writeToMu, log)
		}()
	}

	cancel()
	wg.Wait()
	log.Debug("server: DNS UDP done")
}

// handleSingleDNSQuery processes one DNS query: cache lookup, racing upstream
// on miss, write response back to conn. All writes go through writeToMu.
func (h *ProxyHandler) handleSingleDNSQuery(
	ctx context.Context,
	conn net.PacketConn,
	queryBytes []byte,
	writeToMu *sync.Mutex,
	log *slog.Logger,
) {
	var query dns.Msg
	if err := query.Unpack(queryBytes); err != nil {
		log.Debug("server: DNS UDP unpack failed", "err", err)
		return
	}
	if len(query.Question) == 0 {
		log.Debug("server: DNS UDP no questions")
		return
	}

	domain := query.Question[0].Name
	qtype := query.Question[0].Qtype

	// Cache hit: zero-alloc fast path — pre-packed bytes with ID patched.
	if packed, found := h.dnsCache.GetPacked(domain, qtype, query.Id); found {
		log.Info("DIAG: dns-cache-hit", "domain", domain, "qtype", qtype)
		writeToMu.Lock()
		_, werr := conn.WriteTo(packed, nil)
		writeToMu.Unlock()
		if werr != nil {
			log.Warn("DIAG: dns-stream-write-fail", "err", werr, "len", len(packed))
		}
		return
	}

	log.Info("DIAG: dns-cache-miss", "domain", domain, "qtype", qtype)

	// Cache miss: race upstream via racingResolver inside DirectDNSClient.
	start := time.Now()
	resp, err := h.resolver.Client().Query(ctx, &query)
	rttMs := time.Since(start).Milliseconds()

	if err != nil {
		// racingResolver already tracks per-server health/degradation;
		// this is a session-level fault for the caller (client will retry).
		log.Warn("DIAG: dns-upstream-fail",
			"domain", domain, "qtype", qtype, "rttMs", rttMs, "err", err)
		return
	}

	log.Info("DIAG: dns-upstream-ok",
		"domain", domain, "qtype", qtype, "rcode", resp.Rcode, "rttMs", rttMs)
	if rttMs > 500 {
		log.Warn("DIAG: dns-upstream-slow",
			"domain", domain, "qtype", qtype, "rttMs", rttMs)
	}

	// Cache the response. Add handles negative-cache (NXDOMAIN) internally
	// and skips RFC-illegal TC responses; Query already retries TC over TCP
	// (direct.go:86-97), so resp.Truncated is effectively never set here.
	h.dnsCache.Add(domain, qtype, resp)

	packed, err := resp.Pack()
	if err != nil {
		log.Warn("DIAG: dns-response-pack-fail", "domain", domain, "err", err)
		return
	}

	writeToMu.Lock()
	_, werr := conn.WriteTo(packed, nil)
	writeToMu.Unlock()
	if werr != nil {
		log.Warn("DIAG: dns-stream-write-fail", "err", werr, "len", len(packed))
	}
}

// closeWriter is implemented by connections that support half-close (write-only FIN).
type closeWriter interface {
	CloseWrite() error
}

// pipe copies data bidirectionally between two connections.
// Each direction uses CloseWrite (half-close) to send FIN without killing
// the other direction. Full close is the caller's responsibility via defer.
//
// Direction convention for node-level metering: a is the client side, b is
// the target side. a->b (uplink read from the client) counts as rx; b->a
// (downlink read from the target) counts as tx.
func (h *ProxyHandler) pipe(a, b net.Conn) {
	udid := connDeviceID(a) // a is the client side
	done := make(chan struct{})
	go func() {
		defer close(done)
		bp := tcpBufPool.Get().(*[]byte)
		n, _ := io.CopyBuffer(b, a, *bp) // a->b: uplink (rx)
		h.rxBytes.Add(n)
		h.devTraffic.Add(udid, n, 0)
		tcpBufPool.Put(bp)
		if cw, ok := b.(closeWriter); ok {
			cw.CloseWrite()
		} else {
			b.Close()
		}
	}()
	bp := tcpBufPool.Get().(*[]byte)
	n, _ := io.CopyBuffer(a, b, *bp) // b->a: downlink (tx)
	h.txBytes.Add(n)
	h.devTraffic.Add(udid, 0, n)
	tcpBufPool.Put(bp)
	if cw, ok := a.(closeWriter); ok {
		cw.CloseWrite()
	} else {
		a.Close()
	}
	<-done
}

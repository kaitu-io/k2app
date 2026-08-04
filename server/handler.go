package server

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/netip"
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

// maxUDPDestsPerSession bounds how many distinct destinations one client
// source port may address through a single session. Games and P2P apps use a
// handful (game server + matchmaker + STUN); the cap keeps a hostile or
// runaway client from turning one session into a scanning engine. Once full,
// the least-recently-used destination is evicted to admit a new one (see
// udpDestLRU in HandleUDP) rather than freezing the session's destination set
// — a long-lived game session that rotates peers must keep being able to
// reach new ones.
const maxUDPDestsPerSession = 64

// udpDestNegativeCacheTTL bounds how long a failed destination (blocked by
// policy or unresolvable) stays cached before HandleUDP's uplink loop will
// retry it. Caching failures — not just successes — is what makes
// maxUDPDestsPerSession an actual cap: without it, an attacker addressing N
// distinct unresolvable domains costs N upstream DNS queries and never grows
// the destination table, so the cap never engages (task-4 review C1). The TTL
// keeps a transient upstream hiccup from permanently blackholing a
// destination for the rest of a long session.
const udpDestNegativeCacheTTL = 30 * time.Second

// udpDestResolveTimeout bounds a single destination's checkAddr+resolveAddr
// pipeline in HandleUDP's uplink loop. That loop is shared by every
// destination in the session, so an unbounded resolve (upstream DNS budgets
// up to 5s per query, core/dns/direct.go) serializes in front of every other
// in-flight destination's packets — one black-holed domain stalls a live
// game session by seconds (task-4 review C2). 500ms is generous for any
// resolver that's actually going to answer (LAN/edge resolvers respond in
// tens of ms) while keeping the worst case survivable for real-time traffic.
const udpDestResolveTimeout = 500 * time.Millisecond

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
// ctx bounds the upstream lookup for domain hosts; IP-literal hosts never touch
// the resolver so ctx is irrelevant on that path.
func (h *ProxyHandler) checkAddr(ctx context.Context, addr string) error {
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
	ips, err := h.resolver.LookupHost(ctx, host)
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
// ctx bounds the upstream lookup for domain hosts; IP-literal hosts never touch
// the resolver so ctx is irrelevant on that path.
func (h *ProxyHandler) resolveAddr(ctx context.Context, addr string) (string, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr, nil
	}
	if net.ParseIP(host) != nil {
		return addr, nil
	}
	ips, err := h.resolver.LookupHost(ctx, host)
	if err != nil {
		return "", err
	}
	return net.JoinHostPort(ips[0], port), nil
}

// resolveUDPDest policy-checks and resolves one uplink UDP destination for
// HandleUDP's per-packet dispatch. blocked reports whether a non-nil err came
// from checkAddr (policy) as opposed to resolveAddr/net.ResolveUDPAddr
// (resolution) — callers use it to attribute drops to the right counter.
//
// IP-literal destinations (the common case for game/P2P traffic) never reach
// the resolver: checkAddr and resolveAddr both fast-path IP literals with
// zero network calls, so no explicit bypass is needed here beyond skipping
// the timeout machinery below. Domain destinations are bounded by
// udpDestResolveTimeout — see that constant's doc for why.
func (h *ProxyHandler) resolveUDPDest(key string) (ua *net.UDPAddr, blocked bool, err error) {
	host, _, err := net.SplitHostPort(key)
	if err != nil {
		return nil, false, fmt.Errorf("invalid dest %q: %w", key, err)
	}

	if _, litErr := netip.ParseAddr(host); litErr == nil {
		// IP literal: checkAddr's fast path below is zero-network, so an
		// unbounded context costs nothing here.
		if !h.AllowPrivateIP {
			if err := h.checkAddr(context.Background(), key); err != nil {
				return nil, true, err
			}
		}
		ua, err = net.ResolveUDPAddr("udp4", key)
		return ua, false, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), udpDestResolveTimeout)
	defer cancel()

	if !h.AllowPrivateIP {
		if err := h.checkAddr(ctx, key); err != nil {
			return nil, true, err
		}
	}
	resolved, err := h.resolveAddr(ctx, key)
	if err != nil {
		return nil, false, err
	}
	ua, err = net.ResolveUDPAddr("udp4", resolved)
	return ua, false, err
}

// udpDestEntry caches the outcome of resolveUDPDest for one uplink
// destination within a session. addr is nil for a negative-cache entry
// (checkAddr or resolution failed); blocked distinguishes a policy rejection
// from a resolution failure for the drop counters in HandleUDP.
type udpDestEntry struct {
	addr     *net.UDPAddr
	blocked  bool
	cachedAt time.Time
	lru      *list.Element // this entry's node in HandleUDP's LRU list; Value is the dest key
}

// addressedPeers records the resolved IP:port of every destination a UDP
// session has addressed, so the downlink loop can tell a solicited reply from
// traffic that arrived at the session's egress port unbidden.
//
// The full-cone egress socket is unconnected, so the kernel no longer filters
// unsolicited inbound, and HandleUDP meters every byte it reads from the
// target — including those packets — against the user's device quota. That is
// a deliberate trade (see server/CLAUDE.md, "Per-Device Traffic Accounting"),
// but it must at least be VISIBLE, hence this counter.
//
// Written by the uplink goroutine and read by the downlink loop, so it
// carries its own mutex; the uplink only touches it on a destination-cache
// miss, not per packet. Bounded by the same maxUDPDestsPerSession budget as
// the dests LRU it shadows — entries are added and released in lockstep with
// it, so this introduces no new unbounded memory. The refcount exists because
// two dest keys can resolve to the same peer (a hostname and its IP literal);
// releasing one must not un-address the other.
type addressedPeers struct {
	mu sync.Mutex
	m  map[netip.AddrPort]int
}

func newAddressedPeers() *addressedPeers {
	return &addressedPeers{m: make(map[netip.AddrPort]int, 4)}
}

// normalizeAddrPort strips a 4-in-6 mapping so a destination resolved through
// net.ResolveUDPAddr (which may hand back a 16-byte IPv4) compares equal to a
// source read via ReadFromUDPAddrPort (which yields a 4-byte IPv4 on an AF_INET
// socket).
func normalizeAddrPort(ap netip.AddrPort) netip.AddrPort {
	return netip.AddrPortFrom(ap.Addr().Unmap(), ap.Port())
}

func (p *addressedPeers) add(ua *net.UDPAddr) {
	if ua == nil {
		return
	}
	ap := normalizeAddrPort(ua.AddrPort())
	p.mu.Lock()
	p.m[ap]++
	p.mu.Unlock()
}

func (p *addressedPeers) release(ua *net.UDPAddr) {
	if ua == nil {
		return
	}
	ap := normalizeAddrPort(ua.AddrPort())
	p.mu.Lock()
	if n := p.m[ap]; n > 1 {
		p.m[ap] = n - 1
	} else {
		delete(p.m, ap)
	}
	p.mu.Unlock()
}

// addressed reports whether this session has (still) addressed src. An
// evicted destination reads as unaddressed, so a long-running session that
// cycles through more than maxUDPDestsPerSession peers can over-report. That
// direction is accepted: the alternative is an unbounded set, and the counter
// is a visibility signal, not a security control.
func (p *addressedPeers) addressed(src netip.AddrPort) bool {
	ap := normalizeAddrPort(src)
	p.mu.Lock()
	_, ok := p.m[ap]
	p.mu.Unlock()
	return ok
}

// cachedAddr is a net.Addr with a pre-formatted String(). HandleUDP's
// target->client loop hands the wire layer a net.Addr per downlink packet,
// and the wire layer calls String() on it to embed the source in the
// downlink frame. A *net.UDPAddr's String() reformats from scratch on every
// call; caching the formatted string per source peer (single digits per
// session even under abuse, since full-cone accepts any source) turns that
// into a one-time cost per peer instead of a per-packet one.
type cachedAddr struct {
	network string
	addr    string
}

func (a cachedAddr) Network() string { return a.network }
func (a cachedAddr) String() string  { return a.addr }

// srcEntry is HandleUDP's downlink-loop cache for one source peer: the
// pre-formatted address handed to the wire layer, plus whether this session
// ever addressed that peer. Only the "solicited" verdict is final — see the
// re-check in the downlink loop — so a peer that speaks before we do (ICE,
// STUN) stops being counted as soon as the session addresses it.
type srcEntry struct {
	addr        net.Addr
	unsolicited bool
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
func (h *ProxyHandler) TakeDeviceTraffic() map[DeviceKey]DeviceDelta { return h.devTraffic.Take() }

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
		if err := h.checkAddr(context.Background(), addr); err != nil {
			wire.WriteStreamResponse(conn, &wire.StreamError{Code: 403, Message: err.Error()})
			log.Error("server: TCP proxy blocked", "err", err)
			return
		}
	}

	// Resolve domain to IP before dialing (uses shared DNS cache).
	dialAddr, err := h.resolveAddr(context.Background(), addr)
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

	key := connDeviceKey(conn)
	var sessRx, sessTx int64
	defer func() { h.devTraffic.Add(key, atomic.LoadInt64(&sessRx), sessTx) }()

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
		if err := h.checkAddr(context.Background(), addr); err != nil {
			log.Error("server: UDP proxy blocked", "err", err)
			conn.Close()
			return
		}
	}

	// DNS interception: cache responses for port 53 to reduce upstream load.
	// isDNSPort is evaluated once against the session's initial destination —
	// not per packet. Task 3 made per-packet destinations dest-aware, so a
	// session that starts elsewhere and later addresses a :53 peer will have
	// those later packets forwarded raw through the generic path below
	// (bypassing the cache), not intercepted here. That's accepted: DNS
	// sessions are opened by the client on a dedicated socket, so mixing
	// DNS and non-DNS destinations in one session isn't a real scenario.
	if h.dnsCache != nil && isDNSPort(addr) {
		h.handleDNSUDP(conn, addr, log)
		return
	}

	// Resolve domain to IP before dialing (uses shared DNS cache).
	resolvedAddr, err := h.resolveAddr(context.Background(), addr)
	if err != nil {
		log.Error("server: UDP resolve domain", "err", err)
		conn.Close()
		return
	}

	// Pre-check the session's primary destination so an obviously bad session
	// fails fast; per-packet destinations are checked in the uplink loop.
	if _, err := net.ResolveUDPAddr("udp4", resolvedAddr); err != nil {
		log.Error("server: UDP resolve", "resolved", resolvedAddr, "err", err)
		conn.Close()
		return
	}

	// Unconnected socket: one egress port serves every destination this
	// session addresses, and replies from any peer are accepted. That is
	// endpoint-independent mapping + filtering (full cone), which is what
	// games and ICE need. A connected DialUDP socket would pin the session to
	// its first destination and yield symmetric NAT.
	target, err := net.ListenUDP("udp4", nil)
	if err != nil {
		log.Error("server: UDP listen", "err", err)
		conn.Close()
		return
	}
	untrackTarget := h.conns.track(target)
	defer untrackTarget()

	done := make(chan struct{})
	// Per-session drop/evict counters, written only by the uplink goroutine
	// below and read by the main goroutine in the final log.Info after
	// <-done — atomic to match the sessRx convention just above.
	var sessDestBlocked, sessDestUnresolvable, sessDestEvicted int64
	// The unsolicited-source counters are the exception to the "written only
	// by the uplink goroutine" note above: they are written by the downlink
	// loop (the main goroutine) and read by it, so they need no atomics.
	var sessSrcUnsolicitedPkts, sessSrcUnsolicitedBytes int64
	peers := newAddressedPeers()

	// client -> target
	go func() {
		defer close(done)
		defer target.Close() // unblock target→client Read when client is gone
		bp := udpBufPool.Get().(*[]byte)
		buf := *bp
		// Destination cache, owned by this goroutine (no lock needed): both
		// the map (keyed by dest, for O(1) lookup) and an LRU list (for
		// eviction once maxUDPDestsPerSession is reached) track every
		// distinct destination this session has addressed, successful or
		// not — see udpDestNegativeCacheTTL's doc for why failures must
		// count too.
		dests := make(map[string]*udpDestEntry, 4)
		lru := list.New()
		for {
			n, to, err := conn.ReadFrom(buf)
			if err != nil {
				log.Debug("server: HandleUDP client->target ReadFrom done", "err", err)
				break
			}
			destKey := addr
			if to != nil && to.String() != "" {
				destKey = to.String()
			}

			entry, ok := dests[destKey]
			if ok && entry.addr == nil && time.Since(entry.cachedAt) >= udpDestNegativeCacheTTL {
				// Negative-cache entry expired: drop it and re-resolve below,
				// same as if it had never been seen.
				lru.Remove(entry.lru)
				delete(dests, destKey)
				ok = false
			}

			if !ok {
				if len(dests) >= maxUDPDestsPerSession {
					if oldest := lru.Back(); oldest != nil {
						oldKey := oldest.Value.(string)
						lru.Remove(oldest)
						if old := dests[oldKey]; old != nil {
							peers.release(old.addr)
						}
						delete(dests, oldKey)
						atomic.AddInt64(&sessDestEvicted, 1)
						log.Debug("server: HandleUDP dest evicted for new dest", "evicted", oldKey, "new", destKey)
					}
				}
				ua, blocked, rerr := h.resolveUDPDest(destKey)
				entry = &udpDestEntry{addr: ua, blocked: blocked, cachedAt: time.Now()}
				entry.lru = lru.PushFront(destKey)
				dests[destKey] = entry
				peers.add(ua) // no-op for a negative-cache entry (ua == nil)
				switch {
				case rerr != nil && blocked:
					log.Warn("server: HandleUDP dest blocked", "dest", destKey, "err", rerr)
				case rerr != nil:
					log.Warn("server: HandleUDP dest unresolvable", "dest", destKey, "err", rerr)
				default:
					log.Debug("server: HandleUDP dest resolved", "dest", destKey)
				}
			} else {
				lru.MoveToFront(entry.lru)
			}

			if entry.addr == nil {
				// Dropped: neither counted as rx nor forwarded. This slightly
				// undercounts a hostile session's metered traffic (in the
				// user's favor), but that's an acceptable trade once C1's
				// negative cache is in place — a session can no longer turn
				// this into unbounded upstream DNS load or an unbounded
				// destination table regardless of how the byte counters land.
				if entry.blocked {
					atomic.AddInt64(&sessDestBlocked, 1)
				} else {
					atomic.AddInt64(&sessDestUnresolvable, 1)
				}
				continue
			}

			h.rxBytes.Add(int64(n))            // uplink read from client (rx)
			atomic.AddInt64(&sessRx, int64(n)) // cross-goroutine write, must be atomic
			log.Debug("server: HandleUDP client->target forwarding", "len", n, "dest", destKey)
			if _, err := target.WriteToUDP(buf[:n], entry.addr); err != nil {
				log.Warn("server: HandleUDP client->target Write failed", "dest", destKey, "err", err)
				break
			}
		}
		udpBufPool.Put(bp)
	}()

	// target -> client
	var exitReason string
	bp := udpBufPool.Get().(*[]byte)
	buf := *bp
	// Cache one net.Addr per source peer this loop has already seen. This
	// loop is single-goroutine (no lock needed). Building the address and
	// formatting its String() (which the wire layer calls per packet to
	// embed the source in the downlink frame) costs one allocation per peer
	// instead of one per packet — peer count per session is normally single
	// digits. But full-cone deliberately accepts packets from any source
	// (task-4 review, decided: no source filtering), so a session's egress
	// port is itself an unsolicited-traffic target once an attacker learns
	// or guesses it; without a bound this cache would grow once per distinct
	// attacker source port. Cap it and reset on overflow — this is a perf
	// cache, not a correctness dependency, so dropping it under attack is
	// safe (worst case: one extra allocation per already-seen peer).
	//
	// It also memoizes each peer's solicited/unsolicited verdict so the
	// unsolicited counters below don't take peers' lock on every packet. The
	// overflow clear() only costs a re-derivation — peers stays authoritative
	// — so the verdict survives it.
	srcAddrs := make(map[netip.AddrPort]srcEntry, 4)
	for {
		target.SetReadDeadline(time.Now().Add(60 * time.Second))
		n, srcAP, err := target.ReadFromUDPAddrPort(buf)
		if err != nil {
			if errors.Is(err, os.ErrDeadlineExceeded) {
				exitReason = "target-timeout"
			} else {
				exitReason = "target-read-error"
			}
			log.Debug("server: HandleUDP target->client Read done", "err", err)
			break
		}
		e, ok := srcAddrs[srcAP]
		if !ok {
			if len(srcAddrs) >= maxUDPDestsPerSession {
				clear(srcAddrs)
			}
			e = srcEntry{
				addr:        cachedAddr{network: "udp", addr: net.UDPAddrFromAddrPort(srcAP).String()},
				unsolicited: !peers.addressed(srcAP),
			}
			srcAddrs[srcAP] = e
			if e.unsolicited {
				log.Debug("server: HandleUDP inbound from unaddressed source", "src", e.addr)
			}
		} else if e.unsolicited && peers.addressed(srcAP) {
			// The session has addressed this peer since we last looked. Normal
			// for ICE/STUN, where the remote can legitimately speak first, so
			// the "unsolicited" verdict is re-checked until it clears rather
			// than cached at first sight. Only the SOLICITED verdict is final,
			// which is what keeps peers.addressed off the per-packet path for
			// every well-behaved peer.
			e.unsolicited = false
			srcAddrs[srcAP] = e
		}
		from := e.addr
		if e.unsolicited {
			sessSrcUnsolicitedPkts++
			sessSrcUnsolicitedBytes += int64(n)
		}
		h.txBytes.Add(int64(n)) // downlink read from target (tx)
		sessTx += int64(n)      // main goroutine only, plain add is fine
		log.Debug("server: HandleUDP target->client forwarding", "len", n, "src", from)
		// Carry the true source so the client can demultiplex replies.
		if _, err := conn.WriteTo(buf[:n], from); err != nil {
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
		"exitReason", exitReason, "durationS", int(time.Since(startTime).Seconds()),
		"destsBlocked", atomic.LoadInt64(&sessDestBlocked),
		"destsUnresolvable", atomic.LoadInt64(&sessDestUnresolvable),
		"destsEvicted", atomic.LoadInt64(&sessDestEvicted),
		"srcUnsolicitedPkts", sessSrcUnsolicitedPkts,
		"srcUnsolicitedBytes", sessSrcUnsolicitedBytes)
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
	key := connDeviceKey(a) // a is the client side
	done := make(chan struct{})
	go func() {
		defer close(done)
		bp := tcpBufPool.Get().(*[]byte)
		n, _ := io.CopyBuffer(b, a, *bp) // a->b: uplink (rx)
		h.rxBytes.Add(n)
		h.devTraffic.Add(key, n, 0)
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
	h.devTraffic.Add(key, 0, n)
	tcpBufPool.Put(bp)
	if cw, ok := a.(closeWriter); ok {
		cw.CloseWrite()
	} else {
		a.Close()
	}
	<-done
}

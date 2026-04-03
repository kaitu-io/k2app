package dns

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kaitu-io/k2/core/packet"
	"github.com/kaitu-io/k2/rule"
	"github.com/miekg/dns"
)

const (
	// upstreamTimeout bounds how long a single queryUpstream call may take.
	// Separate from the per-query timeout (30s in dnsHandler) to allow room
	// for stale-fallback and inflight dedup overhead.
	upstreamTimeout = 15 * time.Second

	// prefreshThreshold is the fraction of original TTL remaining below which
	// a cache entry is considered "near expiry" and triggers background refresh.
	prefreshThreshold = 0.25

	// maxConcurrentUpstream limits concurrent upstream DNS queries.
	// Prevents memory spikes when 50+ domains query simultaneously
	// (e.g., after network recovery on iOS where all apps retry at once).
	maxConcurrentUpstream = 8
)

// DNSQuerier is satisfied by DirectDNSClient and ProxyDNSClient.
type DNSQuerier interface {
	Query(ctx context.Context, query *dns.Msg) (*dns.Msg, error)
}

// inflightResult is the shared result of an in-flight upstream DNS query.
// Stores pre-packed bytes so waiters avoid dns.Msg.Copy() + Pack().
type inflightResult struct {
	packed []byte      // pre-packed DNS response (nil on error)
	err    error
	done   chan struct{} // closed when result is ready
}

// Middleware handles DNS query interception and routing
type Middleware struct {
	// Upstream DNS clients
	directDNS DNSQuerier
	proxyDNS  DNSQuerier

	// Caches
	dnsCache *DNSCache // DNS message cache (with negative caching)

	// In-flight dedup: domain+qtype → *inflightResult.
	// Prevents duplicate upstream queries for the same question.
	inflight sync.Map

	// Prefresh dedup: domain:qtype → struct{}, at most one refresh per key.
	prefreshing sync.Map

	// Filters
	hosts  *HostsFile // /etc/hosts lookup
	filter *Filter    // RFC 6761 + noise filtering

	// Metrics (nil-safe — all methods handle nil receiver)
	metrics *Metrics

	// Rule functions (injected by engine)
	matchRule  func(string) rule.Target
	setTmpRule func(string, rule.Target)

	// Writer for sending raw IP packet responses (L3 mode, nil for L4)
	writer io.Writer

	// Engine root context for background prefresh (nil disables prefresh)
	engineCtx context.Context

	// Epoch-based cancellation for in-flight upstream queries.
	// CancelInflight() cancels epochCtx, causing all pending upstream queries
	// to abort immediately (stale sockets released). A new epoch starts.
	epochMu     sync.Mutex
	epochCtx    context.Context
	epochCancel context.CancelFunc

	// querySem limits concurrent upstream DNS queries.
	// Prevents memory spikes when 50+ domains query simultaneously
	// (e.g., after network recovery on iOS).
	querySem chan struct{}
}

// MiddlewareConfig contains configuration for DNS middleware
type MiddlewareConfig struct {
	DirectDNS DNSQuerier
	ProxyDNS  DNSQuerier
	DNSCache  *DNSCache
	Hosts     *HostsFile
	Filter    *Filter
	Metrics    *Metrics
	MatchRule  func(string) rule.Target
	SetTmpRule func(string, rule.Target)
	Writer     io.Writer       // nil for L4-only usage
	EngineCtx  context.Context // engine root context for background prefresh (nil disables)
}

// NewMiddleware creates a new DNS middleware
func NewMiddleware(cfg MiddlewareConfig) *Middleware {
	m := &Middleware{
		directDNS:  cfg.DirectDNS,
		proxyDNS:   cfg.ProxyDNS,
		dnsCache:   cfg.DNSCache,
		hosts:      cfg.Hosts,
		filter:     cfg.Filter,
		metrics:    cfg.Metrics,
		matchRule:  cfg.MatchRule,
		setTmpRule: cfg.SetTmpRule,
		writer:     cfg.Writer,
		engineCtx:  cfg.EngineCtx,
		querySem:   make(chan struct{}, maxConcurrentUpstream),
	}
	if cfg.EngineCtx != nil {
		m.epochCtx, m.epochCancel = context.WithCancel(cfg.EngineCtx)
	}
	return m
}

// CancelInflight cancels all pending upstream DNS queries and starts a new epoch.
// Called on network change — stale queries hold old UDP sockets that will never
// receive responses. Cancelling them releases memory immediately instead of
// waiting for 5-15s timeouts.
func (m *Middleware) CancelInflight() {
	m.epochMu.Lock()
	defer m.epochMu.Unlock()
	if m.epochCancel != nil {
		m.epochCancel()
	}
	if m.engineCtx != nil {
		m.epochCtx, m.epochCancel = context.WithCancel(m.engineCtx)
	}
}

// HandleQuery processes raw DNS query bytes through the full pipeline.
// Returns raw DNS response bytes. This is the L4-friendly entry point.
func (m *Middleware) HandleQuery(ctx context.Context, query []byte) ([]byte, error) {
	// Track total queries
	m.metrics.IncrTotal()

	// 1. Parse DNS query
	var msg dns.Msg
	if err := msg.Unpack(query); err != nil {
		m.metrics.IncrError()
		return nil, fmt.Errorf("unpack DNS query: %w", err)
	}

	// 2-12. Full pipeline (hosts, filter, cache, rule match, upstream, SetTmpRule)
	resp, packed, err := m.processQuery(ctx, &msg)
	if err != nil {
		return nil, err
	}

	// Fast path: cache hit or inflight dedup returned pre-packed bytes.
	if packed != nil {
		return packed, nil
	}

	if resp == nil {
		return nil, fmt.Errorf("no DNS response")
	}

	// 13. Pack response
	return resp.Pack()
}

// processQuery runs the DNS pipeline on a parsed message.
// Returns (resp, nil, nil) for non-hot paths (HandleQuery will Pack),
// or (nil, packed, nil) for cache hits and inflight dedup (pre-packed bytes).
func (m *Middleware) processQuery(ctx context.Context, query *dns.Msg) (*dns.Msg, []byte, error) {
	// 1. Extract domain from query
	domain := extractQueryDomain(query)
	if domain == "" {
		m.metrics.IncrError()
		return nil, nil, fmt.Errorf("no domain in DNS query")
	}

	qtype := extractQueryType(query)
	slog.Debug("DNS query", "domain", domain, "type", qtype)

	// 2. Check hosts file first (OS behavior)
	if m.hosts != nil {
		if ips, found := m.hosts.Lookup(domain); found {
			m.metrics.IncrHostsHit()
			return m.createHostsResponse(query, ips), nil, nil
		}
	}

	// 3. Check RFC 6761 special-use domains
	if m.filter != nil && m.filter.IsSpecialUse(domain) {
		m.metrics.IncrSpecialUseHit()
		slog.Debug("DNS special-use filtered", "domain", domain)
		return m.filter.HandleSpecialUse(query), nil, nil
	}

	// 4. Check local domains (.local, .lan, etc.)
	if m.filter != nil && m.filter.IsLocalDomain(domain) {
		m.metrics.IncrLocalDomainHit()
		slog.Debug("DNS local domain NXDOMAIN", "domain", domain)
		return m.filter.CreateNXDOMAIN(query), nil, nil
	}

	// 5. Noise filtering (PTR, AAAA, WPAD)
	if m.filter != nil {
		if isNoise, noiseResp := m.filter.TryFilterNoise(query); isNoise {
			m.metrics.IncrNoiseFiltered()
			slog.Debug("DNS noise filtered", "domain", domain)
			return noiseResp, nil, nil
		}
	}

	// 6. Check DNS message cache — fast path returns pre-packed bytes.
	// Active SWR: if near expiry, return cache AND trigger background refresh.
	if m.dnsCache != nil {
		packed, nearExpiry := m.dnsCache.GetPackedNearExpiry(domain, query.Question[0].Qtype, query.Id, prefreshThreshold)
		if packed != nil {
			if nearExpiry {
				// Lazy rule match — only on near-expiry, not every cache hit.
				action := m.matchRule(domain)
				if action != rule.TargetReject {
					m.triggerPrefresh(domain, query.Question[0].Qtype, action)
				}
			}
			m.metrics.IncrMessageCacheHit()
			return nil, packed, nil
		}
	}

	// 6b. Pre-fetch stale entry for fallback on upstream failure (RFC 8767).
	var stalePacked []byte
	if m.dnsCache != nil {
		stalePacked, _ = m.dnsCache.GetStalePacked(domain, query.Question[0].Qtype, query.Id)
	}

	// 7. Domain-based routing
	action := m.matchRule(domain)

	// Reject is instant — no upstream query, no dedup needed.
	if action == rule.TargetReject {
		slog.Debug("DNS routed reject", "domain", domain)
		return createNXDOMAIN(query), nil, nil
	}

	// 8. Query upstream DNS with in-flight deduplication.
	// If another goroutine is already querying the same domain+qtype,
	// we wait for its result instead of issuing a duplicate query.
	inflightKey := domain + ":" + strconv.FormatUint(uint64(query.Question[0].Qtype), 10)

	result := &inflightResult{done: make(chan struct{})}
	if existing, loaded := m.inflight.LoadOrStore(inflightKey, result); loaded {
		// Another goroutine is already querying — wait for its result.
		existing := existing.(*inflightResult)
		select {
		case <-existing.done:
			if existing.err != nil {
				return m.returnStaleOrError(stalePacked, existing.err)
			}
			// Copy packed bytes and patch caller's query ID.
			packed := cloneAndPatchID(existing.packed, query.Id)
			return nil, packed, nil
		case <-ctx.Done():
			// Parent cancelled (shutdown/timeout) — return stale or error (sing-box pattern).
			// The owner is still running and will broadcast when done.
			return m.returnStaleOrError(stalePacked, ctx.Err())
		}
	}

	// We own this query — execute and broadcast result.
	// defer guarantees cleanup even on panic (sing-box pattern).
	slog.Debug("DNS inflight owner start", "domain", domain, "key", inflightKey)
	defer func() {
		m.inflight.Delete(inflightKey)
		close(result.done)
	}()

	resp, err := m.queryUpstream(ctx, domain, action, query)
	if err != nil {
		slog.Debug("DNS inflight owner failed", "domain", domain, "key", inflightKey, "err", err)
		result.err = err
		return m.returnStaleOrError(stalePacked, err)
	}

	packed, packErr := resp.Pack()
	if packErr != nil {
		result.err = packErr
		return nil, nil, packErr
	}

	// Broadcast packed bytes to waiters.
	slog.Debug("DNS inflight owner done", "domain", domain, "key", inflightKey)
	result.packed = packed

	// 9. Cache DNS message (uses *dns.Msg for TTL optimization, packs internally)
	if m.dnsCache != nil {
		m.dnsCache.Add(domain, query.Question[0].Qtype, resp)
	}

	// 10. Store DNS-learned IP routes as temporary rules.
	// Skip private IPs — ip_is_private routes must evaluate fresh at connection time.
	if resp.Rcode == dns.RcodeSuccess {
		for _, ip := range extractIPsFromResponse(resp) {
			if addr, err := netip.ParseAddr(ip); err == nil &&
				(addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast()) {
				continue
			}
			m.setTmpRule(ip, action)
		}
	}

	return nil, packed, nil
}

// triggerPrefresh launches a background goroutine to refresh a near-expiry cache entry.
// Deduped via prefreshing sync.Map — at most one refresh per domain:qtype at a time.
// Uses engineCtx (not caller's ctx) so the refresh outlives the triggering query.
func (m *Middleware) triggerPrefresh(domain string, qtype uint16, action rule.Target) {
	if m.engineCtx == nil {
		return
	}

	key := domain + ":" + strconv.FormatUint(uint64(qtype), 10)
	if _, loaded := m.prefreshing.LoadOrStore(key, struct{}{}); loaded {
		return // already prefreshing this domain:qtype
	}

	m.metrics.IncrPrefreshTriggered()

	// Build a synthetic query for upstream.
	query := new(dns.Msg)
	query.SetQuestion(dns.Fqdn(domain), qtype)

	go func() {
		defer m.prefreshing.Delete(key)
		defer func() {
			if r := recover(); r != nil {
				slog.Error("DNS prefresh panic", "domain", domain, "panic", fmt.Sprint(r))
			}
		}()

		// queryUpstream applies its own upstreamTimeout internally.
		resp, err := m.queryUpstream(m.engineCtx, domain, action, query)
		if err != nil {
			slog.Debug("DNS prefresh failed", "domain", domain, "err", err)
			return
		}

		// Update cache with fresh response.
		if m.dnsCache != nil {
			m.dnsCache.Add(domain, qtype, resp)
		}

		// Update temporary routing rules.
		// Skip private IPs — ip_is_private routes must evaluate fresh at connection time.
		if resp.Rcode == dns.RcodeSuccess {
			for _, ip := range extractIPsFromResponse(resp) {
				if addr, err := netip.ParseAddr(ip); err == nil &&
					(addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast()) {
					continue
				}
				m.setTmpRule(ip, action)
			}
		}

		slog.Debug("DNS prefresh ok", "domain", domain)
	}()
}

// returnStaleOrError returns stale cached data if available, otherwise the error.
func (m *Middleware) returnStaleOrError(stalePacked []byte, err error) (*dns.Msg, []byte, error) {
	if stalePacked != nil {
		m.metrics.IncrStaleCacheHit()
		return nil, stalePacked, nil
	}
	return nil, nil, err
}

// queryUpstream sends a DNS query to the appropriate upstream (direct or proxy).
func (m *Middleware) queryUpstream(ctx context.Context, domain string, action rule.Target, query *dns.Msg) (*dns.Msg, error) {
	// Derive from epoch context — CancelInflight() aborts all pending queries.
	m.epochMu.Lock()
	epochCtx := m.epochCtx
	m.epochMu.Unlock()

	if epochCtx != nil {
		qctx, qcancel := context.WithTimeout(epochCtx, upstreamTimeout)
		defer qcancel()
		// Also cancel if caller's context is done (engine shutdown).
		stop := context.AfterFunc(ctx, qcancel)
		defer stop()
		ctx = qctx
	} else {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, upstreamTimeout)
		defer cancel()
	}

	// Limit concurrent upstream queries — prevents memory spikes from
	// burst DNS (e.g., 50+ domains after iOS network recovery).
	select {
	case m.querySem <- struct{}{}:
		defer func() { <-m.querySem }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	start := time.Now()

	switch action {
	case rule.TargetDirect:
		m.metrics.IncrDirect()
		resp, err := m.directDNS.Query(ctx, query)
		elapsed := time.Since(start)
		if err != nil {
			m.metrics.IncrError()
			slog.Warn("DNS direct query failed", "domain", domain, "err", err)
			slog.Warn("DIAG: dns-fail", "domain", domain, "upstream", "direct", "err", err)
			return nil, err
		}
		if elapsed > 500*time.Millisecond {
			slog.Info("DIAG: dns-slow", "domain", domain, "upstream", "direct", "latencyMs", elapsed.Milliseconds())
		}
		slog.Debug("DNS routed direct", "domain", domain)
		return resp, nil

	case rule.TargetProxy:
		m.metrics.IncrProxy()
		resp, err := m.proxyDNS.Query(ctx, query)
		elapsed := time.Since(start)
		if err != nil {
			// No fallback to direct DNS — poisoned results in censored networks
			// cause certificate errors and security violations. (2026-03-08 incident)
			m.metrics.IncrError()
			slog.Warn("DIAG: dns-fail", "domain", domain, "upstream", "proxy", "err", err)
			return nil, fmt.Errorf("DNS proxy query failed for %s: %w", domain, err)
		}
		if elapsed > 500*time.Millisecond {
			slog.Info("DIAG: dns-slow", "domain", domain, "upstream", "proxy", "latencyMs", elapsed.Milliseconds())
		}
		slog.Debug("DNS routed proxy", "domain", domain)
		return resp, nil

	default:
		return nil, fmt.Errorf("unexpected routing action: %d", action)
	}
}

// HandleDNSQuery processes a DNS query from L3 packets (TUN device).
// Extracts UDP payload, runs the pipeline via HandleQuery, wraps response in IP packet.
func (m *Middleware) HandleDNSQuery(ctx context.Context, pkt []byte, info packet.Info) error {
	// 1. Extract UDP payload
	udpPayload, err := packet.ExtractUDPPayload(pkt)
	if err != nil {
		m.metrics.IncrTotal()
		m.metrics.IncrError()
		return fmt.Errorf("extract UDP payload: %w", err)
	}

	// 2-12. Run full pipeline via HandleQuery
	respBytes, err := m.HandleQuery(ctx, udpPayload)
	if err != nil {
		return err
	}

	// 13. Build UDP/IP response packet (swap src/dst)
	respPacket, err := packet.BuildUDPIP(
		info.DstIP,   // DNS server IP becomes source
		info.SrcIP,   // Client IP becomes destination
		info.DstPort, // Port 53
		info.SrcPort, // Client's source port
		respBytes,
	)
	if err != nil {
		return fmt.Errorf("build DNS response packet: %w", err)
	}

	// Write response back to TUN device
	if _, err := m.writer.Write(respPacket); err != nil {
		return fmt.Errorf("write DNS response: %w", err)
	}

	return nil
}

// createHostsResponse creates a DNS response from hosts file IPs
func (m *Middleware) createHostsResponse(query *dns.Msg, ips []net.IP) *dns.Msg {
	resp := new(dns.Msg)
	resp.SetReply(query)

	if len(query.Question) == 0 {
		return resp
	}

	q := query.Question[0]

	// Add A records for IPv4 addresses
	if q.Qtype == dns.TypeA || q.Qtype == dns.TypeANY {
		for _, ip := range ips {
			if ip.To4() != nil {
				resp.Answer = append(resp.Answer, &dns.A{
					Hdr: dns.RR_Header{
						Name:   q.Name,
						Rrtype: dns.TypeA,
						Class:  dns.ClassINET,
						Ttl:    300,
					},
					A: ip.To4(),
				})
			}
		}
	}

	// Add AAAA records for IPv6 addresses
	if q.Qtype == dns.TypeAAAA || q.Qtype == dns.TypeANY {
		for _, ip := range ips {
			if ip.To4() == nil && ip.To16() != nil {
				resp.Answer = append(resp.Answer, &dns.AAAA{
					Hdr: dns.RR_Header{
						Name:   q.Name,
						Rrtype: dns.TypeAAAA,
						Class:  dns.ClassINET,
						Ttl:    300,
					},
					AAAA: ip.To16(),
				})
			}
		}
	}

	return resp
}

// extractQueryDomain extracts the domain name from a DNS query
func extractQueryDomain(query *dns.Msg) string {
	if len(query.Question) == 0 {
		return ""
	}

	domain := query.Question[0].Name
	// Remove trailing dot
	return strings.TrimSuffix(domain, ".")
}

// extractQueryType extracts the query type (A, AAAA, etc.) as a string
func extractQueryType(query *dns.Msg) string {
	if len(query.Question) == 0 {
		return "unknown"
	}

	qtype := query.Question[0].Qtype
	switch qtype {
	case dns.TypeA:
		return "A"
	case dns.TypeAAAA:
		return "AAAA"
	case dns.TypeCNAME:
		return "CNAME"
	case dns.TypeMX:
		return "MX"
	case dns.TypeTXT:
		return "TXT"
	default:
		return fmt.Sprintf("Type%d", qtype)
	}
}

// extractIPsFromResponse extracts all IP addresses from a DNS response
func extractIPsFromResponse(resp *dns.Msg) []string {
	ips := make([]string, 0, len(resp.Answer))

	for _, answer := range resp.Answer {
		switch rr := answer.(type) {
		case *dns.A:
			if rr.A != nil {
				ips = append(ips, rr.A.String())
			}
		case *dns.AAAA:
			if rr.AAAA != nil {
				ips = append(ips, rr.AAAA.String())
			}
		}
	}

	return ips
}

// createNXDOMAIN creates a DNS NXDOMAIN response
func createNXDOMAIN(query *dns.Msg) *dns.Msg {
	resp := new(dns.Msg)
	resp.SetReply(query)
	resp.Rcode = dns.RcodeNameError
	return resp
}

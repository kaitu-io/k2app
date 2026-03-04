package dns

import (
	"fmt"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kaitu-io/k2rule"
	"github.com/kaitu-io/k2/core/packet"
	"github.com/miekg/dns"
)

// inflightTimeout bounds how long a waiter blocks for an in-flight
// upstream DNS query. Must exceed ProxyDNSClient's worst-case:
// per server = DialUDP (5s ctx) + ReadFrom (5s deadline) = 10s,
// 2 servers sequential = 20s. Set to 22s with margin.
const inflightTimeout = 22 * time.Second

// DNSQuerier is satisfied by DirectDNSClient and ProxyDNSClient.
type DNSQuerier interface {
	Query(query *dns.Msg) (*dns.Msg, error)
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

	// Filters
	hosts  *HostsFile // /etc/hosts lookup
	filter *Filter    // RFC 6761 + noise filtering

	// Metrics
	metrics *Metrics

	// Writer for sending raw IP packet responses (L3 mode, nil for L4)
	writer io.Writer
}

// MiddlewareConfig contains configuration for DNS middleware
type MiddlewareConfig struct {
	DirectDNS DNSQuerier
	ProxyDNS  DNSQuerier
	DNSCache  *DNSCache
	Hosts     *HostsFile
	Filter    *Filter
	Metrics   *Metrics
	Writer    io.Writer // nil for L4-only usage
}

// NewMiddleware creates a new DNS middleware
func NewMiddleware(cfg MiddlewareConfig) *Middleware {
	return &Middleware{
		directDNS: cfg.DirectDNS,
		proxyDNS:  cfg.ProxyDNS,
		dnsCache:  cfg.DNSCache,
		hosts:     cfg.Hosts,
		filter:    cfg.Filter,
		metrics:   cfg.Metrics,
		writer:    cfg.Writer,
	}
}

// HandleQuery processes raw DNS query bytes through the full pipeline.
// Returns raw DNS response bytes. This is the L4-friendly entry point.
func (m *Middleware) HandleQuery(query []byte) ([]byte, error) {
	// Track total queries
	if m.metrics != nil {
		m.metrics.IncrTotal()
	}

	// 1. Parse DNS query
	var msg dns.Msg
	if err := msg.Unpack(query); err != nil {
		if m.metrics != nil {
			m.metrics.IncrError()
		}
		return nil, fmt.Errorf("unpack DNS query: %w", err)
	}

	// 2-12. Full pipeline (hosts, filter, cache, k2rule, upstream, SetTmpRule)
	resp, packed, err := m.processQuery(&msg)
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
func (m *Middleware) processQuery(query *dns.Msg) (*dns.Msg, []byte, error) {
	// 1. Extract domain from query
	domain := extractQueryDomain(query)
	if domain == "" {
		if m.metrics != nil {
			m.metrics.IncrError()
		}
		return nil, nil, fmt.Errorf("no domain in DNS query")
	}

	qtype := extractQueryType(query)
	slog.Debug("DNS query", "domain", domain, "type", qtype)

	// 2. Check hosts file first (OS behavior)
	if m.hosts != nil {
		if ips, found := m.hosts.Lookup(domain); found {
			if m.metrics != nil {
				m.metrics.IncrHostsHit()
			}
			return m.createHostsResponse(query, ips), nil, nil
		}
	}

	// 3. Check RFC 6761 special-use domains
	if m.filter != nil && m.filter.IsSpecialUse(domain) {
		if m.metrics != nil {
			m.metrics.IncrSpecialUseHit()
		}
		slog.Debug("DNS special-use filtered", "domain", domain)
		return m.filter.HandleSpecialUse(query), nil, nil
	}

	// 4. Check local domains (.local, .lan, etc.)
	if m.filter != nil && m.filter.IsLocalDomain(domain) {
		if m.metrics != nil {
			m.metrics.IncrLocalDomainHit()
		}
		slog.Debug("DNS local domain NXDOMAIN", "domain", domain)
		return m.filter.CreateNXDOMAIN(query), nil, nil
	}

	// 5. Noise filtering (PTR, AAAA, WPAD)
	if m.filter != nil {
		if isNoise, noiseResp := m.filter.TryFilterNoise(query); isNoise {
			if m.metrics != nil {
				m.metrics.IncrNoiseFiltered()
			}
			slog.Debug("DNS noise filtered", "domain", domain)
			return noiseResp, nil, nil
		}
	}

	// 6. Check DNS message cache — fast path returns pre-packed bytes.
	if m.dnsCache != nil {
		if packed, found := m.dnsCache.GetPacked(domain, query.Question[0].Qtype, query.Id); found {
			if m.metrics != nil {
				m.metrics.IncrMessageCacheHit()
			}
			slog.Debug("DNS cache hit", "domain", domain)
			return nil, packed, nil
		}
	}

	// 7. Domain-based routing (k2rule)
	action := k2rule.Match(domain)

	// Reject is instant — no upstream query, no dedup needed.
	if action == k2rule.TargetReject {
		slog.Debug("DNS routed reject", "domain", domain)
		return createNXDOMAIN(query), nil, nil
	}

	// 8. Query upstream DNS with in-flight deduplication.
	// If another goroutine is already querying the same domain+qtype,
	// we wait for its result instead of issuing a duplicate query.
	inflightKey := domain + ":" + strconv.FormatUint(uint64(query.Question[0].Qtype), 10)

	timedOut := false
	result := &inflightResult{done: make(chan struct{})}
	if existing, loaded := m.inflight.LoadOrStore(inflightKey, result); loaded {
		// Another goroutine is already querying — wait for its result.
		existing := existing.(*inflightResult)
		timer := time.NewTimer(inflightTimeout)
		defer timer.Stop()
		select {
		case <-existing.done:
			if existing.err != nil {
				return nil, nil, existing.err
			}
			// Copy packed bytes and patch caller's query ID.
			packed := cloneAndPatchID(existing.packed, query.Id)
			return nil, packed, nil
		case <-timer.C:
			// Clear stale entry so subsequent queries start fresh instead of
			// hitting the same dead owner in a timeout cascade loop.
			m.inflight.CompareAndDelete(inflightKey, existing)
			slog.Warn("DNS inflight dedup timeout, evicted stale entry, querying independently", "domain", domain, "key", inflightKey)
			timedOut = true
		}
	}

	// Waiter timed out — query independently (owner still owns the inflight key).
	if timedOut {
		resp, err := m.queryUpstream(domain, action, query)
		if err != nil {
			return nil, nil, err
		}
		packed, packErr := resp.Pack()
		if packErr != nil {
			return nil, nil, packErr
		}
		if m.dnsCache != nil {
			m.dnsCache.Add(domain, query.Question[0].Qtype, resp)
		}
		if resp.Rcode == dns.RcodeSuccess {
			for _, ip := range extractIPsFromResponse(resp) {
				k2rule.SetTmpRule(ip, action)
			}
		}
		return nil, packed, nil
	}

	// We own this query — execute and broadcast result.
	slog.Debug("DNS inflight owner start", "domain", domain, "key", inflightKey)
	resp, err := m.queryUpstream(domain, action, query)

	if err != nil {
		slog.Debug("DNS inflight owner failed", "domain", domain, "key", inflightKey, "err", err)
		result.err = err
		close(result.done)
		m.inflight.Delete(inflightKey)
		return nil, nil, err
	}

	// Pack response for inflight waiters and our return value.
	packed, packErr := resp.Pack()
	if packErr != nil {
		result.err = packErr
		close(result.done)
		m.inflight.Delete(inflightKey)
		return nil, nil, packErr
	}

	// Broadcast packed bytes to waiters.
	slog.Debug("DNS inflight owner done", "domain", domain, "key", inflightKey)
	result.packed = packed
	close(result.done)
	m.inflight.Delete(inflightKey)

	// 9. Cache DNS message (uses *dns.Msg for TTL optimization, packs internally)
	if m.dnsCache != nil {
		m.dnsCache.Add(domain, query.Question[0].Qtype, resp)
	}

	// 10. Store DNS-learned IP routes as temporary rules.
	// SetTmpRule skips storage when static rules already agree (built-in optimization).
	if resp.Rcode == dns.RcodeSuccess {
		ips := extractIPsFromResponse(resp)
		for _, ip := range ips {
			k2rule.SetTmpRule(ip, action)
		}
	}

	return nil, packed, nil
}

// queryUpstream sends a DNS query to the appropriate upstream (direct or proxy).
func (m *Middleware) queryUpstream(domain string, action k2rule.Target, query *dns.Msg) (*dns.Msg, error) {
	switch action {
	case k2rule.TargetDirect:
		if m.metrics != nil {
			m.metrics.IncrDirect()
		}
		resp, err := m.directDNS.Query(query)
		if err != nil {
			if m.metrics != nil {
				m.metrics.IncrError()
			}
			slog.Warn("DNS direct query failed", "domain", domain, "err", err)
			return nil, err
		}
		slog.Debug("DNS routed direct", "domain", domain)
		return resp, nil

	case k2rule.TargetProxy:
		if m.metrics != nil {
			m.metrics.IncrProxy()
		}
		resp, err := m.proxyDNS.Query(query)
		if err != nil {
			slog.Warn("DNS proxy query failed, fallback to direct", "domain", domain, "err", err)
			// Fallback: proxy failed → try direct DNS.
			// May return polluted results in censored networks, but a response
			// (even imperfect) is better than no response when tunnel is flaky.
			resp, err = m.directDNS.Query(query)
			if err != nil {
				if m.metrics != nil {
					m.metrics.IncrError()
				}
				return nil, fmt.Errorf("DNS proxy+direct fallback failed: %w", err)
			}
		}
		slog.Debug("DNS routed proxy", "domain", domain)
		return resp, nil

	default:
		return nil, fmt.Errorf("unexpected routing action: %d", action)
	}
}

// HandleDNSQuery processes a DNS query from L3 packets (TUN device).
// Extracts UDP payload, runs the pipeline via HandleQuery, wraps response in IP packet.
func (m *Middleware) HandleDNSQuery(pkt []byte, info packet.Info) error {
	// 1. Extract UDP payload
	udpPayload, err := packet.ExtractUDPPayload(pkt)
	if err != nil {
		if m.metrics != nil {
			m.metrics.IncrTotal()
			m.metrics.IncrError()
		}
		return fmt.Errorf("extract UDP payload: %w", err)
	}

	// 2-12. Run full pipeline via HandleQuery
	respBytes, err := m.HandleQuery(udpPayload)
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

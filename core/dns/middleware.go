package dns

import (
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"time"

	"github.com/kaitu-io/k2rule"
	"github.com/kaitu-io/k2/core/packet"
	"github.com/miekg/dns"
)

// Middleware handles DNS query interception and routing
type Middleware struct {
	// Upstream DNS clients
	directDNS *DirectDNSClient
	proxyDNS  *ProxyDNSClient

	// Caches
	dnsCache *DNSCache       // DNS message cache (with negative caching)
	ipCache  *DynamicIPCache // IP → Target mapping for routing

	// Filters
	hosts  *HostsFile // /etc/hosts lookup
	filter *Filter    // RFC 6761 + noise filtering

	// Metrics
	metrics *Metrics

	// Writer for sending raw IP packet responses (L3 mode, will be replaced in T5)
	writer io.Writer
}

// MiddlewareConfig contains configuration for DNS middleware
type MiddlewareConfig struct {
	DirectDNS *DirectDNSClient
	ProxyDNS  *ProxyDNSClient
	DNSCache  *DNSCache
	IPCache   *DynamicIPCache
	Hosts     *HostsFile
	Filter    *Filter
	Metrics   *Metrics
	Writer    io.Writer
}

// NewMiddleware creates a new DNS middleware
func NewMiddleware(cfg MiddlewareConfig) *Middleware {
	return &Middleware{
		directDNS: cfg.DirectDNS,
		proxyDNS:  cfg.ProxyDNS,
		dnsCache:  cfg.DNSCache,
		ipCache:   cfg.IPCache,
		hosts:     cfg.Hosts,
		filter:    cfg.Filter,
		metrics:   cfg.Metrics,
		writer:    cfg.Writer,
	}
}

// HandleDNSQuery processes a DNS query with comprehensive 10-step pipeline
func (m *Middleware) HandleDNSQuery(pkt []byte, info packet.Info) error {
	// Track total queries
	if m.metrics != nil {
		m.metrics.IncrTotal()
	}

	// 1. Extract UDP payload
	udpPayload, err := packet.ExtractUDPPayload(pkt)
	if err != nil {
		if m.metrics != nil {
			m.metrics.IncrError()
		}
		return fmt.Errorf("extract UDP payload: %w", err)
	}

	// 2. Parse DNS query
	var query dns.Msg
	if err := query.Unpack(udpPayload); err != nil {
		if m.metrics != nil {
			m.metrics.IncrError()
		}
		return fmt.Errorf("unpack DNS query: %w", err)
	}

	// 3. Extract domain from query
	domain := extractQueryDomain(&query)
	if domain == "" {
		if m.metrics != nil {
			m.metrics.IncrError()
		}
		return fmt.Errorf("no domain in DNS query")
	}

	qtype := extractQueryType(&query)
	log.Printf("DNS: %s %s", domain, qtype)

	var resp *dns.Msg

	// 4. Check hosts file first (OS behavior)
	if m.hosts != nil {
		if ips, found := m.hosts.Lookup(domain); found {
			if m.metrics != nil {
				m.metrics.IncrHostsHit()
			}
			resp = m.createHostsResponse(&query, ips)
			return m.sendResponse(resp, info)
		}
	}

	// 5. Check RFC 6761 special-use domains
	if m.filter != nil && m.filter.IsSpecialUse(domain) {
		if m.metrics != nil {
			m.metrics.IncrSpecialUseHit()
		}
		resp = m.filter.HandleSpecialUse(&query)
		log.Printf("DNS: %s special-use → filtered", domain)
		return m.sendResponse(resp, info)
	}

	// 6. Check local domains (.local, .lan, etc.)
	if m.filter != nil && m.filter.IsLocalDomain(domain) {
		if m.metrics != nil {
			m.metrics.IncrLocalDomainHit()
		}
		resp = m.filter.CreateNXDOMAIN(&query)
		log.Printf("DNS: %s local domain → NXDOMAIN", domain)
		return m.sendResponse(resp, info)
	}

	// 7. Noise filtering (PTR, AAAA, WPAD)
	if m.filter != nil {
		if isNoise, noiseResp := m.filter.TryFilterNoise(&query); isNoise {
			if m.metrics != nil {
				m.metrics.IncrNoiseFiltered()
			}
			log.Printf("DNS: %s noise → filtered", domain)
			return m.sendResponse(noiseResp, info)
		}
	}

	// 8. Check DNS message cache
	if m.dnsCache != nil {
		if cachedResp, found := m.dnsCache.Get(domain, query.Question[0].Qtype); found {
			if m.metrics != nil {
				m.metrics.IncrMessageCacheHit()
			}
			cachedResp.Id = query.Id // Update query ID
			log.Printf("DNS: %s cache hit", domain)
			return m.sendResponse(cachedResp, info)
		}
	}

	// 9. Domain-based routing (k2rule)
	action := k2rule.MatchDomain(domain)

	// 10. Query upstream DNS
	switch action {
	case k2rule.TargetDirect:
		if m.metrics != nil {
			m.metrics.IncrDirect()
		}
		resp, err = m.directDNS.Query(&query)
		if err != nil {
			if m.metrics != nil {
				m.metrics.IncrError()
			}
			log.Printf("DNS: direct query failed for %s: %v", domain, err)
			return err
		}
		log.Printf("DNS: %s → direct", domain)

	case k2rule.TargetProxy:
		if m.metrics != nil {
			m.metrics.IncrProxy()
		}
		resp, err = m.proxyDNS.Query(&query)
		if err != nil {
			if m.metrics != nil {
				m.metrics.IncrError()
			}
			log.Printf("DNS: proxy query failed for %s: %v", domain, err)
			return err
		}
		log.Printf("DNS: %s → proxy", domain)

	case k2rule.TargetReject:
		resp = createNXDOMAIN(&query)
		log.Printf("DNS: %s → reject", domain)
	}

	// 11. Cache DNS message
	if m.dnsCache != nil && resp != nil {
		m.dnsCache.Add(domain, query.Question[0].Qtype, resp)
	}

	// 12. Update IP cache for packet routing
	if resp != nil && resp.Rcode == dns.RcodeSuccess && m.ipCache != nil {
		ips := extractIPsFromResponse(resp)
		ttl := extractTTL(resp)

		for _, ip := range ips {
			rule := IPRule{
				Domain:   domain,
				Action:   action,
				ExpireAt: time.Now().Add(time.Duration(ttl) * time.Second),
			}
			m.ipCache.Add(ip, rule)
		}
	}

	// 13. Send response
	return m.sendResponse(resp, info)
}

// sendResponse sends a DNS response back through the TUN device
func (m *Middleware) sendResponse(resp *dns.Msg, info packet.Info) error {
	if resp == nil {
		return fmt.Errorf("no DNS response to send")
	}

	// Pack DNS response
	respBytes, err := resp.Pack()
	if err != nil {
		return fmt.Errorf("pack DNS response: %w", err)
	}

	// Build UDP/IP response packet (swap src/dst)
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
	var ips []string

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

// extractTTL extracts the minimum TTL from DNS response
// Returns 300 seconds (5 minutes) as default if no answers
func extractTTL(resp *dns.Msg) uint32 {
	if len(resp.Answer) == 0 {
		return 300 // Default 5 minutes
	}

	minTTL := resp.Answer[0].Header().Ttl
	for _, answer := range resp.Answer {
		ttl := answer.Header().Ttl
		if ttl < minTTL {
			minTTL = ttl
		}
	}

	// Enforce minimum TTL of 60 seconds
	if minTTL < 60 {
		minTTL = 60
	}

	return minTTL
}

// createNXDOMAIN creates a DNS NXDOMAIN response
func createNXDOMAIN(query *dns.Msg) *dns.Msg {
	resp := new(dns.Msg)
	resp.SetReply(query)
	resp.Rcode = dns.RcodeNameError
	return resp
}

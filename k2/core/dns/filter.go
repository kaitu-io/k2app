package dns

import (
	"strings"

	"github.com/miekg/dns"
)

// Filter handles DNS query filtering (RFC 6761, noise, local domains)
type Filter struct {
	enableIPv6 bool
}

// FilterConfig contains filter configuration
type FilterConfig struct {
	EnableIPv6 bool // If false, filter AAAA queries
}

// NewFilter creates a new DNS filter
func NewFilter(cfg FilterConfig) *Filter {
	return &Filter{
		enableIPv6: cfg.EnableIPv6,
	}
}

// IsSpecialUse checks if a domain is an RFC 6761 special-use domain
// These should NEVER be forwarded to upstream DNS
func (f *Filter) IsSpecialUse(domain string) bool {
	domain = strings.ToLower(domain)

	// RFC 6761 special-use domains
	specialUseSuffixes := []string{
		".local",      // RFC 6762 - mDNS
		".localhost",  // RFC 6761 - localhost
		".invalid",    // RFC 6761 - invalid domain
		".test",       // RFC 6761 - testing
		".example",    // RFC 6761 - documentation
		".onion",      // RFC 7686 - Tor
		".alt",        // RFC 9476 - alternative name resolution
		"home.arpa",   // RFC 8375 - home networks
	}

	for _, suffix := range specialUseSuffixes {
		if strings.HasSuffix(domain, suffix) || domain == strings.TrimPrefix(suffix, ".") {
			return true
		}
	}

	return false
}

// IsLocalDomain checks if a domain is a local network domain
// These are commonly used in LANs and should return NXDOMAIN
func (f *Filter) IsLocalDomain(domain string) bool {
	domain = strings.ToLower(domain)

	localSuffixes := []string{
		".lan",
		".internal",
		".localdomain",
		".home",
		".corp",
		".private",
	}

	for _, suffix := range localSuffixes {
		if strings.HasSuffix(domain, suffix) || domain == strings.TrimPrefix(suffix, ".") {
			return true
		}
	}

	return false
}

// TryFilterNoise attempts to filter noise queries (PTR, AAAA, WPAD, etc.)
// Returns (true, response) if filtered, (false, nil) if should proceed
func (f *Filter) TryFilterNoise(query *dns.Msg) (bool, *dns.Msg) {
	if len(query.Question) == 0 {
		return false, nil
	}

	q := query.Question[0]
	qname := strings.ToLower(q.Name)
	qtype := q.Qtype

	// Filter PTR queries (reverse DNS lookups)
	if qtype == dns.TypePTR {
		return true, createEmptyResponse(query)
	}

	// Filter AAAA (IPv6) queries if IPv6 is disabled
	if !f.enableIPv6 && qtype == dns.TypeAAAA {
		return true, createEmptyResponse(query)
	}

	// Filter WPAD (Web Proxy Auto-Discovery)
	if strings.Contains(qname, "wpad") {
		return true, createNXDOMAINWithSOA(query)
	}

	// Filter common noise domains
	noiseDomains := []string{
		"_msdcs.",      // Microsoft domain controller discovery
		"_ldap.",       // LDAP service discovery
		"_kerberos.",   // Kerberos service discovery
		"isatap.",      // ISATAP tunneling (IPv6)
		"teredo.",      // Teredo tunneling (IPv6)
	}

	for _, noise := range noiseDomains {
		if strings.Contains(qname, noise) {
			return true, createNXDOMAINWithSOA(query)
		}
	}

	return false, nil
}

// HandleSpecialUse creates an appropriate response for special-use domains
func (f *Filter) HandleSpecialUse(query *dns.Msg) *dns.Msg {
	if len(query.Question) == 0 {
		return createNXDOMAINWithSOA(query)
	}

	domain := strings.ToLower(query.Question[0].Name)

	// localhost → 127.0.0.1 or ::1
	if strings.HasSuffix(domain, ".localhost.") || domain == "localhost." {
		return createLocalhostResponse(query)
	}

	// .local domains → empty response (should use mDNS)
	if strings.HasSuffix(domain, ".local.") {
		return createEmptyResponse(query)
	}

	// All other special-use domains → NXDOMAIN
	return createNXDOMAINWithSOA(query)
}

// CreateNXDOMAIN creates an NXDOMAIN response with SOA record (RFC 2308)
func (f *Filter) CreateNXDOMAIN(query *dns.Msg) *dns.Msg {
	return createNXDOMAINWithSOA(query)
}

// createLocalhostResponse creates a response for localhost queries
func createLocalhostResponse(query *dns.Msg) *dns.Msg {
	resp := new(dns.Msg)
	resp.SetReply(query)

	if len(query.Question) == 0 {
		return resp
	}

	q := query.Question[0]

	// Return 127.0.0.1 for A queries
	if q.Qtype == dns.TypeA {
		resp.Answer = append(resp.Answer, &dns.A{
			Hdr: dns.RR_Header{
				Name:   q.Name,
				Rrtype: dns.TypeA,
				Class:  dns.ClassINET,
				Ttl:    300,
			},
			A: []byte{127, 0, 0, 1},
		})
	}

	// Return ::1 for AAAA queries
	if q.Qtype == dns.TypeAAAA {
		resp.Answer = append(resp.Answer, &dns.AAAA{
			Hdr: dns.RR_Header{
				Name:   q.Name,
				Rrtype: dns.TypeAAAA,
				Class:  dns.ClassINET,
				Ttl:    300,
			},
			AAAA: []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
		})
	}

	return resp
}

// createEmptyResponse creates an empty (no answers) success response
func createEmptyResponse(query *dns.Msg) *dns.Msg {
	resp := new(dns.Msg)
	resp.SetReply(query)
	return resp
}

// createNXDOMAINWithSOA creates an NXDOMAIN response with SOA record (RFC 2308)
// This enables negative caching on the client side
func createNXDOMAINWithSOA(query *dns.Msg) *dns.Msg {
	resp := new(dns.Msg)
	resp.SetReply(query)
	resp.Rcode = dns.RcodeNameError

	// Add SOA record for negative caching (RFC 2308)
	// This tells clients how long to cache the NXDOMAIN response
	soa := &dns.SOA{
		Hdr: dns.RR_Header{
			Name:   "k2tunnel.",
			Rrtype: dns.TypeSOA,
			Class:  dns.ClassINET,
			Ttl:    600, // 10 minutes negative cache
		},
		Ns:      "ns.k2tunnel.",
		Mbox:    "admin.k2tunnel.",
		Serial:  1,
		Refresh: 3600,
		Retry:   600,
		Expire:  86400,
		Minttl:  600, // Minimum TTL for negative caching
	}

	resp.Ns = append(resp.Ns, soa)
	return resp
}

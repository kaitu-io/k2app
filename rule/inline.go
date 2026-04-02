package rule

import (
	"encoding/binary"
	"net/netip"
)

// InlineSet is an in-memory rule set compiled from config domain_suffix and
// ip_cidr strings. Unlike BundleSet (backed by k2b file bytes), InlineSet
// owns its data — allocated at engine build time, freed on Close.
type InlineSet struct {
	domains domainSection
	ipv4    ipv4Section
	ipv6    ipv6Section
}

// BuildInlineSet compiles domain suffixes and CIDR strings into an InlineSet.
func BuildInlineSet(domainSuffixes []string, cidrs []string) InlineSet {
	var s InlineSet

	if len(domainSuffixes) > 0 {
		data := buildDomainData(domainSuffixes)
		if len(data) >= 4 {
			count := int(binary.LittleEndian.Uint32(data[0:4]))
			s.domains = domainSection{data: data, count: count}
		}
	}

	if len(cidrs) > 0 {
		v4data, v4count := buildIPv4Data(cidrs)
		if v4count > 0 {
			s.ipv4 = ipv4Section{data: v4data, count: v4count}
		}
		v6data, v6count := buildIPv6Data(cidrs)
		if v6count > 0 {
			s.ipv6 = ipv6Section{data: v6data, count: v6count}
		}
	}

	return s
}

// MatchDomain reports whether domain matches any suffix in this inline set.
func (s *InlineSet) MatchDomain(domain string) bool {
	return s.domains.Match(domain)
}

// MatchIP reports whether addr falls within any CIDR in this inline set.
func (s *InlineSet) MatchIP(addr netip.Addr) bool {
	if addr.Is4() {
		return s.ipv4.Contains(addr)
	}
	return s.ipv6.Contains(addr)
}

// IsEmpty reports whether this inline set has no rules.
func (s *InlineSet) IsEmpty() bool {
	return s.domains.count == 0 && s.ipv4.count == 0 && s.ipv6.count == 0
}

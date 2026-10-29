package dns

import (
	"testing"

	"github.com/miekg/dns"
)

func TestFilter_IsSpecialUse(t *testing.T) {
	filter := NewFilter(FilterConfig{})

	tests := []struct {
		domain   string
		expected bool
	}{
		// RFC 6761 special-use domains
		{"test.local", true},
		{"example.localhost", true},
		{"test.invalid", true},
		{"example.test", true},
		{"docs.example", true},
		{"site.onion", true},
		{"alt.example.alt", true},
		{"device.home.arpa", true},

		// Edge cases
		{"local", true},
		{"localhost", true},
		{"invalid", true},

		// Regular domains (not special-use)
		{"google.com", false},
		{"example.com", false},
		{"test.com", false},
		{"local.example.com", false}, // .local is suffix, not in middle
	}

	for _, tt := range tests {
		result := filter.IsSpecialUse(tt.domain)
		if result != tt.expected {
			t.Errorf("IsSpecialUse(%q) = %v, want %v", tt.domain, result, tt.expected)
		}
	}
}

func TestFilter_IsLocalDomain(t *testing.T) {
	filter := NewFilter(FilterConfig{})

	tests := []struct {
		domain   string
		expected bool
	}{
		// Local domains
		{"server.lan", true},
		{"nas.internal", true},
		{"router.localdomain", true},
		{"printer.home", true},
		{"vpn.corp", true},
		{"dev.private", true},

		// Edge cases
		{"lan", true},
		{"internal", true},

		// Regular domains
		{"example.com", false},
		{"internal.example.com", false}, // .internal in middle
	}

	for _, tt := range tests {
		result := filter.IsLocalDomain(tt.domain)
		if result != tt.expected {
			t.Errorf("IsLocalDomain(%q) = %v, want %v", tt.domain, result, tt.expected)
		}
	}
}

func TestFilter_TryFilterNoise_PTR(t *testing.T) {
	filter := NewFilter(FilterConfig{})

	query := &dns.Msg{
		Question: []dns.Question{{
			Name:   "1.1.168.192.in-addr.arpa.",
			Qtype:  dns.TypePTR,
			Qclass: dns.ClassINET,
		}},
	}

	isNoise, resp := filter.TryFilterNoise(query)
	if !isNoise {
		t.Error("Expected PTR query to be filtered as noise")
	}
	if resp == nil {
		t.Error("Expected response for PTR query")
	}
	if resp.Rcode != dns.RcodeSuccess {
		t.Errorf("Expected success response, got %d", resp.Rcode)
	}
}

func TestFilter_TryFilterNoise_AAAA(t *testing.T) {
	// IPv6 disabled
	filter := NewFilter(FilterConfig{EnableIPv6: false})

	query := &dns.Msg{
		Question: []dns.Question{{
			Name:   "example.com.",
			Qtype:  dns.TypeAAAA,
			Qclass: dns.ClassINET,
		}},
	}

	isNoise, resp := filter.TryFilterNoise(query)
	if !isNoise {
		t.Error("Expected AAAA query to be filtered when IPv6 disabled")
	}
	if resp == nil {
		t.Error("Expected response for AAAA query")
	}

	// IPv6 enabled - should NOT filter
	filter2 := NewFilter(FilterConfig{EnableIPv6: true})
	isNoise2, _ := filter2.TryFilterNoise(query)
	if isNoise2 {
		t.Error("Expected AAAA query NOT to be filtered when IPv6 enabled")
	}
}

func TestFilter_TryFilterNoise_WPAD(t *testing.T) {
	filter := NewFilter(FilterConfig{})

	query := &dns.Msg{
		Question: []dns.Question{{
			Name:   "wpad.",
			Qtype:  dns.TypeA,
			Qclass: dns.ClassINET,
		}},
	}

	isNoise, resp := filter.TryFilterNoise(query)
	if !isNoise {
		t.Error("Expected WPAD query to be filtered")
	}
	if resp == nil {
		t.Error("Expected response for WPAD query")
	}
	if resp.Rcode != dns.RcodeNameError {
		t.Errorf("Expected NXDOMAIN for WPAD, got %d", resp.Rcode)
	}
}

func TestFilter_TryFilterNoise_ServiceDiscovery(t *testing.T) {
	filter := NewFilter(FilterConfig{})

	tests := []string{
		"_ldap._tcp.dc._msdcs.example.com.",
		"_kerberos._tcp.example.com.",
		"isatap.example.com.",
		"teredo.ipv6.example.com.",
	}

	for _, domain := range tests {
		query := &dns.Msg{
			Question: []dns.Question{{
				Name:   domain,
				Qtype:  dns.TypeA,
				Qclass: dns.ClassINET,
			}},
		}

		isNoise, resp := filter.TryFilterNoise(query)
		if !isNoise {
			t.Errorf("Expected %q to be filtered as noise", domain)
		}
		if resp == nil {
			t.Errorf("Expected response for %q", domain)
		}
	}
}

func TestFilter_HandleSpecialUse_Localhost(t *testing.T) {
	filter := NewFilter(FilterConfig{})

	query := &dns.Msg{
		Question: []dns.Question{{
			Name:   "localhost.",
			Qtype:  dns.TypeA,
			Qclass: dns.ClassINET,
		}},
	}
	query.SetQuestion("localhost.", dns.TypeA)

	resp := filter.HandleSpecialUse(query)
	if resp == nil {
		t.Fatal("Expected response for localhost query")
	}
	if resp.Rcode != dns.RcodeSuccess {
		t.Errorf("Expected success response, got %d", resp.Rcode)
	}
	if len(resp.Answer) == 0 {
		t.Error("Expected answer for localhost query")
	}

	// Check for 127.0.0.1
	if a, ok := resp.Answer[0].(*dns.A); ok {
		if !a.A.Equal([]byte{127, 0, 0, 1}) {
			t.Errorf("Expected 127.0.0.1, got %v", a.A)
		}
	} else {
		t.Error("Expected A record in response")
	}
}

func TestFilter_HandleSpecialUse_Local(t *testing.T) {
	filter := NewFilter(FilterConfig{})

	query := &dns.Msg{
		Question: []dns.Question{{
			Name:   "device.local.",
			Qtype:  dns.TypeA,
			Qclass: dns.ClassINET,
		}},
	}
	query.SetQuestion("device.local.", dns.TypeA)

	resp := filter.HandleSpecialUse(query)
	if resp == nil {
		t.Fatal("Expected response for .local query")
	}
	// .local should return empty response (use mDNS)
	if resp.Rcode != dns.RcodeSuccess {
		t.Errorf("Expected success response, got %d", resp.Rcode)
	}
	if len(resp.Answer) != 0 {
		t.Error("Expected empty answer for .local query")
	}
}

func TestFilter_HandleSpecialUse_Invalid(t *testing.T) {
	filter := NewFilter(FilterConfig{})

	query := &dns.Msg{
		Question: []dns.Question{{
			Name:   "test.invalid.",
			Qtype:  dns.TypeA,
			Qclass: dns.ClassINET,
		}},
	}
	query.SetQuestion("test.invalid.", dns.TypeA)

	resp := filter.HandleSpecialUse(query)
	if resp == nil {
		t.Fatal("Expected response for .invalid query")
	}
	if resp.Rcode != dns.RcodeNameError {
		t.Errorf("Expected NXDOMAIN for .invalid, got %d", resp.Rcode)
	}
	// Should have SOA record for negative caching
	if len(resp.Ns) == 0 {
		t.Error("Expected SOA record in authority section")
	}
}

func TestFilter_CreateNXDOMAIN(t *testing.T) {
	filter := NewFilter(FilterConfig{})

	query := &dns.Msg{
		Question: []dns.Question{{
			Name:   "nonexistent.com.",
			Qtype:  dns.TypeA,
			Qclass: dns.ClassINET,
		}},
	}
	query.SetQuestion("nonexistent.com.", dns.TypeA)

	resp := filter.CreateNXDOMAIN(query)
	if resp == nil {
		t.Fatal("Expected NXDOMAIN response")
	}
	if resp.Rcode != dns.RcodeNameError {
		t.Errorf("Expected NXDOMAIN, got %d", resp.Rcode)
	}
	// Should have SOA record for RFC 2308 negative caching
	if len(resp.Ns) == 0 {
		t.Error("Expected SOA record in authority section")
	}

	// Verify SOA record
	soa, ok := resp.Ns[0].(*dns.SOA)
	if !ok {
		t.Error("Expected SOA record type")
	}
	if soa.Minttl < 1 {
		t.Error("Expected positive minimum TTL in SOA")
	}
}

package rule

import (
	"net/netip"
	"testing"
)

func buildTestIPv4Section(cidrs []string) ipv4Section {
	data, count := buildIPv4Data(cidrs)
	return ipv4Section{data: data, count: count}
}

func buildTestIPv6Section(cidrs []string) ipv6Section {
	data, count := buildIPv6Data(cidrs)
	return ipv6Section{data: data, count: count}
}

func TestIPv4Match(t *testing.T) {
	s := buildTestIPv4Section([]string{"8.8.8.0/24", "1.1.1.0/24"})

	tests := []struct {
		addr string
		want bool
	}{
		{"8.8.8.0", true},
		{"8.8.8.255", true},
		{"8.8.8.8", true},
		{"1.1.1.0", true},
		{"1.1.1.1", true},
		{"1.1.1.255", true},
		{"8.8.9.0", false},
		{"1.1.2.0", false},
		{"192.168.1.1", false},
	}

	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			got := s.Contains(netip.MustParseAddr(tt.addr))
			if got != tt.want {
				t.Errorf("Contains(%s) = %v, want %v", tt.addr, got, tt.want)
			}
		})
	}
}

func TestIPv4Empty(t *testing.T) {
	s := buildTestIPv4Section(nil)
	if s.Contains(netip.MustParseAddr("8.8.8.8")) {
		t.Error("empty section should match nothing")
	}

	s2 := buildTestIPv4Section([]string{})
	if s2.Contains(netip.MustParseAddr("8.8.8.8")) {
		t.Error("empty section should match nothing")
	}
}

func TestIPv4AdjacentRanges(t *testing.T) {
	s := buildTestIPv4Section([]string{"10.0.0.0/24", "10.0.1.0/24"})

	tests := []struct {
		addr string
		want bool
	}{
		{"10.0.0.0", true},
		{"10.0.0.128", true},
		{"10.0.0.255", true},
		{"10.0.1.0", true},
		{"10.0.1.128", true},
		{"10.0.1.255", true},
		{"10.0.2.0", false},
		{"9.255.255.255", false},
	}

	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			got := s.Contains(netip.MustParseAddr(tt.addr))
			if got != tt.want {
				t.Errorf("Contains(%s) = %v, want %v", tt.addr, got, tt.want)
			}
		})
	}
}

func TestIPv6Match(t *testing.T) {
	s := buildTestIPv6Section([]string{"2001:4860::/32"})

	tests := []struct {
		addr string
		want bool
	}{
		{"2001:4860::", true},
		{"2001:4860::1", true},
		{"2001:4860:ffff:ffff:ffff:ffff:ffff:ffff", true},
		{"2001:4861::", false},
		{"2001:4859:ffff:ffff:ffff:ffff:ffff:ffff", false},
		{"::1", false},
	}

	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			got := s.Contains(netip.MustParseAddr(tt.addr))
			if got != tt.want {
				t.Errorf("Contains(%s) = %v, want %v", tt.addr, got, tt.want)
			}
		})
	}
}

func TestIPv6Empty(t *testing.T) {
	s := buildTestIPv6Section(nil)
	if s.Contains(netip.MustParseAddr("2001:4860::1")) {
		t.Error("empty section should match nothing")
	}

	s2 := buildTestIPv6Section([]string{})
	if s2.Contains(netip.MustParseAddr("2001:4860::1")) {
		t.Error("empty section should match nothing")
	}
}

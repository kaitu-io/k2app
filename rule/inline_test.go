package rule

import (
	"net/netip"
	"testing"

	"github.com/kaitu-io/k2-rules/krs"
)

func TestInlineSet_DomainMatch(t *testing.T) {
	s := BuildInlineSet([]string{"google.com", "youtube.com"}, nil)

	tests := []struct {
		domain string
		want   bool
	}{
		{"google.com", true},
		{"www.google.com", true},
		{"youtube.com", true},
		{"example.com", false},
		{"notgoogle.com", false},
	}
	for _, tt := range tests {
		if got := s.MatchDomain(tt.domain); got != tt.want {
			t.Errorf("MatchDomain(%q) = %v, want %v", tt.domain, got, tt.want)
		}
	}
}

func TestInlineSet_IPMatch(t *testing.T) {
	s := BuildInlineSet(nil, []string{"8.8.8.0/24", "2001:4860::/32"})

	tests := []struct {
		ip   string
		want bool
	}{
		{"8.8.8.8", true},
		{"8.8.8.1", true},
		{"8.8.4.4", false},
		{"2001:4860::1", true},
		{"2001:4861::1", false},
	}
	for _, tt := range tests {
		addr := netip.MustParseAddr(tt.ip)
		if got := s.MatchIP(addr); got != tt.want {
			t.Errorf("MatchIP(%q) = %v, want %v", tt.ip, got, tt.want)
		}
	}
}

func TestInlineSet_Empty(t *testing.T) {
	s := BuildInlineSet(nil, nil)
	if s.MatchDomain("anything.com") {
		t.Error("empty InlineSet should not match domains")
	}
	if s.MatchIP(netip.MustParseAddr("1.2.3.4")) {
		t.Error("empty InlineSet should not match IPs")
	}
	if !s.IsEmpty() {
		t.Error("empty InlineSet should report IsEmpty")
	}
}

func TestInlineSet_Mixed(t *testing.T) {
	s := BuildInlineSet([]string{"example.com"}, []string{"10.0.0.0/8"})
	if !s.MatchDomain("example.com") {
		t.Error("should match domain")
	}
	if !s.MatchIP(netip.MustParseAddr("10.1.2.3")) {
		t.Error("should match IP")
	}
	if s.MatchDomain("other.com") {
		t.Error("should not match unrelated domain")
	}
	if s.MatchIP(netip.MustParseAddr("192.168.1.1")) {
		t.Error("should not match unrelated IP")
	}
	if s.IsEmpty() {
		t.Error("non-empty InlineSet should not report IsEmpty")
	}
}

func TestInlineSet_MatchDomainReversed_AgreesWithMatchDomain(t *testing.T) {
	s := BuildInlineSet([]string{"example.com", "youtube.com"}, nil)
	hosts := []string{
		"example.com", "www.example.com", "a.b.example.com",
		"notexample.com", "example.org", "youtube.com", "com", "x.youtube.com",
	}
	for _, host := range hosts {
		want := s.MatchDomain(host)
		got := s.MatchDomainReversed(krs.ReversedParents(host))
		if got != want {
			t.Errorf("host=%q: MatchDomainReversed=%v want(MatchDomain)=%v", host, got, want)
		}
	}
}

func TestInlineSet_MatchDomainReversed_EmptySet(t *testing.T) {
	var s InlineSet
	if s.MatchDomainReversed(krs.ReversedParents("anything.com")) {
		t.Error("empty inline set must not match")
	}
}

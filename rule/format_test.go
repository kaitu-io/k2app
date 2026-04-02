package rule

import (
	"bytes"
	"net/netip"
	"testing"
)

func TestFormatRoundTrip(t *testing.T) {
	sets := []BundleSet{
		{
			Name:           "google",
			DomainSuffixes: []string{"google.com", "youtube.com", "gmail.com"},
			CIDRs:          []string{"8.8.8.0/24", "8.8.4.0/24", "2001:4860::/32"},
		},
		{
			Name:           "telegram",
			DomainSuffixes: []string{"telegram.org", "t.me"},
			CIDRs:          []string{"149.154.160.0/20", "2001:67c:4e8::/48"},
		},
	}

	var buf bytes.Buffer
	if err := WriteBundle(&buf, sets); err != nil {
		t.Fatalf("WriteBundle: %v", err)
	}

	data := buf.Bytes()
	if len(data) == 0 {
		t.Fatal("WriteBundle produced empty output")
	}

	bundle, err := ReadBundle(data)
	if err != nil {
		t.Fatalf("ReadBundle: %v", err)
	}

	if len(bundle.Sets) != 2 {
		t.Fatalf("expected 2 sets, got %d", len(bundle.Sets))
	}

	// Verify set names.
	if bundle.Sets[0].Name != "google" {
		t.Errorf("set[0].Name = %q, want %q", bundle.Sets[0].Name, "google")
	}
	if bundle.Sets[1].Name != "telegram" {
		t.Errorf("set[1].Name = %q, want %q", bundle.Sets[1].Name, "telegram")
	}

	// Google domain matches.
	for _, d := range []string{"google.com", "www.google.com", "youtube.com", "mail.gmail.com"} {
		if !bundle.Sets[0].MatchDomain(d) {
			t.Errorf("google set should match domain %q", d)
		}
	}
	// Google domain non-matches.
	for _, d := range []string{"telegram.org", "example.com"} {
		if bundle.Sets[0].MatchDomain(d) {
			t.Errorf("google set should NOT match domain %q", d)
		}
	}

	// Google IPv4 matches.
	for _, ip := range []string{"8.8.8.8", "8.8.4.4"} {
		addr := netip.MustParseAddr(ip)
		if !bundle.Sets[0].MatchIP(addr) {
			t.Errorf("google set should match IP %s", ip)
		}
	}
	// Google IPv6 match.
	addr6 := netip.MustParseAddr("2001:4860::1")
	if !bundle.Sets[0].MatchIP(addr6) {
		t.Error("google set should match 2001:4860::1")
	}
	// Google IP non-match.
	if bundle.Sets[0].MatchIP(netip.MustParseAddr("1.1.1.1")) {
		t.Error("google set should NOT match 1.1.1.1")
	}

	// Telegram domain matches.
	if !bundle.Sets[1].MatchDomain("telegram.org") {
		t.Error("telegram set should match telegram.org")
	}
	if !bundle.Sets[1].MatchDomain("t.me") {
		t.Error("telegram set should match t.me")
	}
	// Telegram IP matches.
	if !bundle.Sets[1].MatchIP(netip.MustParseAddr("149.154.167.50")) {
		t.Error("telegram set should match 149.154.167.50")
	}
	if !bundle.Sets[1].MatchIP(netip.MustParseAddr("2001:67c:4e8::1")) {
		t.Error("telegram set should match 2001:67c:4e8::1")
	}
}

func TestFormatEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteBundle(&buf, nil); err != nil {
		t.Fatalf("WriteBundle(nil): %v", err)
	}

	bundle, err := ReadBundle(buf.Bytes())
	if err != nil {
		t.Fatalf("ReadBundle: %v", err)
	}
	if len(bundle.Sets) != 0 {
		t.Errorf("expected 0 sets, got %d", len(bundle.Sets))
	}
}

func TestFormatDomainsOnly(t *testing.T) {
	sets := []BundleSet{
		{
			Name:           "domains-only",
			DomainSuffixes: []string{"example.com", "test.org"},
		},
	}

	var buf bytes.Buffer
	if err := WriteBundle(&buf, sets); err != nil {
		t.Fatalf("WriteBundle: %v", err)
	}

	bundle, err := ReadBundle(buf.Bytes())
	if err != nil {
		t.Fatalf("ReadBundle: %v", err)
	}

	if len(bundle.Sets) != 1 {
		t.Fatalf("expected 1 set, got %d", len(bundle.Sets))
	}

	s := &bundle.Sets[0]

	// Domain matches work.
	if !s.MatchDomain("example.com") {
		t.Error("should match example.com")
	}
	if !s.MatchDomain("sub.test.org") {
		t.Error("should match sub.test.org")
	}

	// IP does not match (no CIDRs).
	if s.MatchIP(netip.MustParseAddr("1.2.3.4")) {
		t.Error("should NOT match any IPv4")
	}
	if s.MatchIP(netip.MustParseAddr("::1")) {
		t.Error("should NOT match any IPv6")
	}
}

func TestFormatIPsOnly(t *testing.T) {
	sets := []BundleSet{
		{
			Name:  "ips-only",
			CIDRs: []string{"10.0.0.0/8", "fd00::/8"},
		},
	}

	var buf bytes.Buffer
	if err := WriteBundle(&buf, sets); err != nil {
		t.Fatalf("WriteBundle: %v", err)
	}

	bundle, err := ReadBundle(buf.Bytes())
	if err != nil {
		t.Fatalf("ReadBundle: %v", err)
	}

	if len(bundle.Sets) != 1 {
		t.Fatalf("expected 1 set, got %d", len(bundle.Sets))
	}

	s := &bundle.Sets[0]

	// IP matches work.
	if !s.MatchIP(netip.MustParseAddr("10.1.2.3")) {
		t.Error("should match 10.1.2.3")
	}
	if !s.MatchIP(netip.MustParseAddr("fd00::1")) {
		t.Error("should match fd00::1")
	}

	// Domain does not match (no domains).
	if s.MatchDomain("example.com") {
		t.Error("should NOT match any domain")
	}
}

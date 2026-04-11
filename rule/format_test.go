package rule

import (
	"bytes"
	"encoding/binary"
	"net/netip"
	"testing"
)

func TestFormatRoundTrip(t *testing.T) {
	sets := []BundleSet{
		{
			Name:           "google",
			DomainSuffixes: []string{"google.com", "youtube.com", "gmail.com"},
			ExcludeDomains: []string{"google.cn"}, // should be carved out of google.com
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
	// Exclude semantics: google.cn is carved out of google.com suffix.
	for _, d := range []string{"google.cn", "www.google.cn"} {
		if bundle.Sets[0].MatchDomain(d) {
			t.Errorf("google set should exclude %q (would otherwise match google.com suffix)", d)
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

func TestFormatExcludeSemantics(t *testing.T) {
	// Model the cn-sites use case: weibo.com is Chinese, but hk.weibo.com @!cn
	// should NOT be treated as Chinese.
	sets := []BundleSet{
		{
			Name:           "cn-sites",
			DomainSuffixes: []string{"weibo.com"},
			ExcludeDomains: []string{"hk.weibo.com"},
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

	// Included (suffix hits, no exclude hit).
	for _, d := range []string{"weibo.com", "www.weibo.com", "sina.weibo.com"} {
		if !s.MatchDomain(d) {
			t.Errorf("should match %q (weibo.com suffix, not excluded)", d)
		}
	}

	// Excluded (hk.weibo.com exclude wins over weibo.com include).
	for _, d := range []string{"hk.weibo.com", "m.hk.weibo.com"} {
		if s.MatchDomain(d) {
			t.Errorf("should NOT match %q (hk.weibo.com is excluded)", d)
		}
	}

	// Unrelated domain — never hit include.
	if s.MatchDomain("example.com") {
		t.Errorf("should NOT match example.com")
	}
}

// TestReadBundleRejectsV1 verifies that a v1-shaped header is refused cleanly
// (no panic, no corrupted read). v1 was never shipped to consumers — this
// test guards against accidental CDN rollback.
func TestReadBundleRejectsV1(t *testing.T) {
	data := make([]byte, headerSize)
	copy(data[0:4], magic)
	binary.LittleEndian.PutUint32(data[4:8], 1) // v1
	binary.LittleEndian.PutUint32(data[8:12], 0)

	_, err := ReadBundle(data)
	if err == nil {
		t.Fatal("ReadBundle should reject v1 header")
	}
	// Error message must mention both versions so humans can diagnose.
	msg := err.Error()
	if !bytes.Contains([]byte(msg), []byte("unsupported version 1")) {
		t.Errorf("error should mention 'unsupported version 1', got %q", msg)
	}
	if !bytes.Contains([]byte(msg), []byte("want 2")) {
		t.Errorf("error should mention expected version 2, got %q", msg)
	}
}

// TestReadBundleBoundsCheckExclude verifies an out-of-bounds exclude offset
// returns an error instead of panicking.
func TestReadBundleBoundsCheckExclude(t *testing.T) {
	// Construct a minimum-valid header + index with exclDomainOff pointing
	// past the buffer and exclDomainCnt > 0.
	setCount := 1
	total := headerSize + indexSize*setCount
	data := make([]byte, total)
	copy(data[0:4], magic)
	binary.LittleEndian.PutUint32(data[4:8], version)
	binary.LittleEndian.PutUint32(data[8:12], uint32(setCount))

	idx := data[headerSize:]
	// Name at offset = total (out of bounds will be caught by name check first
	// if we put it there). Put name at end-0 length 0 so it passes.
	binary.LittleEndian.PutUint32(idx[0:4], uint32(total))
	binary.LittleEndian.PutUint16(idx[4:6], 0)
	// Zero domain/ipv4/ipv6 sections.
	// Put exclDomainOff way past buffer with count > 0.
	binary.LittleEndian.PutUint32(idx[28:32], uint32(total+1024))
	binary.LittleEndian.PutUint32(idx[32:36], 5)

	_, err := ReadBundle(data)
	if err == nil {
		t.Fatal("ReadBundle should reject out-of-bounds exclude offset")
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

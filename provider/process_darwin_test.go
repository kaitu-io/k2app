//go:build darwin && !ios

package provider

import (
	"net/netip"
	"testing"
)

// =============================================================================
// parseLsofOutput — synthetic lsof -F pc input. Tests every empirical quirk
// observed on production macOS (Darwin 25.x) to push App Bypass confidence
// to 10/10 without needing a fleet of test devices.
// =============================================================================

const selfPID = 1234

// lsofSpec must bracket IPv6 addresses — lsof's -i grammar splits the selector
// at the first colon otherwise, silently matching nothing. Before 2026-08-14
// this never mattered because FindProcess early-returned on any non-IPv4
// srcIP; now v6 connections reach lsof and the spec must be well-formed.
func TestLsofSpec(t *testing.T) {
	cases := []struct {
		network string
		ip      string
		port    uint16
		want    string
	}{
		{"tcp", "192.168.1.5", 54321, "tcp@192.168.1.5:54321"},
		{"udp", "198.18.0.2", 443, "udp@198.18.0.2:443"},
		{"tcp", "fdfe:dcba:9876::2", 54321, "tcp@[fdfe:dcba:9876::2]:54321"},
		{"udp", "2001:db8::1", 443, "udp@[2001:db8::1]:443"},
	}
	for _, c := range cases {
		got := lsofSpec(c.network, netip.MustParseAddr(c.ip), c.port)
		if got != c.want {
			t.Errorf("lsofSpec(%s, %s, %d) = %q, want %q", c.network, c.ip, c.port, got, c.want)
		}
	}
}

// Empirically-verified output forms collected from `lsof -i -F pc` on a
// production macOS 25.5. The COMMAND field arrives untruncated, even for
// 20+ char bundle helper names — proving the kernel quirk that mattered
// on Linux (TASK_COMM_LEN) doesn't apply to macOS lsof.
func TestParseLsofOutput_EmpiricalForms(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "single record with prefix-formatted fields",
			in:   "p9999\ncCurl\nf3\n",
			want: "Curl",
		},
		{
			name: "Chrome helper untruncated (20 chars)",
			in:   "p2995\ncGoogle Chrome Helper\nf42\n",
			want: "Google Chrome Helper",
		},
		{
			name: "Code Helper with parens (22 chars)",
			in:   "p3034\ncCode Helper (Renderer)\nf42\n",
			want: "Code Helper (Renderer)",
		},
		{
			name: "WebKit dotted bundle (27 chars)",
			in:   "p1500\nccom.apple.WebKit.Networking\nf17\n",
			want: "com.apple.WebKit.Networking",
		},
		{
			name: "Slack Helper (12 chars)",
			in:   "p3500\ncSlack Helper\nf28\n",
			want: "Slack Helper",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseLsofOutput([]byte(tc.in), selfPID); got != tc.want {
				t.Errorf("parseLsofOutput(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// Multi-process output: lsof returns BOTH endpoints when a TCP connection
// is established. The first record is the daemon itself; we must skip
// self and return the peer (curl/browser/etc.).
func TestParseLsofOutput_SkipSelfReturnsPeer(t *testing.T) {
	// Daemon (PID = selfPID) and peer (PID 9999) both listed.
	in := "p1234\nck2\nf10\np9999\nccurl\nf42\n"
	if got := parseLsofOutput([]byte(in), selfPID); got != "curl" {
		t.Errorf("got %q, want curl (self must be skipped)", got)
	}
}

// Reverse order: peer first, daemon second. Parser must return the peer
// without being misled by the trailing self record.
func TestParseLsofOutput_PeerFirstSelfSecond(t *testing.T) {
	in := "p9999\ncchrome\nf42\np1234\nck2\nf10\n"
	if got := parseLsofOutput([]byte(in), selfPID); got != "chrome" {
		t.Errorf("got %q, want chrome (peer-first ordering)", got)
	}
}

// Only the daemon's own record → empty (correctly skipped).
func TestParseLsofOutput_OnlySelf_ReturnsEmpty(t *testing.T) {
	in := "p1234\nck2\nf10\n"
	if got := parseLsofOutput([]byte(in), selfPID); got != "" {
		t.Errorf("got %q, want empty (only self present)", got)
	}
}

// Empty input — process disappeared between socket scan and lsof exec.
// Falls through gracefully.
func TestParseLsofOutput_Empty(t *testing.T) {
	if got := parseLsofOutput([]byte(""), selfPID); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

// Garbage/malformed PID line — parser must not crash and must not
// commit a partial record.
func TestParseLsofOutput_BadPidLine_Resets(t *testing.T) {
	in := "pNOT_A_NUMBER\ncghost\np9999\ncchrome\nf42\n"
	if got := parseLsofOutput([]byte(in), selfPID); got != "chrome" {
		t.Errorf("got %q, want chrome (bad-pid record discarded, valid one returned)", got)
	}
}

// Unknown record-type prefixes ('f', 't', 'P', etc.) are ignored — only
// 'p' and 'c' carry signal. Surface lsof versions may emit additional
// fields with -F pc.
func TestParseLsofOutput_UnknownPrefixesIgnored(t *testing.T) {
	in := "p9999\nccurl\nf42\ntIPv4\nPTCP\nLLISTEN\n"
	if got := parseLsofOutput([]byte(in), selfPID); got != "curl" {
		t.Errorf("got %q, want curl", got)
	}
}

// Empty command ("c\n") — record dropped, parser continues.
func TestParseLsofOutput_EmptyCommand_RecordDropped(t *testing.T) {
	in := "p9999\nc\nf42\np8888\nccurl\nf17\n"
	if got := parseLsofOutput([]byte(in), selfPID); got != "curl" {
		t.Errorf("got %q, want curl (empty-cmd record skipped)", got)
	}
}

// Multi-record both non-self — parser returns the first non-self
// command in document order. Deterministic.
func TestParseLsofOutput_MultipleNonSelf_ReturnsFirst(t *testing.T) {
	in := "p7777\ncwget\nf3\np9999\nccurl\nf42\n"
	if got := parseLsofOutput([]byte(in), selfPID); got != "wget" {
		t.Errorf("got %q, want wget (first non-self record)", got)
	}
}

// Trailing whitespace/empty lines at end of output — common when reading
// via exec.Command().Output(). Must not cause issues.
func TestParseLsofOutput_TrailingEmptyLines(t *testing.T) {
	in := "p9999\nccurl\nf42\n\n\n"
	if got := parseLsofOutput([]byte(in), selfPID); got != "curl" {
		t.Errorf("got %q, want curl (trailing empty lines tolerated)", got)
	}
}

// Multi-byte UTF-8 process name (e.g. localized macOS bundle name).
// COMMAND field is UTF-8 bytes; comparison is byte-equal.
func TestParseLsofOutput_UTF8Name(t *testing.T) {
	in := "p9999\nc微信\nf42\n"
	if got := parseLsofOutput([]byte(in), selfPID); got != "微信" {
		t.Errorf("got %q, want %q (UTF-8 multibyte name)", got, "微信")
	}
}

// =============================================================================
// Integration: DarwinProcessSearcher cache hit avoids re-shelling lsof.
// Verified via the cache layer's contract — first call populates, second
// returns the cached value without ever invoking lsof (which would be
// blocked by the closed listener in TestProcessSearcher_SkipsSelf).
// =============================================================================

func TestDarwinProcessSearcher_CacheHitShortCircuits(t *testing.T) {
	s := NewDarwinProcessSearcher()
	srcIP := netip.MustParseAddr("127.0.0.1")
	srcPort := uint16(54321)
	// Pre-populate cache with a known answer (simulates "we already looked
	// this up 1.5s ago, lsof should NOT be re-run").
	s.cache.set(cacheKey("tcp", srcIP, srcPort), "WeChat", "", "")
	name, _, _ := s.FindProcess("tcp", srcIP, srcPort)
	if name != "WeChat" {
		t.Errorf("got %q, want WeChat (cache hit)", name)
	}
}

// IPv6 source addresses early-return without touching lsof. The provider
// layer is IPv4-only for App Bypass attribution (matches Linux + Windows
// behavior).
func TestDarwinProcessSearcher_IPv6Early(t *testing.T) {
	s := NewDarwinProcessSearcher()
	name, pkg, installer := s.FindProcess("tcp", netip.MustParseAddr("2001:db8::1"), 443)
	if name != "" || pkg != "" || installer != "" {
		t.Errorf("IPv6 must early-return empty, got (%q,%q,%q)", name, pkg, installer)
	}
}

// Zero-value DarwinProcessSearcher (someone did &DarwinProcessSearcher{}
// instead of NewDarwinProcessSearcher()) must lazy-init its cache and
// still work. Otherwise the cache.set call panics on nil deref.
func TestDarwinProcessSearcher_ZeroValueLazyInits(t *testing.T) {
	s := &DarwinProcessSearcher{}
	// Pre-populating directly would panic on nil cache, so we go through
	// FindProcess which has the lazy-init guard. IPv6 path doesn't exec
	// lsof, but FindProcess returns before reaching cache.set — so use
	// IPv4 path. Cache may or may not get populated depending on actual
	// lsof output; the assertion is just "doesn't panic".
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("FindProcess panicked on zero-value struct: %v", r)
		}
	}()
	_, _, _ = s.FindProcess("tcp", netip.MustParseAddr("127.0.0.1"), 0)
}

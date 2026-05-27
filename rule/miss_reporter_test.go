package rule

import (
	"bytes"
	"sync/atomic"
	"testing"

	"github.com/kaitu-io/k2-rules/krs"
)

// stubReporter is a test MissReporter that records calls.
type stubReporter struct {
	calls int32
	last  struct {
		host     string
		protocol string
	}
}

func (s *stubReporter) Report(host, protocol string) {
	atomic.AddInt32(&s.calls, 1)
	s.last.host = host
	s.last.protocol = protocol
}

// TestMissReporter_NilSafe verifies that Match / MatchConn do not panic
// when no reporter is installed — this is the Phase 1 "dark" default.
func TestMissReporter_NilSafe(t *testing.T) {
	eng := NewEngine(nil, Target(0), nil, false)
	defer eng.Close()

	// Both paths (domain + IP) must be nil-safe.
	_ = eng.Match("example.com")
	_ = eng.Match("1.2.3.4")
	_ = eng.MatchConn("example.com", ConnMeta{Network: "tcp"})
}

// TestMissReporter_ReceivesFQDN verifies the reporter is called with the
// exact host and protocol on fallback, and only for FQDNs (no IPs, no
// bare hostnames, no .local).
func TestMissReporter_ReceivesFQDN(t *testing.T) {
	b := mustBundle(t, []krs.NamedSet{
		{Name: "google", DomainSuffixes: []string{"google.com"}},
	})
	idx := krs.Index([]*krs.Bundle{b})
	routes := []RouteEntry{
		{Target: Target(2), Sets: bindSets(idx, "google")},
	}
	eng := NewEngine(routes, Target(0), []*krs.Bundle{b}, false)
	defer eng.Close()

	var rep stubReporter
	eng.SetMissReporter(&rep)

	// 1. Fallback FQDN via Match → one call with empty protocol.
	eng.Match("example.com")
	if rep.calls != 1 {
		t.Fatalf("Match FQDN: expected 1 call, got %d", rep.calls)
	}
	if rep.last.host != "example.com" || rep.last.protocol != "" {
		t.Errorf("Match FQDN: got (%q, %q)", rep.last.host, rep.last.protocol)
	}

	// 2. Fallback FQDN via MatchConn with tcp protocol.
	eng.MatchConn("bing.com", ConnMeta{Network: "tcp"})
	if rep.calls != 2 {
		t.Fatalf("MatchConn FQDN: expected 2 calls, got %d", rep.calls)
	}
	if rep.last.host != "bing.com" || rep.last.protocol != "tcp" {
		t.Errorf("MatchConn FQDN: got (%q, %q)", rep.last.host, rep.last.protocol)
	}

	// 3. Matched route (not fallback) → no call.
	before := rep.calls
	eng.Match("mail.google.com")
	if rep.calls != before {
		t.Errorf("matched route: expected no report, got %d (was %d)", rep.calls, before)
	}

	// 4. IP literal → no call.
	eng.Match("1.2.3.4")
	if rep.calls != before {
		t.Errorf("IP literal: expected no report, got %d", rep.calls)
	}

	// 5. Bare hostname → no call (no dot).
	eng.Match("myserver")
	if rep.calls != before {
		t.Errorf("bare hostname: expected no report, got %d", rep.calls)
	}

	// 6. .local → no call (private TLD).
	eng.Match("printer.local")
	if rep.calls != before {
		t.Errorf(".local: expected no report, got %d", rep.calls)
	}
}

// TestIsFQDN ensures the private-tld filter and basic shape checks work.
func TestIsFQDN(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"example.com", true},
		{"a.b.example.co.uk", true},
		{"", false},
		{"localhost", false},
		{"mybox", false},
		{"1.2.3.4", false},
		{"::1", false},
		{"printer.local", false},
		{"foo.lan", false},
		{"host.internal", false},
		{".com", false},
		{"example.", false},
	}
	for _, tt := range tests {
		if got := isFQDN(tt.in); got != tt.want {
			t.Errorf("isFQDN(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

// BenchmarkMatchHostLocked measures the domain-fallback match path. This
// is the hot path — every DNS query passes through here.
//
// Phase 1 requirement: reporter=nil (the dark default) must cost <1% more
// than the pre-instrumentation baseline. "baseline" here runs an
// unlocked raw-match that skips the MissReporter field read entirely,
// approximating the pre-instrumentation ns/op.
func BenchmarkMatchHostLocked(b *testing.B) {
	bundle := mustBenchBundle(b, []krs.NamedSet{
		{Name: "google", DomainSuffixes: []string{"google.com", "youtube.com"}},
		{Name: "cn-sites", DomainSuffixes: []string{"baidu.com", "qq.com", "weibo.com"}},
	})
	idx := krs.Index([]*krs.Bundle{bundle})
	routes := []RouteEntry{
		{Target: Target(2), Sets: bindSets(idx, "google")},
		{Target: Target(0), Sets: bindSets(idx, "cn-sites")},
	}

	// Mix of matching and fallback hosts.
	hosts := []string{
		"mail.google.com",      // matched (route 0)
		"www.baidu.com",        // matched (route 1)
		"fallback-example.com", // fallback FQDN
		"8.8.8.8",              // fallback IP
		"youtube.com",          // matched
		"unknown-site.org",     // fallback FQDN
	}

	// baseline: the pre-instrumentation Match() equivalent exposed on
	// the Engine as matchNoTelemetry — same receiver type + call shape
	// as Match(), but with no atomic pointer load and no reporter branch.
	// This is an apples-to-apples comparison: both are method calls via
	// the Engine vtable; the only delta is the instrumentation code.
	b.Run("baseline", func(b *testing.B) {
		eng := NewEngine(routes, Target(0), []*krs.Bundle{bundle}, false)
		defer eng.Close()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = eng.matchNoTelemetry(hosts[i%len(hosts)])
		}
	})

	// nil_reporter: the actual Phase 1 default — Match() with a nil
	// MissReporter field. Should be within 1% of baseline.
	b.Run("nil_reporter", func(b *testing.B) {
		eng := NewEngine(routes, Target(0), []*krs.Bundle{bundle}, false)
		defer eng.Close()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = eng.Match(hosts[i%len(hosts)])
		}
	})

	// stub_reporter: worst-case Phase 1 cost — reporter installed,
	// isFQDN evaluated, interface method called on fallbacks.
	b.Run("stub_reporter", func(b *testing.B) {
		eng := NewEngine(routes, Target(0), []*krs.Bundle{bundle}, false)
		defer eng.Close()
		eng.SetMissReporter(&stubReporter{})
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = eng.Match(hosts[i%len(hosts)])
		}
	})
}

// matchNoTelemetry is a test-only baseline method shaped exactly like
// Match() but without any rule-miss telemetry instrumentation. Used by
// BenchmarkMatchHostLocked to compute the overhead of the Phase 1
// atomic.Pointer load + nil check on the hot path.
func (e *Engine) matchNoTelemetry(host string) Target {
	e.mu.RLock()
	t := e.matchHostLocked(host)
	e.mu.RUnlock()
	return t
}

// mustBenchBundle mirrors mustBundle but takes a *testing.B.
func mustBenchBundle(b *testing.B, sets []krs.NamedSet) *krs.Bundle {
	b.Helper()
	var buf bytes.Buffer
	if err := krs.WriteBundle(&buf, &krs.Bundle{Sets: sets}); err != nil {
		b.Fatal(err)
	}
	bundle, err := krs.ReadBundle(buf.Bytes())
	if err != nil {
		b.Fatal(err)
	}
	return bundle
}

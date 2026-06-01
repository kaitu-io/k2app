package rule

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/kaitu-io/k2-rules/krs"
)

// captureSlog redirects slog default logger into a buffer for the duration of
// the test, restoring it on cleanup. Returns the buffer the test can search.
func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// mustBundle creates a krs.Bundle from BundleSets for testing.
func mustBundle(t *testing.T, sets []krs.NamedSet) *krs.Bundle {
	t.Helper()
	var buf bytes.Buffer
	if err := krs.WriteBundle(&buf, &krs.Bundle{Sets: sets}); err != nil {
		t.Fatal(err)
	}
	b, err := krs.ReadBundle(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// bindSets looks up BundleSets by name from an index.
func bindSets(idx map[string]*krs.NamedSet, names ...string) []krs.Matcher {
	var sets []krs.Matcher
	for _, n := range names {
		if s, ok := idx[n]; ok {
			sets = append(sets, s)
		}
	}
	return sets
}

func TestEngineMatchDomain(t *testing.T) {
	b := mustBundle(t, []krs.NamedSet{
		{Name: "google", DomainSuffixes: []string{"google.com", "youtube.com"}},
		{Name: "telegram", DomainSuffixes: []string{"telegram.org", "t.me"}},
	})
	idx := krs.Index([]*krs.Bundle{b})

	routes := []RouteEntry{
		{Target: Target(2), Sets: bindSets(idx, "google", "telegram")},
	}
	eng := NewEngine(routes, Target(0), []*krs.Bundle{b}, false)
	defer eng.Close()

	tests := []struct {
		host string
		want Target
	}{
		{"google.com", Target(2)},
		{"www.google.com", Target(2)},
		{"youtube.com", Target(2)},
		{"telegram.org", Target(2)},
		{"t.me", Target(2)},
		{"example.com", Target(0)},  // fallback
		{"unknown.org", Target(0)},  // fallback
	}
	for _, tt := range tests {
		if got := eng.Match(tt.host); got != tt.want {
			t.Errorf("Match(%q) = %v, want %v", tt.host, got, tt.want)
		}
	}
}

func TestEngineMatchIP(t *testing.T) {
	b := mustBundle(t, []krs.NamedSet{
		{Name: "google", CIDRs: []string{"8.8.8.0/24", "8.8.4.0/24"}},
		{Name: "geoip-cn", CIDRs: []string{"114.114.114.0/24", "223.5.5.0/24"}},
	})
	idx := krs.Index([]*krs.Bundle{b})

	routes := []RouteEntry{
		{Target: Target(2), Sets: bindSets(idx, "google")},
		{Target: Target(0), Sets: bindSets(idx, "geoip-cn")},
	}
	eng := NewEngine(routes, Target(1), []*krs.Bundle{b}, false)
	defer eng.Close()

	tests := []struct {
		host string
		want Target
	}{
		{"8.8.8.8", Target(2)},
		{"8.8.4.4", Target(2)},
		{"114.114.114.114", Target(0)},
		{"223.5.5.5", Target(0)},
		{"1.1.1.1", Target(1)}, // fallback
	}
	for _, tt := range tests {
		if got := eng.Match(tt.host); got != tt.want {
			t.Errorf("Match(%q) = %v, want %v", tt.host, got, tt.want)
		}
	}
}

func TestEngineSetTmpRule(t *testing.T) {
	b := mustBundle(t, []krs.NamedSet{
		{Name: "google", DomainSuffixes: []string{"google.com"}},
	})
	idx := krs.Index([]*krs.Bundle{b})

	routes := []RouteEntry{
		{Target: Target(2), Sets: bindSets(idx, "google")},
	}
	eng := NewEngine(routes, Target(0), []*krs.Bundle{b}, false)
	defer eng.Close()

	// 9.9.9.9 not in any set — should be direct (fallback).
	if got := eng.Match("9.9.9.9"); got != Target(0) {
		t.Fatalf("before SetTmpRule: Match(9.9.9.9) = %v, want direct", got)
	}

	// After SetTmpRule → proxy.
	eng.SetTmpRule("9.9.9.9", Target(2))
	if got := eng.Match("9.9.9.9"); got != Target(2) {
		t.Fatalf("after SetTmpRule: Match(9.9.9.9) = %v, want proxy", got)
	}
}

func TestEngineTmpRulePriority(t *testing.T) {
	b := mustBundle(t, []krs.NamedSet{
		{Name: "google", CIDRs: []string{"8.8.8.0/24"}},
	})
	idx := krs.Index([]*krs.Bundle{b})

	routes := []RouteEntry{
		{Target: Target(2), Sets: bindSets(idx, "google")},
	}
	eng := NewEngine(routes, Target(0), []*krs.Bundle{b}, false)
	defer eng.Close()

	// 8.8.8.8 matches static route → proxy.
	if got := eng.Match("8.8.8.8"); got != Target(2) {
		t.Fatalf("static: Match(8.8.8.8) = %v, want proxy", got)
	}

	// SetTmpRule overrides to direct.
	eng.SetTmpRule("8.8.8.8", Target(0))
	if got := eng.Match("8.8.8.8"); got != Target(0) {
		t.Fatalf("after SetTmpRule: Match(8.8.8.8) = %v, want direct", got)
	}
}

func TestEngineRouteOrder(t *testing.T) {
	b := mustBundle(t, []krs.NamedSet{
		{Name: "set-a", DomainSuffixes: []string{"overlap.com"}},
		{Name: "set-b", DomainSuffixes: []string{"overlap.com"}},
	})
	idx := krs.Index([]*krs.Bundle{b})

	routes := []RouteEntry{
		{Target: Target(1), Sets: bindSets(idx, "set-a")},  // first wins
		{Target: Target(2), Sets: bindSets(idx, "set-b")},
	}
	eng := NewEngine(routes, Target(0), []*krs.Bundle{b}, false)
	defer eng.Close()

	if got := eng.Match("overlap.com"); got != Target(1) {
		t.Errorf("Match(overlap.com) = %v, want reject (first route wins)", got)
	}
}

func TestEngineConcurrency(t *testing.T) {
	b := mustBundle(t, []krs.NamedSet{
		{Name: "google", DomainSuffixes: []string{"google.com"}, CIDRs: []string{"8.8.8.0/24"}},
	})
	idx := krs.Index([]*krs.Bundle{b})

	routes := []RouteEntry{
		{Target: Target(2), Sets: bindSets(idx, "google")},
	}
	eng := NewEngine(routes, Target(0), []*krs.Bundle{b}, false)
	defer eng.Close()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Mix of Match and SetTmpRule.
			eng.Match("google.com")
			eng.Match("8.8.8.8")
			eng.Match("unknown.example.com")
			eng.SetTmpRule("10.0.0.1", Target(2))
			eng.Match("10.0.0.1")
		}(i)
	}
	wg.Wait()
}

func TestEngineMatchConn_InlineDomain(t *testing.T) {
	inline := BuildInlineSet([]string{"custom-blocked.com"}, nil)
	routes := []RouteEntry{
		{Target: Target(2), Inline: &inline},
	}
	eng := NewEngine(routes, Target(0), nil, false)
	defer eng.Close()

	meta := ConnMeta{Network: "tcp"}
	if got := eng.MatchConn("custom-blocked.com", meta); got != Target(2) {
		t.Errorf("MatchConn(custom-blocked.com) = %v, want proxy", got)
	}
	if got := eng.MatchConn("www.custom-blocked.com", meta); got != Target(2) {
		t.Errorf("MatchConn(www.custom-blocked.com) = %v, want proxy", got)
	}
	if got := eng.MatchConn("other.com", meta); got != Target(0) {
		t.Errorf("MatchConn(other.com) = %v, want direct", got)
	}
}

func TestEngineMatchConn_InlineIP(t *testing.T) {
	inline := BuildInlineSet(nil, []string{"10.0.0.0/8"})
	routes := []RouteEntry{
		{Target: Target(0), Inline: &inline},
	}
	eng := NewEngine(routes, Target(2), nil, false)
	defer eng.Close()

	meta := ConnMeta{Network: "tcp"}
	if got := eng.MatchConn("10.1.2.3", meta); got != Target(0) {
		t.Errorf("MatchConn(10.1.2.3) = %v, want direct", got)
	}
	if got := eng.MatchConn("8.8.8.8", meta); got != Target(2) {
		t.Errorf("MatchConn(8.8.8.8) = %v, want proxy (fallback)", got)
	}
}

func TestEngineMatchConn_ProcessName(t *testing.T) {
	routes := []RouteEntry{
		{Target: Target(0), ProcessNames: []string{"ssh", "git"}},
	}
	eng := NewEngine(routes, Target(2), nil, false)
	defer eng.Close()

	if got := eng.MatchConn("8.8.8.8", ConnMeta{ProcessName: "ssh"}); got != Target(0) {
		t.Errorf("ssh = %v, want direct", got)
	}
	if got := eng.MatchConn("8.8.8.8", ConnMeta{ProcessName: "git"}); got != Target(0) {
		t.Errorf("git = %v, want direct", got)
	}
	if got := eng.MatchConn("8.8.8.8", ConnMeta{ProcessName: "chrome"}); got != Target(2) {
		t.Errorf("chrome = %v, want proxy", got)
	}
	// empty process → meta-only route requires meta → no match
	if got := eng.MatchConn("8.8.8.8", ConnMeta{}); got != Target(2) {
		t.Errorf("empty = %v, want proxy", got)
	}
}

// Linux /proc/PID/comm and macOS lsof COMMAND truncate process names to 15
// chars. UI-supplied rule names come from full executable basenames. Match
// must tolerate the truncation so App Bypass actually fires for apps like
// "Google Chrome Helper" (20 chars) where the daemon only ever sees
// "Google Chrome H".
func TestEngineMatchConn_ProcessName_KernelTruncation(t *testing.T) {
	routes := []RouteEntry{
		{Target: Target(0), ProcessNames: []string{
			"Google Chrome Helper",            // 20 chars — Chrome network helper
			"Microsoft PowerPoint",            // 20 chars
			"Code Helper (Renderer)",          // 22 chars — VS Code
			"firefox",                         // 7 chars — fits, exact-match path
		}},
	}
	eng := NewEngine(routes, Target(2), nil, false)
	defer eng.Close()

	// Daemon observes the 15-char truncation
	if got := eng.MatchConn("1.1.1.1", ConnMeta{ProcessName: "Google Chrome H"}); got != Target(0) {
		t.Errorf("truncated 'Google Chrome H' = %v, want direct (0)", got)
	}
	if got := eng.MatchConn("1.1.1.1", ConnMeta{ProcessName: "Microsoft Power"}); got != Target(0) {
		t.Errorf("truncated 'Microsoft Power' = %v, want direct (0)", got)
	}
	if got := eng.MatchConn("1.1.1.1", ConnMeta{ProcessName: "Code Helper (Re"}); got != Target(0) {
		t.Errorf("truncated 'Code Helper (Re' = %v, want direct (0)", got)
	}
	// Short name still works exact-match
	if got := eng.MatchConn("1.1.1.1", ConnMeta{ProcessName: "firefox"}); got != Target(0) {
		t.Errorf("exact 'firefox' = %v, want direct (0)", got)
	}
	// Non-truncation prefix collision: observed 14 chars matching rule prefix
	// must NOT match (would over-match unrelated processes).
	if got := eng.MatchConn("1.1.1.1", ConnMeta{ProcessName: "Google Chrome"}); got != Target(2) {
		t.Errorf("non-truncated 'Google Chrome' (13 chars) should NOT match 20-char rule = %v, want proxy (2)", got)
	}
	// Truncation only when observed is exactly 15 chars AND matches rule prefix
	if got := eng.MatchConn("1.1.1.1", ConnMeta{ProcessName: "Slack Helper (R"}); got != Target(2) {
		t.Errorf("15-char truncation of unrelated process = %v, want proxy (2)", got)
	}
}

func TestEngineMatchConn_PackageName(t *testing.T) {
	routes := []RouteEntry{
		{Target: Target(2), PackageNames: []string{"com.android.chrome"}},
	}
	eng := NewEngine(routes, Target(0), nil, false)
	defer eng.Close()

	if got := eng.MatchConn("1.2.3.4", ConnMeta{PackageName: "com.android.chrome"}); got != Target(2) {
		t.Errorf("chrome = %v, want proxy", got)
	}
	if got := eng.MatchConn("1.2.3.4", ConnMeta{PackageName: "com.tencent.mm"}); got != Target(0) {
		t.Errorf("wechat = %v, want direct", got)
	}
}

func TestEngineMatchConn_Network(t *testing.T) {
	routes := []RouteEntry{
		{Target: Target(0), NetworkMatch: "udp"},
	}
	eng := NewEngine(routes, Target(2), nil, false)
	defer eng.Close()

	if got := eng.MatchConn("1.2.3.4", ConnMeta{Network: "udp"}); got != Target(0) {
		t.Errorf("udp = %v, want direct", got)
	}
	if got := eng.MatchConn("1.2.3.4", ConnMeta{Network: "tcp"}); got != Target(2) {
		t.Errorf("tcp = %v, want proxy", got)
	}
}

func TestEngineMatchConn_IPIsPrivate(t *testing.T) {
	routes := []RouteEntry{
		{Target: Target(0), IPIsPrivate: true},
	}
	eng := NewEngine(routes, Target(2), nil, false)
	defer eng.Close()

	meta := ConnMeta{Network: "tcp"}
	if got := eng.MatchConn("192.168.1.1", meta); got != Target(0) {
		t.Errorf("192.168.1.1 = %v, want direct", got)
	}
	if got := eng.MatchConn("10.0.0.1", meta); got != Target(0) {
		t.Errorf("10.0.0.1 = %v, want direct", got)
	}
	if got := eng.MatchConn("127.0.0.1", meta); got != Target(0) {
		t.Errorf("127.0.0.1 = %v, want direct", got)
	}
	if got := eng.MatchConn("8.8.8.8", meta); got != Target(2) {
		t.Errorf("8.8.8.8 = %v, want proxy", got)
	}
	// domain: ip_is_private does not apply
	if got := eng.MatchConn("google.com", meta); got != Target(2) {
		t.Errorf("google.com = %v, want proxy", got)
	}
}

func TestEngineMatchConn_AND(t *testing.T) {
	b := mustBundle(t, []krs.NamedSet{
		{Name: "google", DomainSuffixes: []string{"google.com"}},
	})
	idx := krs.Index([]*krs.Bundle{b})
	routes := []RouteEntry{
		{Target: Target(2), Sets: bindSets(idx, "google"), ProcessNames: []string{"Chrome"}},
	}
	eng := NewEngine(routes, Target(0), []*krs.Bundle{b}, false)
	defer eng.Close()

	if got := eng.MatchConn("google.com", ConnMeta{ProcessName: "Chrome"}); got != Target(2) {
		t.Errorf("chrome+google = %v, want proxy", got)
	}
	if got := eng.MatchConn("google.com", ConnMeta{ProcessName: "ssh"}); got != Target(0) {
		t.Errorf("ssh+google = %v, want direct", got)
	}
	if got := eng.MatchConn("example.com", ConnMeta{ProcessName: "Chrome"}); got != Target(0) {
		t.Errorf("chrome+example = %v, want direct", got)
	}
}

func TestEngineMatch_IgnoresMetaOnlyRoutes(t *testing.T) {
	routes := []RouteEntry{
		{Target: Target(0), ProcessNames: []string{"ssh"}},
	}
	eng := NewEngine(routes, Target(2), nil, false)
	defer eng.Close()

	if got := eng.Match("anything.com"); got != Target(2) {
		t.Errorf("Match(anything.com) = %v, want proxy (fallback)", got)
	}
}

func TestEngineIsAllProxy(t *testing.T) {
	eng := NewEngine(nil, Target(2), nil, true)
	defer eng.Close()
	if !eng.IsAllProxy() {
		t.Error("IsAllProxy() = false, want true")
	}

	eng2 := NewEngine(nil, Target(0), nil, false)
	defer eng2.Close()
	if eng2.IsAllProxy() {
		t.Error("IsAllProxy() = true, want false")
	}
}

func TestEngineReload(t *testing.T) {
	b1 := mustBundle(t, []krs.NamedSet{
		{Name: "google", DomainSuffixes: []string{"google.com"}},
	})
	idx1 := krs.Index([]*krs.Bundle{b1})

	routes1 := []RouteEntry{
		{Target: Target(2), Sets: bindSets(idx1, "google")},
	}
	eng := NewEngine(routes1, Target(0), []*krs.Bundle{b1}, false)
	defer eng.Close()

	// Before reload: google→proxy, facebook→direct (fallback).
	if got := eng.Match("google.com"); got != Target(2) {
		t.Fatalf("before reload: Match(google.com) = %v, want proxy", got)
	}
	if got := eng.Match("facebook.com"); got != Target(0) {
		t.Fatalf("before reload: Match(facebook.com) = %v, want direct", got)
	}

	// Reload: facebook→proxy, google removed.
	b2 := mustBundle(t, []krs.NamedSet{
		{Name: "facebook", DomainSuffixes: []string{"facebook.com"}},
	})
	idx2 := krs.Index([]*krs.Bundle{b2})
	routes2 := []RouteEntry{
		{Target: Target(2), Sets: bindSets(idx2, "facebook")},
	}
	if err := eng.Reload(routes2, []*krs.Bundle{b2}); err != nil {
		t.Fatalf("Reload() error: %v", err)
	}

	// After reload: google→direct (fallback), facebook→proxy.
	if got := eng.Match("google.com"); got != Target(0) {
		t.Errorf("after reload: Match(google.com) = %v, want direct", got)
	}
	if got := eng.Match("facebook.com"); got != Target(2) {
		t.Errorf("after reload: Match(facebook.com) = %v, want proxy", got)
	}
}

func TestEngineReload_ClearsTmpRules(t *testing.T) {
	b := mustBundle(t, []krs.NamedSet{
		{Name: "google", DomainSuffixes: []string{"google.com"}},
	})
	idx := krs.Index([]*krs.Bundle{b})

	routes := []RouteEntry{
		{Target: Target(2), Sets: bindSets(idx, "google")},
	}
	eng := NewEngine(routes, Target(0), []*krs.Bundle{b}, false)
	defer eng.Close()

	// Set a tmp rule for 8.8.8.8 → proxy.
	eng.SetTmpRule("8.8.8.8", Target(2))
	if got := eng.Match("8.8.8.8"); got != Target(2) {
		t.Fatalf("before reload: Match(8.8.8.8) = %v, want proxy", got)
	}

	// Reload with the same routes — tmp rules must be cleared.
	if err := eng.Reload(routes, []*krs.Bundle{b}); err != nil {
		t.Fatalf("Reload() error: %v", err)
	}
	if got := eng.Match("8.8.8.8"); got != Target(0) {
		t.Errorf("after reload: Match(8.8.8.8) = %v, want direct (tmp cleared)", got)
	}
}

func TestRouteEntry_HasMetaCriteria_NewFields(t *testing.T) {
	tests := []struct {
		name string
		r    RouteEntry
	}{
		{"process_name_prefixes", RouteEntry{ProcessNamePrefixes: []string{"wechat"}}},
		{"package_name_prefixes", RouteEntry{PackageNamePrefixes: []string{"com.tencent."}}},
		{"installer_packages",    RouteEntry{InstallerPackages: []string{"com.xiaomi.market"}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if !tc.r.hasMetaCriteria() {
				t.Errorf("hasMetaCriteria should be true for %s", tc.name)
			}
		})
	}
}

func TestEngineReload_Concurrent(t *testing.T) {
	b := mustBundle(t, []krs.NamedSet{
		{Name: "google", DomainSuffixes: []string{"google.com"}, CIDRs: []string{"8.8.8.0/24"}},
	})
	idx := krs.Index([]*krs.Bundle{b})

	routes := []RouteEntry{
		{Target: Target(2), Sets: bindSets(idx, "google")},
	}
	eng := NewEngine(routes, Target(0), []*krs.Bundle{b}, false)
	defer eng.Close()

	var wg sync.WaitGroup

	// 100 goroutines doing Match + MatchConn.
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			eng.Match("google.com")
			eng.Match("8.8.8.8")
			eng.Match("example.com")
			eng.MatchConn("google.com", ConnMeta{Network: "tcp"})
			eng.MatchConn("8.8.8.8", ConnMeta{Network: "udp"})
		}()
	}

	// 10 goroutines doing Reload.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b2 := mustBundle(t, []krs.NamedSet{
				{Name: "google", DomainSuffixes: []string{"google.com"}, CIDRs: []string{"8.8.8.0/24"}},
			})
			idx2 := krs.Index([]*krs.Bundle{b2})
			r2 := []RouteEntry{
				{Target: Target(2), Sets: bindSets(idx2, "google")},
			}
			if err := eng.Reload(r2, []*krs.Bundle{b2}); err != nil {
				t.Errorf("Reload() error: %v", err)
			}
		}()
	}

	wg.Wait()
}

func TestEngineSetTmpRule_DIAGNewPin(t *testing.T) {
	buf := captureSlog(t)
	eng := NewEngine(nil, Target(0), nil, false)
	defer eng.Close()

	eng.SetTmpRule("8.8.8.8", Target(2))

	out := buf.String()
	if !strings.Contains(out, "DIAG: rule-tmp-pin") {
		t.Fatalf("missing rule-tmp-pin log:\n%s", out)
	}
	if !strings.Contains(out, `ip=8.8.8.8`) {
		t.Errorf("log missing ip field:\n%s", out)
	}
	if !strings.Contains(out, `target=2`) {
		t.Errorf("log missing target field:\n%s", out)
	}
	if !strings.Contains(out, `action=add`) {
		t.Errorf("log missing action=add:\n%s", out)
	}
}

func TestEngineSetTmpRule_DIAGDuplicateSuppressed(t *testing.T) {
	// Same ip + same target called twice. Second call must NOT emit a log —
	// DNS pipeline re-pins on every cache hit and we don't want a flood.
	buf := captureSlog(t)
	eng := NewEngine(nil, Target(0), nil, false)
	defer eng.Close()

	eng.SetTmpRule("8.8.8.8", Target(2))
	firstLen := buf.Len()
	eng.SetTmpRule("8.8.8.8", Target(2)) // duplicate
	secondLen := buf.Len()

	if secondLen != firstLen {
		t.Fatalf("duplicate SetTmpRule emitted log (delta %d bytes):\n%s",
			secondLen-firstLen, buf.String()[firstLen:])
	}
}

func TestEngineSetTmpRule_DIAGTargetChange(t *testing.T) {
	buf := captureSlog(t)
	eng := NewEngine(nil, Target(0), nil, false)
	defer eng.Close()

	eng.SetTmpRule("8.8.8.8", Target(2)) // initial pin
	buf.Reset()                          // discard add log
	eng.SetTmpRule("8.8.8.8", Target(0)) // change to direct

	out := buf.String()
	if !strings.Contains(out, "DIAG: rule-tmp-pin") {
		t.Fatalf("change did not emit rule-tmp-pin:\n%s", out)
	}
	if !strings.Contains(out, `action=change`) {
		t.Errorf("log missing action=change:\n%s", out)
	}
	if !strings.Contains(out, `prevTarget=2`) {
		t.Errorf("log missing prevTarget=2:\n%s", out)
	}
}

func TestEngineSetTmpRule_DIAGInvalidIPSilent(t *testing.T) {
	// An unparseable IP must produce neither a pin nor a log.
	buf := captureSlog(t)
	eng := NewEngine(nil, Target(0), nil, false)
	defer eng.Close()

	eng.SetTmpRule("not.an.ip.address", Target(2))

	if strings.Contains(buf.String(), "DIAG:") {
		t.Fatalf("invalid IP emitted DIAG:\n%s", buf.String())
	}
}

func TestEngineReload_DIAGEmitsCleared(t *testing.T) {
	buf := captureSlog(t)
	eng := NewEngine(nil, Target(0), nil, false)
	defer eng.Close()

	eng.SetTmpRule("1.1.1.1", Target(2))
	eng.SetTmpRule("2.2.2.2", Target(2))
	buf.Reset() // discard pin logs

	if err := eng.Reload(nil, nil); err != nil {
		t.Fatalf("Reload error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "DIAG: rule-tmp-reload") {
		t.Fatalf("missing rule-tmp-reload log:\n%s", out)
	}
	if !strings.Contains(out, `cleared=2`) {
		t.Errorf("expected cleared=2 (we pinned 2 IPs):\n%s", out)
	}
}

func TestMatchMeta_ProcessNamePrefix_Hit(t *testing.T) {
	// Entries pre-lowercased at compile; matcher lowercases meta.ProcessName.
	r := &RouteEntry{ProcessNamePrefixes: []string{"wechat"}}
	for _, name := range []string{"WeChat", "WeChatHelper", "wechat", "WeChat.exe", "WeChatStart.exe"} {
		if !matchMeta(r, ConnMeta{ProcessName: name}) {
			t.Errorf("prefix 'wechat' should match %q", name)
		}
	}
}

func TestMatchMeta_ProcessNamePrefix_Miss(t *testing.T) {
	r := &RouteEntry{ProcessNamePrefixes: []string{"wechat"}}
	if matchMeta(r, ConnMeta{ProcessName: "Telegram"}) {
		t.Errorf("prefix 'wechat' should NOT match 'Telegram'")
	}
	if matchMeta(r, ConnMeta{ProcessName: ""}) {
		t.Errorf("prefix should NOT match empty process name")
	}
}

func TestMatchMeta_PackageNamePrefix_CaseSensitive(t *testing.T) {
	r := &RouteEntry{PackageNamePrefixes: []string{"com.tencent."}}
	if !matchMeta(r, ConnMeta{PackageName: "com.tencent.mm"}) {
		t.Errorf("expected match on com.tencent.mm")
	}
	if matchMeta(r, ConnMeta{PackageName: "com.Tencent.mm"}) {
		t.Errorf("package prefix should be case-sensitive (capital T)")
	}
}

func TestMatchMeta_InstallerPackage_Hit(t *testing.T) {
	r := &RouteEntry{InstallerPackages: []string{"com.xiaomi.market", "com.huawei.appmarket"}}
	if !matchMeta(r, ConnMeta{InstallerPackage: "com.xiaomi.market"}) {
		t.Errorf("should match Xiaomi installer")
	}
	if matchMeta(r, ConnMeta{InstallerPackage: "com.google.android.packageinstaller"}) {
		t.Errorf("should NOT match Google installer")
	}
}

func TestMatchMeta_OrSemanticsWithinPackageGroup(t *testing.T) {
	// Within the package group (PackageNames / PackageNamePrefixes / InstallerPackages),
	// ANY hit means group satisfied. Across groups (process/package/network),
	// AND semantics still apply.
	r := &RouteEntry{
		ProcessNamePrefixes: []string{"wechat"},
		InstallerPackages:   []string{"com.xiaomi.market"},
	}
	// Process matches → group ok; package group has criteria but no field set → fail.
	if matchMeta(r, ConnMeta{ProcessName: "WeChat"}) {
		t.Errorf("missing installer/package fields with installer criterion set should fail")
	}
	// Both match
	if !matchMeta(r, ConnMeta{ProcessName: "WeChat", InstallerPackage: "com.xiaomi.market"}) {
		t.Errorf("both groups satisfied should pass")
	}
	// Only installer (no process — but process group has criteria)
	if matchMeta(r, ConnMeta{InstallerPackage: "com.xiaomi.market"}) {
		t.Errorf("process group has criteria + empty process → fail")
	}
}

func TestMatchMeta_OrWithinProcessGroup(t *testing.T) {
	// Route has BOTH exact and prefix entries. Either alone should satisfy.
	r := &RouteEntry{
		ProcessNames:        []string{"Telegram"},
		ProcessNamePrefixes: []string{"wechat"},
	}
	// Exact-only hit.
	if !matchMeta(r, ConnMeta{ProcessName: "Telegram"}) {
		t.Errorf("exact ProcessNames match should satisfy process group")
	}
	// Prefix-only hit.
	if !matchMeta(r, ConnMeta{ProcessName: "WeChat"}) {
		t.Errorf("prefix ProcessNamePrefixes match should satisfy process group")
	}
	// Neither.
	if matchMeta(r, ConnMeta{ProcessName: "Firefox"}) {
		t.Errorf("non-matching name should fail process group")
	}
}

func TestMatchMeta_OrWithinPackageGroup(t *testing.T) {
	// Route has all three kinds of package criteria. Any one match satisfies.
	r := &RouteEntry{
		PackageNames:        []string{"com.exact.app"},
		PackageNamePrefixes: []string{"com.prefix."},
		InstallerPackages:   []string{"com.xiaomi.market"},
	}
	// Exact-only hit.
	if !matchMeta(r, ConnMeta{PackageName: "com.exact.app"}) {
		t.Errorf("PackageNames exact should satisfy")
	}
	// Prefix-only hit.
	if !matchMeta(r, ConnMeta{PackageName: "com.prefix.foo"}) {
		t.Errorf("PackageNamePrefixes prefix should satisfy")
	}
	// Installer-only hit (PackageName non-empty but doesn't match exact/prefix).
	if !matchMeta(r, ConnMeta{PackageName: "com.unrelated.app", InstallerPackage: "com.xiaomi.market"}) {
		t.Errorf("InstallerPackages match should satisfy even when PackageName misses")
	}
	// No hits.
	if matchMeta(r, ConnMeta{PackageName: "com.unrelated.app", InstallerPackage: "com.google.android.packageinstaller"}) {
		t.Errorf("all three lists missing → should fail")
	}
}

func TestEngine_ReloadNamed(t *testing.T) {
	cn := mustBundle(t, []krs.NamedSet{{Name: "cn-sites", DomainSuffixes: []string{"baidu.com"}}})
	e := NewEngineNamed(nil, Target(0), []NamedBundle{{Region: "cn", Bundle: cn}}, false)
	defer e.Close()

	// ReloadNamed flattens NamedBundles and hot-swaps routes + bundles.
	th := mustBundle(t, []krs.NamedSet{{Name: "th-sites", DomainSuffixes: []string{"pantip.com"}}})
	newRoutes := []RouteEntry{{}, {}}
	if err := e.ReloadNamed(newRoutes, []NamedBundle{{Region: "th", Bundle: th}}); err != nil {
		t.Fatalf("ReloadNamed: %v", err)
	}
	if got := e.RouteCount(); got != len(newRoutes) {
		t.Errorf("RouteCount after reload = %d, want %d", got, len(newRoutes))
	}
}

// TestEngine_MatchDomain_OverMatcherInterface proves the engine routes domains
// when RouteEntry.Sets holds a krs.Matcher (the type both the heap *NamedSet and
// the mmap *diskSet satisfy). Drives the []krs.Matcher migration + ReversedParents hoist.
func TestEngine_MatchDomain_OverMatcherInterface(t *testing.T) {
	var buf bytes.Buffer
	if err := krs.WriteBundle(&buf, &krs.Bundle{Sets: []krs.NamedSet{
		{Name: "t", DomainSuffixes: []string{"example.com"}, CIDRs: []string{"1.2.3.0/24"}},
	}}); err != nil {
		t.Fatal(err)
	}
	b, err := krs.ReadBundle(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	var m krs.Matcher = &b.Sets[0]
	e := NewEngine([]RouteEntry{{Target: 2, Sets: []krs.Matcher{m}}}, 0, nil, false)

	if got := e.Match("www.example.com"); got != 2 {
		t.Fatalf("domain: want 2 got %d", got)
	}
	if got := e.Match("other.org"); got != 0 {
		t.Fatalf("domain fallback: want 0 got %d", got)
	}
	if got := e.Match("1.2.3.4"); got != 2 {
		t.Fatalf("ip: want 2 got %d", got)
	}
}

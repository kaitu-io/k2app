package rule

import (
	"bytes"
	"sync"
	"testing"
)

// mustBundle creates a Bundle from BundleSets for testing.
func mustBundle(t *testing.T, sets []BundleSet) *Bundle {
	t.Helper()
	var buf bytes.Buffer
	if err := WriteBundle(&buf, sets); err != nil {
		t.Fatal(err)
	}
	b, err := ReadBundle(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// bindSets looks up BundleSets by name from an index.
func bindSets(idx map[string]*BundleSet, names ...string) []*BundleSet {
	var sets []*BundleSet
	for _, n := range names {
		if s, ok := idx[n]; ok {
			sets = append(sets, s)
		}
	}
	return sets
}

func TestEngineMatchDomain(t *testing.T) {
	b := mustBundle(t, []BundleSet{
		{Name: "google", DomainSuffixes: []string{"google.com", "youtube.com"}},
		{Name: "telegram", DomainSuffixes: []string{"telegram.org", "t.me"}},
	})
	idx := Index([]*Bundle{b})

	routes := []RouteEntry{
		{Target: TargetProxy, Sets: bindSets(idx, "google", "telegram")},
	}
	eng := NewEngine(routes, TargetDirect, []*Bundle{b}, false)
	defer eng.Close()

	tests := []struct {
		host string
		want Target
	}{
		{"google.com", TargetProxy},
		{"www.google.com", TargetProxy},
		{"youtube.com", TargetProxy},
		{"telegram.org", TargetProxy},
		{"t.me", TargetProxy},
		{"example.com", TargetDirect},  // fallback
		{"unknown.org", TargetDirect},  // fallback
	}
	for _, tt := range tests {
		if got := eng.Match(tt.host); got != tt.want {
			t.Errorf("Match(%q) = %v, want %v", tt.host, got, tt.want)
		}
	}
}

func TestEngineMatchIP(t *testing.T) {
	b := mustBundle(t, []BundleSet{
		{Name: "google", CIDRs: []string{"8.8.8.0/24", "8.8.4.0/24"}},
		{Name: "geoip-cn", CIDRs: []string{"114.114.114.0/24", "223.5.5.0/24"}},
	})
	idx := Index([]*Bundle{b})

	routes := []RouteEntry{
		{Target: TargetProxy, Sets: bindSets(idx, "google")},
		{Target: TargetDirect, Sets: bindSets(idx, "geoip-cn")},
	}
	eng := NewEngine(routes, TargetReject, []*Bundle{b}, false)
	defer eng.Close()

	tests := []struct {
		host string
		want Target
	}{
		{"8.8.8.8", TargetProxy},
		{"8.8.4.4", TargetProxy},
		{"114.114.114.114", TargetDirect},
		{"223.5.5.5", TargetDirect},
		{"1.1.1.1", TargetReject}, // fallback
	}
	for _, tt := range tests {
		if got := eng.Match(tt.host); got != tt.want {
			t.Errorf("Match(%q) = %v, want %v", tt.host, got, tt.want)
		}
	}
}

func TestEngineSetTmpRule(t *testing.T) {
	b := mustBundle(t, []BundleSet{
		{Name: "google", DomainSuffixes: []string{"google.com"}},
	})
	idx := Index([]*Bundle{b})

	routes := []RouteEntry{
		{Target: TargetProxy, Sets: bindSets(idx, "google")},
	}
	eng := NewEngine(routes, TargetDirect, []*Bundle{b}, false)
	defer eng.Close()

	// 9.9.9.9 not in any set — should be direct (fallback).
	if got := eng.Match("9.9.9.9"); got != TargetDirect {
		t.Fatalf("before SetTmpRule: Match(9.9.9.9) = %v, want direct", got)
	}

	// After SetTmpRule → proxy.
	eng.SetTmpRule("9.9.9.9", TargetProxy)
	if got := eng.Match("9.9.9.9"); got != TargetProxy {
		t.Fatalf("after SetTmpRule: Match(9.9.9.9) = %v, want proxy", got)
	}
}

func TestEngineTmpRulePriority(t *testing.T) {
	b := mustBundle(t, []BundleSet{
		{Name: "google", CIDRs: []string{"8.8.8.0/24"}},
	})
	idx := Index([]*Bundle{b})

	routes := []RouteEntry{
		{Target: TargetProxy, Sets: bindSets(idx, "google")},
	}
	eng := NewEngine(routes, TargetDirect, []*Bundle{b}, false)
	defer eng.Close()

	// 8.8.8.8 matches static route → proxy.
	if got := eng.Match("8.8.8.8"); got != TargetProxy {
		t.Fatalf("static: Match(8.8.8.8) = %v, want proxy", got)
	}

	// SetTmpRule overrides to direct.
	eng.SetTmpRule("8.8.8.8", TargetDirect)
	if got := eng.Match("8.8.8.8"); got != TargetDirect {
		t.Fatalf("after SetTmpRule: Match(8.8.8.8) = %v, want direct", got)
	}
}

func TestEngineRouteOrder(t *testing.T) {
	b := mustBundle(t, []BundleSet{
		{Name: "set-a", DomainSuffixes: []string{"overlap.com"}},
		{Name: "set-b", DomainSuffixes: []string{"overlap.com"}},
	})
	idx := Index([]*Bundle{b})

	routes := []RouteEntry{
		{Target: TargetReject, Sets: bindSets(idx, "set-a")},  // first wins
		{Target: TargetProxy, Sets: bindSets(idx, "set-b")},
	}
	eng := NewEngine(routes, TargetDirect, []*Bundle{b}, false)
	defer eng.Close()

	if got := eng.Match("overlap.com"); got != TargetReject {
		t.Errorf("Match(overlap.com) = %v, want reject (first route wins)", got)
	}
}

func TestEngineConcurrency(t *testing.T) {
	b := mustBundle(t, []BundleSet{
		{Name: "google", DomainSuffixes: []string{"google.com"}, CIDRs: []string{"8.8.8.0/24"}},
	})
	idx := Index([]*Bundle{b})

	routes := []RouteEntry{
		{Target: TargetProxy, Sets: bindSets(idx, "google")},
	}
	eng := NewEngine(routes, TargetDirect, []*Bundle{b}, false)
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
			eng.SetTmpRule("10.0.0.1", TargetProxy)
			eng.Match("10.0.0.1")
		}(i)
	}
	wg.Wait()
}

func TestEngineMatchConn_InlineDomain(t *testing.T) {
	inline := BuildInlineSet([]string{"custom-blocked.com"}, nil)
	routes := []RouteEntry{
		{Target: TargetProxy, Inline: &inline},
	}
	eng := NewEngine(routes, TargetDirect, nil, false)
	defer eng.Close()

	meta := ConnMeta{Network: "tcp"}
	if got := eng.MatchConn("custom-blocked.com", meta); got != TargetProxy {
		t.Errorf("MatchConn(custom-blocked.com) = %v, want proxy", got)
	}
	if got := eng.MatchConn("www.custom-blocked.com", meta); got != TargetProxy {
		t.Errorf("MatchConn(www.custom-blocked.com) = %v, want proxy", got)
	}
	if got := eng.MatchConn("other.com", meta); got != TargetDirect {
		t.Errorf("MatchConn(other.com) = %v, want direct", got)
	}
}

func TestEngineMatchConn_InlineIP(t *testing.T) {
	inline := BuildInlineSet(nil, []string{"10.0.0.0/8"})
	routes := []RouteEntry{
		{Target: TargetDirect, Inline: &inline},
	}
	eng := NewEngine(routes, TargetProxy, nil, false)
	defer eng.Close()

	meta := ConnMeta{Network: "tcp"}
	if got := eng.MatchConn("10.1.2.3", meta); got != TargetDirect {
		t.Errorf("MatchConn(10.1.2.3) = %v, want direct", got)
	}
	if got := eng.MatchConn("8.8.8.8", meta); got != TargetProxy {
		t.Errorf("MatchConn(8.8.8.8) = %v, want proxy (fallback)", got)
	}
}

func TestEngineMatchConn_ProcessName(t *testing.T) {
	routes := []RouteEntry{
		{Target: TargetDirect, ProcessNames: []string{"ssh", "git"}},
	}
	eng := NewEngine(routes, TargetProxy, nil, false)
	defer eng.Close()

	if got := eng.MatchConn("8.8.8.8", ConnMeta{ProcessName: "ssh"}); got != TargetDirect {
		t.Errorf("ssh = %v, want direct", got)
	}
	if got := eng.MatchConn("8.8.8.8", ConnMeta{ProcessName: "git"}); got != TargetDirect {
		t.Errorf("git = %v, want direct", got)
	}
	if got := eng.MatchConn("8.8.8.8", ConnMeta{ProcessName: "chrome"}); got != TargetProxy {
		t.Errorf("chrome = %v, want proxy", got)
	}
	// empty process → meta-only route requires meta → no match
	if got := eng.MatchConn("8.8.8.8", ConnMeta{}); got != TargetProxy {
		t.Errorf("empty = %v, want proxy", got)
	}
}

func TestEngineMatchConn_PackageName(t *testing.T) {
	routes := []RouteEntry{
		{Target: TargetProxy, PackageNames: []string{"com.android.chrome"}},
	}
	eng := NewEngine(routes, TargetDirect, nil, false)
	defer eng.Close()

	if got := eng.MatchConn("1.2.3.4", ConnMeta{PackageName: "com.android.chrome"}); got != TargetProxy {
		t.Errorf("chrome = %v, want proxy", got)
	}
	if got := eng.MatchConn("1.2.3.4", ConnMeta{PackageName: "com.tencent.mm"}); got != TargetDirect {
		t.Errorf("wechat = %v, want direct", got)
	}
}

func TestEngineMatchConn_Network(t *testing.T) {
	routes := []RouteEntry{
		{Target: TargetDirect, NetworkMatch: "udp"},
	}
	eng := NewEngine(routes, TargetProxy, nil, false)
	defer eng.Close()

	if got := eng.MatchConn("1.2.3.4", ConnMeta{Network: "udp"}); got != TargetDirect {
		t.Errorf("udp = %v, want direct", got)
	}
	if got := eng.MatchConn("1.2.3.4", ConnMeta{Network: "tcp"}); got != TargetProxy {
		t.Errorf("tcp = %v, want proxy", got)
	}
}

func TestEngineMatchConn_IPIsPrivate(t *testing.T) {
	routes := []RouteEntry{
		{Target: TargetDirect, IPIsPrivate: true},
	}
	eng := NewEngine(routes, TargetProxy, nil, false)
	defer eng.Close()

	meta := ConnMeta{Network: "tcp"}
	if got := eng.MatchConn("192.168.1.1", meta); got != TargetDirect {
		t.Errorf("192.168.1.1 = %v, want direct", got)
	}
	if got := eng.MatchConn("10.0.0.1", meta); got != TargetDirect {
		t.Errorf("10.0.0.1 = %v, want direct", got)
	}
	if got := eng.MatchConn("127.0.0.1", meta); got != TargetDirect {
		t.Errorf("127.0.0.1 = %v, want direct", got)
	}
	if got := eng.MatchConn("8.8.8.8", meta); got != TargetProxy {
		t.Errorf("8.8.8.8 = %v, want proxy", got)
	}
	// domain: ip_is_private does not apply
	if got := eng.MatchConn("google.com", meta); got != TargetProxy {
		t.Errorf("google.com = %v, want proxy", got)
	}
}

func TestEngineMatchConn_AND(t *testing.T) {
	b := mustBundle(t, []BundleSet{
		{Name: "google", DomainSuffixes: []string{"google.com"}},
	})
	idx := Index([]*Bundle{b})
	routes := []RouteEntry{
		{Target: TargetProxy, Sets: bindSets(idx, "google"), ProcessNames: []string{"Chrome"}},
	}
	eng := NewEngine(routes, TargetDirect, []*Bundle{b}, false)
	defer eng.Close()

	if got := eng.MatchConn("google.com", ConnMeta{ProcessName: "Chrome"}); got != TargetProxy {
		t.Errorf("chrome+google = %v, want proxy", got)
	}
	if got := eng.MatchConn("google.com", ConnMeta{ProcessName: "ssh"}); got != TargetDirect {
		t.Errorf("ssh+google = %v, want direct", got)
	}
	if got := eng.MatchConn("example.com", ConnMeta{ProcessName: "Chrome"}); got != TargetDirect {
		t.Errorf("chrome+example = %v, want direct", got)
	}
}

func TestEngineMatch_IgnoresMetaOnlyRoutes(t *testing.T) {
	routes := []RouteEntry{
		{Target: TargetDirect, ProcessNames: []string{"ssh"}},
	}
	eng := NewEngine(routes, TargetProxy, nil, false)
	defer eng.Close()

	if got := eng.Match("anything.com"); got != TargetProxy {
		t.Errorf("Match(anything.com) = %v, want proxy (fallback)", got)
	}
}

func TestEngineIsAllProxy(t *testing.T) {
	eng := NewEngine(nil, TargetProxy, nil, true)
	defer eng.Close()
	if !eng.IsAllProxy() {
		t.Error("IsAllProxy() = false, want true")
	}

	eng2 := NewEngine(nil, TargetDirect, nil, false)
	defer eng2.Close()
	if eng2.IsAllProxy() {
		t.Error("IsAllProxy() = true, want false")
	}
}

func TestEngineReload(t *testing.T) {
	b1 := mustBundle(t, []BundleSet{
		{Name: "google", DomainSuffixes: []string{"google.com"}},
	})
	idx1 := Index([]*Bundle{b1})

	routes1 := []RouteEntry{
		{Target: TargetProxy, Sets: bindSets(idx1, "google")},
	}
	eng := NewEngine(routes1, TargetDirect, []*Bundle{b1}, false)
	defer eng.Close()

	// Before reload: google→proxy, facebook→direct (fallback).
	if got := eng.Match("google.com"); got != TargetProxy {
		t.Fatalf("before reload: Match(google.com) = %v, want proxy", got)
	}
	if got := eng.Match("facebook.com"); got != TargetDirect {
		t.Fatalf("before reload: Match(facebook.com) = %v, want direct", got)
	}

	// Reload: facebook→proxy, google removed.
	b2 := mustBundle(t, []BundleSet{
		{Name: "facebook", DomainSuffixes: []string{"facebook.com"}},
	})
	idx2 := Index([]*Bundle{b2})
	routes2 := []RouteEntry{
		{Target: TargetProxy, Sets: bindSets(idx2, "facebook")},
	}
	if err := eng.Reload(routes2, []*Bundle{b2}); err != nil {
		t.Fatalf("Reload() error: %v", err)
	}

	// After reload: google→direct (fallback), facebook→proxy.
	if got := eng.Match("google.com"); got != TargetDirect {
		t.Errorf("after reload: Match(google.com) = %v, want direct", got)
	}
	if got := eng.Match("facebook.com"); got != TargetProxy {
		t.Errorf("after reload: Match(facebook.com) = %v, want proxy", got)
	}
}

func TestEngineReload_ClearsTmpRules(t *testing.T) {
	b := mustBundle(t, []BundleSet{
		{Name: "google", DomainSuffixes: []string{"google.com"}},
	})
	idx := Index([]*Bundle{b})

	routes := []RouteEntry{
		{Target: TargetProxy, Sets: bindSets(idx, "google")},
	}
	eng := NewEngine(routes, TargetDirect, []*Bundle{b}, false)
	defer eng.Close()

	// Set a tmp rule for 8.8.8.8 → proxy.
	eng.SetTmpRule("8.8.8.8", TargetProxy)
	if got := eng.Match("8.8.8.8"); got != TargetProxy {
		t.Fatalf("before reload: Match(8.8.8.8) = %v, want proxy", got)
	}

	// Reload with the same routes — tmp rules must be cleared.
	if err := eng.Reload(routes, []*Bundle{b}); err != nil {
		t.Fatalf("Reload() error: %v", err)
	}
	if got := eng.Match("8.8.8.8"); got != TargetDirect {
		t.Errorf("after reload: Match(8.8.8.8) = %v, want direct (tmp cleared)", got)
	}
}

func TestEngineReload_Concurrent(t *testing.T) {
	b := mustBundle(t, []BundleSet{
		{Name: "google", DomainSuffixes: []string{"google.com"}, CIDRs: []string{"8.8.8.0/24"}},
	})
	idx := Index([]*Bundle{b})

	routes := []RouteEntry{
		{Target: TargetProxy, Sets: bindSets(idx, "google")},
	}
	eng := NewEngine(routes, TargetDirect, []*Bundle{b}, false)
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
			b2 := mustBundle(t, []BundleSet{
				{Name: "google", DomainSuffixes: []string{"google.com"}, CIDRs: []string{"8.8.8.0/24"}},
			})
			idx2 := Index([]*Bundle{b2})
			r2 := []RouteEntry{
				{Target: TargetProxy, Sets: bindSets(idx2, "google")},
			}
			if err := eng.Reload(r2, []*Bundle{b2}); err != nil {
				t.Errorf("Reload() error: %v", err)
			}
		}()
	}

	wg.Wait()
}

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
		{Target: Target(2), Sets: bindSets(idx, "google", "telegram")},
	}
	eng := NewEngine(routes, Target(0), []*Bundle{b}, false)
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
	b := mustBundle(t, []BundleSet{
		{Name: "google", CIDRs: []string{"8.8.8.0/24", "8.8.4.0/24"}},
		{Name: "geoip-cn", CIDRs: []string{"114.114.114.0/24", "223.5.5.0/24"}},
	})
	idx := Index([]*Bundle{b})

	routes := []RouteEntry{
		{Target: Target(2), Sets: bindSets(idx, "google")},
		{Target: Target(0), Sets: bindSets(idx, "geoip-cn")},
	}
	eng := NewEngine(routes, Target(1), []*Bundle{b}, false)
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
	b := mustBundle(t, []BundleSet{
		{Name: "google", DomainSuffixes: []string{"google.com"}},
	})
	idx := Index([]*Bundle{b})

	routes := []RouteEntry{
		{Target: Target(2), Sets: bindSets(idx, "google")},
	}
	eng := NewEngine(routes, Target(0), []*Bundle{b}, false)
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
	b := mustBundle(t, []BundleSet{
		{Name: "google", CIDRs: []string{"8.8.8.0/24"}},
	})
	idx := Index([]*Bundle{b})

	routes := []RouteEntry{
		{Target: Target(2), Sets: bindSets(idx, "google")},
	}
	eng := NewEngine(routes, Target(0), []*Bundle{b}, false)
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
	b := mustBundle(t, []BundleSet{
		{Name: "set-a", DomainSuffixes: []string{"overlap.com"}},
		{Name: "set-b", DomainSuffixes: []string{"overlap.com"}},
	})
	idx := Index([]*Bundle{b})

	routes := []RouteEntry{
		{Target: Target(1), Sets: bindSets(idx, "set-a")},  // first wins
		{Target: Target(2), Sets: bindSets(idx, "set-b")},
	}
	eng := NewEngine(routes, Target(0), []*Bundle{b}, false)
	defer eng.Close()

	if got := eng.Match("overlap.com"); got != Target(1) {
		t.Errorf("Match(overlap.com) = %v, want reject (first route wins)", got)
	}
}

func TestEngineConcurrency(t *testing.T) {
	b := mustBundle(t, []BundleSet{
		{Name: "google", DomainSuffixes: []string{"google.com"}, CIDRs: []string{"8.8.8.0/24"}},
	})
	idx := Index([]*Bundle{b})

	routes := []RouteEntry{
		{Target: Target(2), Sets: bindSets(idx, "google")},
	}
	eng := NewEngine(routes, Target(0), []*Bundle{b}, false)
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
	b := mustBundle(t, []BundleSet{
		{Name: "google", DomainSuffixes: []string{"google.com"}},
	})
	idx := Index([]*Bundle{b})
	routes := []RouteEntry{
		{Target: Target(2), Sets: bindSets(idx, "google"), ProcessNames: []string{"Chrome"}},
	}
	eng := NewEngine(routes, Target(0), []*Bundle{b}, false)
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
	b1 := mustBundle(t, []BundleSet{
		{Name: "google", DomainSuffixes: []string{"google.com"}},
	})
	idx1 := Index([]*Bundle{b1})

	routes1 := []RouteEntry{
		{Target: Target(2), Sets: bindSets(idx1, "google")},
	}
	eng := NewEngine(routes1, Target(0), []*Bundle{b1}, false)
	defer eng.Close()

	// Before reload: google→proxy, facebook→direct (fallback).
	if got := eng.Match("google.com"); got != Target(2) {
		t.Fatalf("before reload: Match(google.com) = %v, want proxy", got)
	}
	if got := eng.Match("facebook.com"); got != Target(0) {
		t.Fatalf("before reload: Match(facebook.com) = %v, want direct", got)
	}

	// Reload: facebook→proxy, google removed.
	b2 := mustBundle(t, []BundleSet{
		{Name: "facebook", DomainSuffixes: []string{"facebook.com"}},
	})
	idx2 := Index([]*Bundle{b2})
	routes2 := []RouteEntry{
		{Target: Target(2), Sets: bindSets(idx2, "facebook")},
	}
	if err := eng.Reload(routes2, []*Bundle{b2}); err != nil {
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
	b := mustBundle(t, []BundleSet{
		{Name: "google", DomainSuffixes: []string{"google.com"}},
	})
	idx := Index([]*Bundle{b})

	routes := []RouteEntry{
		{Target: Target(2), Sets: bindSets(idx, "google")},
	}
	eng := NewEngine(routes, Target(0), []*Bundle{b}, false)
	defer eng.Close()

	// Set a tmp rule for 8.8.8.8 → proxy.
	eng.SetTmpRule("8.8.8.8", Target(2))
	if got := eng.Match("8.8.8.8"); got != Target(2) {
		t.Fatalf("before reload: Match(8.8.8.8) = %v, want proxy", got)
	}

	// Reload with the same routes — tmp rules must be cleared.
	if err := eng.Reload(routes, []*Bundle{b}); err != nil {
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
	b := mustBundle(t, []BundleSet{
		{Name: "google", DomainSuffixes: []string{"google.com"}, CIDRs: []string{"8.8.8.0/24"}},
	})
	idx := Index([]*Bundle{b})

	routes := []RouteEntry{
		{Target: Target(2), Sets: bindSets(idx, "google")},
	}
	eng := NewEngine(routes, Target(0), []*Bundle{b}, false)
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
				{Target: Target(2), Sets: bindSets(idx2, "google")},
			}
			if err := eng.Reload(r2, []*Bundle{b2}); err != nil {
				t.Errorf("Reload() error: %v", err)
			}
		}()
	}

	wg.Wait()
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

func TestMatchMeta_OrSemanticsBetweenProcessAndPackageGroups(t *testing.T) {
	// When BOTH process and package criteria are set, the groups use OR semantics:
	// a connection satisfying EITHER group is matched. This supports app-bypass
	// routes that list desktop process names AND Android package/installer
	// criteria in one route entry.
	//
	// When only one group is set, the behaviour is unchanged (the lone group
	// must match; the absent group is trivially satisfied).
	r := &RouteEntry{
		ProcessNamePrefixes: []string{"wechat"},
		InstallerPackages:   []string{"com.xiaomi.market"},
	}
	// Process group satisfied alone → should pass (OR semantics).
	if !matchMeta(r, ConnMeta{ProcessName: "WeChat"}) {
		t.Errorf("process group satisfied alone should pass under OR semantics")
	}
	// Both groups satisfied → should pass.
	if !matchMeta(r, ConnMeta{ProcessName: "WeChat", InstallerPackage: "com.xiaomi.market"}) {
		t.Errorf("both groups satisfied should pass")
	}
	// Package group satisfied alone → should pass (OR semantics).
	if !matchMeta(r, ConnMeta{InstallerPackage: "com.xiaomi.market"}) {
		t.Errorf("package group satisfied alone should pass under OR semantics")
	}
	// Neither group satisfied → should fail.
	if matchMeta(r, ConnMeta{ProcessName: "Telegram"}) {
		t.Errorf("neither group satisfied should fail")
	}
	if matchMeta(r, ConnMeta{InstallerPackage: "com.google.play"}) {
		t.Errorf("wrong installer should fail")
	}
	if matchMeta(r, ConnMeta{}) {
		t.Errorf("empty meta should fail")
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

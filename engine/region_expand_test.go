//go:build !ios

package engine

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/kaitu-io/k2-rules/krs"
	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/rule"
)

// TestNEDiskPath_DirtyHeapBudget is the in-tree regression guard for the whole
// effort: building the NE (useDiskReader) engine over a large corpus must keep
// resident dirty heap O(set count) — tens of KB — not O(rule count) (~19 MB,
// which the heap path would cost). Generous 2 MB budget vs measured ~tens of KB.
func TestNEDiskPath_DirtyHeapBudget(t *testing.T) {
	dir := t.TempDir()
	regions := []string{"cn", "overseas", "ru", "ir", "id", "bd"}
	for _, r := range regions {
		domains := make([]string, 8000)
		for i := range domains {
			domains[i] = fmt.Sprintf("d%d-%s.example.com", i, r)
		}
		cidrs := make([]string, 2000)
		for i := range cidrs {
			cidrs[i] = fmt.Sprintf("10.%d.%d.0/24", (i>>8)&255, i&255)
		}
		writeDiskKRS(t, dir, r+".krs", &krs.Bundle{Sets: []krs.NamedSet{
			{Name: r, DomainSuffixes: domains, CIDRs: cidrs},
		}})
	}
	client := &config.ClientConfig{
		CacheDir: dir,
		Routes: []config.RouteConfig{
			{Via: "k2v5://x", Match: rule.MatchConfig{Region: "cn"}},
			{Via: "direct", Match: rule.MatchConfig{All: true}},
		},
	}
	outboundMap := buildOutboundMap(client.Routes)

	runtime.GC()
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	re := buildRuleEngine(client, outboundMap, true) // NE disk path

	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	deltaMB := float64(int64(after.HeapInuse)-int64(before.HeapInuse)) / (1 << 20)
	t.Logf("NE disk-path resident dirty heap: %.3f MB across %d regions (8000 domains + 2000 CIDRs each)",
		deltaMB, len(regions))
	if deltaMB > 2.0 {
		t.Fatalf("NE disk path dirty heap %.3f MB exceeds 2 MB budget — mmap reader not O(sets)? "+
			"(heap path over this corpus would cost ~tens of MB)", deltaMB)
	}
	runtime.KeepAlive(re)
	re.Close()
}

// openTempDiskBundle writes b to a temp .krs and mmaps it; unmapped on cleanup.
func openTempDiskBundle(t *testing.T, b *krs.Bundle) *krs.DiskBundle {
	t.Helper()
	var buf bytes.Buffer
	if err := krs.WriteBundle(&buf, b); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "x.krs")
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	db, err := krs.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestExpandRegionRouteDisk_HostOnly(t *testing.T) {
	db := openTempDiskBundle(t, &krs.Bundle{Sets: []krs.NamedSet{
		{Name: "cn", DomainSuffixes: []string{"qq.com"}, CIDRs: []string{"1.1.1.0/24"}},
	}})
	host := expandRegionRouteDisk(db, rule.Target(2))
	if host == nil || len(host.Sets) == 0 {
		t.Fatal("want host entry with sets")
	}
	if host.Target != rule.Target(2) {
		t.Fatalf("target = %v, want 2", host.Target)
	}
	if !host.Sets[0].MatchDomainReversed(krs.ReversedParents("www.qq.com")) {
		t.Error("disk set should match www.qq.com")
	}
}

func TestExpandRegionRouteDisk_Nil(t *testing.T) {
	if expandRegionRouteDisk(nil, rule.Target(2)) != nil {
		t.Error("nil bundle must yield nil entry")
	}
}

func writeDiskKRS(t *testing.T, dir, name string, b *krs.Bundle) {
	t.Helper()
	var buf bytes.Buffer
	if err := krs.WriteBundle(&buf, b); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestBuildRuleEngine_DiskPath_RoutesAndCloses drives the NE (useDiskReader)
// branch end to end: region route resolves via the mmap reader and Close
// munmaps without panic.
func TestBuildRuleEngine_DiskPath_RoutesAndCloses(t *testing.T) {
	dir := t.TempDir()
	writeDiskKRS(t, dir, "cn.krs", &krs.Bundle{Sets: []krs.NamedSet{
		{Name: "cn", DomainSuffixes: []string{"qq.com"}},
	}})
	client := &config.ClientConfig{
		CacheDir: dir,
		Routes: []config.RouteConfig{
			{Via: "k2v5://x", Match: rule.MatchConfig{Region: "cn"}},
			{Via: "direct", Match: rule.MatchConfig{All: true}},
		},
	}
	outboundMap := buildOutboundMap(client.Routes)
	proxy := outboundMap["k2v5://x"]

	re := buildRuleEngine(client, outboundMap, true) // NE path = mmap reader
	if re == nil {
		t.Fatal("nil engine")
	}
	if got := re.Match("www.qq.com"); got != proxy {
		t.Fatalf("cn domain → proxy %v, got %v", proxy, got)
	}
	if got := re.Match("google.com"); got == proxy {
		t.Fatal("non-cn domain must not route to proxy")
	}
	re.Close() // must munmap without panic
}

// fakeBundle builds a minimal krs.Bundle with one named set and platform-
// specific app patterns sufficient to exercise the expander.
func fakeBundle(setName string, darwin, win, android, installers []string) *krs.Bundle {
	b := &krs.Bundle{
		Sets: []krs.NamedSet{{Name: setName}},
		Apps: &krs.AppPatterns{},
	}
	b.Apps.Darwin.Apps = darwin
	b.Apps.Windows.Apps = win
	b.Apps.Android.Apps = android
	b.Apps.Android.Installers = installers
	return b
}

func TestExpandRegionRoute_HostEntryReferencesBundleSets(t *testing.T) {
	b := fakeBundle("geoip-cn", nil, nil, nil, nil)
	target := rule.Target(2)

	host, meta := expandRegionRoute(b, target)

	if host == nil {
		t.Fatal("host entry must be non-nil when bundle has Sets")
	}
	if ns, ok := host.Sets[0].(*krs.NamedSet); len(host.Sets) != 1 || !ok || ns.Name != "geoip-cn" {
		t.Errorf("host.Sets = %+v, want [geoip-cn]", host.Sets)
	}
	if host.Target != target {
		t.Errorf("host.Target = %v, want %v", host.Target, target)
	}
	if meta != nil {
		t.Errorf("meta entry should be nil when bundle.Apps has no patterns; got %+v", meta)
	}
}

func TestExpandRegionRoute_DarwinMetaUsesSplitGlobsCasePreserved(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin-only — case-preserved meta expansion")
	}
	b := fakeBundle("cn-sites",
		[]string{"WeChat", "Slack*"},
		nil, nil, nil,
	)
	target := rule.Target(2)

	_, meta := expandRegionRoute(b, target)

	if meta == nil {
		t.Fatal("meta entry expected for darwin with Darwin.Apps")
	}
	wantExact := []string{"WeChat"}
	if !equalStrSlices(meta.ProcessNames, wantExact) {
		t.Errorf("meta.ProcessNames = %v, want %v (case preserved)", meta.ProcessNames, wantExact)
	}
	wantPrefix := []string{"slack"}
	if !equalStrSlices(meta.ProcessNamePrefixes, wantPrefix) {
		t.Errorf("meta.ProcessNamePrefixes = %v, want %v (lowercased)", meta.ProcessNamePrefixes, wantPrefix)
	}
}

// Regression: match.apps force overrides used to compile to nil on Linux
// (expandAppsRoute had no linux case → default: return nil), so per-app
// overrides silently did nothing despite Linux having a working
// ProcessSearcher. They must now build a process-name meta entry, mirroring
// Darwin semantics (/proc/PID/exe basename, case-sensitive exact match).
func TestExpandAppsRoute_LinuxBuildsProcessNames(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux-only — match.apps expansion on linux desktop")
	}
	target := rule.Target(2)
	entry := expandAppsRoute([]string{"curl", "Chrome*"}, target)
	if entry == nil {
		t.Fatal("expandAppsRoute must build a meta entry on linux (regression: was nil)")
	}
	if !equalStrSlices(entry.ProcessNames, []string{"curl"}) {
		t.Errorf("entry.ProcessNames = %v, want [curl] (case preserved)", entry.ProcessNames)
	}
	if !equalStrSlices(entry.ProcessNamePrefixes, []string{"chrome"}) {
		t.Errorf("entry.ProcessNamePrefixes = %v, want [chrome] (lowercased)", entry.ProcessNamePrefixes)
	}
	if entry.Target != target {
		t.Errorf("entry.Target = %v, want %v", entry.Target, target)
	}
}

func TestExpandRegionRoute_WindowsMetaUsesWindowsApps(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only — Windows.Apps expansion")
	}
	b := fakeBundle("cn-sites", nil,
		[]string{"qq.exe", "wechat*"},
		nil, nil,
	)
	_, meta := expandRegionRoute(b, rule.Target(2))
	if meta == nil {
		t.Fatal("meta expected")
	}
	if !slices.Contains(meta.ProcessNames, "qq.exe") {
		t.Errorf("ProcessNames missing qq.exe: %v", meta.ProcessNames)
	}
	if !slices.Contains(meta.ProcessNamePrefixes, "wechat") {
		t.Errorf("ProcessNamePrefixes missing wechat: %v", meta.ProcessNamePrefixes)
	}
}

func TestExpandRegionRoute_AndroidMetaUsesAndroidAppsNotInstallers(t *testing.T) {
	b := fakeBundle("cn-sites", nil, nil,
		[]string{"com.tencent.mm", "com.alibaba.*"},
		[]string{"com.tencent.android.qqdownloader"},
	)
	meta := buildMetaEntryFromAppsFor("android", b.Apps, rule.Target(2))
	if meta == nil {
		t.Fatal("meta expected")
	}
	if !slices.Contains(meta.PackageNames, "com.tencent.mm") {
		t.Errorf("PackageNames missing com.tencent.mm: %v", meta.PackageNames)
	}
	if !slices.Contains(meta.PackageNamePrefixes, "com.alibaba.") {
		t.Errorf("PackageNamePrefixes missing com.alibaba.: %v", meta.PackageNamePrefixes)
	}
	// Installer must NOT route: on Chinese ROMs nearly every app (foreign
	// browsers included) reports the OEM store as installer — see the
	// comment in buildMetaEntryFromApps.
	if len(meta.InstallerPackages) != 0 {
		t.Errorf("InstallerPackages must stay empty for region routing, got %v", meta.InstallerPackages)
	}
}

func TestExpandRegionRoute_NilBundleReturnsNilNil(t *testing.T) {
	host, meta := expandRegionRoute(nil, rule.Target(2))
	if host != nil || meta != nil {
		t.Errorf("nil bundle → (nil, nil); got (%v, %v)", host, meta)
	}
}

func TestExpandRegionRoute_EmptyBundleReturnsNilNil(t *testing.T) {
	host, meta := expandRegionRoute(&krs.Bundle{}, rule.Target(2))
	if host != nil || meta != nil {
		t.Errorf("empty bundle → (nil, nil); got (%v, %v)", host, meta)
	}
}

func TestExpandAppsRoute_DarwinPlatformDispatch(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin-only")
	}
	globs := []string{"Firefox", "Chrome*"}
	entry := expandAppsRoute(globs, rule.Target(3))
	if entry == nil {
		t.Fatal("expected meta entry on Darwin")
	}
	if !slices.Contains(entry.ProcessNames, "Firefox") {
		t.Errorf("ProcessNames = %v, want contains Firefox", entry.ProcessNames)
	}
	if !slices.Contains(entry.ProcessNamePrefixes, "chrome") {
		t.Errorf("ProcessNamePrefixes = %v, want contains chrome (lowercased)", entry.ProcessNamePrefixes)
	}
	if entry.Target != rule.Target(3) {
		t.Errorf("Target = %v, want 3", entry.Target)
	}
}

func TestExpandAppsRoute_EmptyReturnsNil(t *testing.T) {
	if entry := expandAppsRoute(nil, rule.Target(3)); entry != nil {
		t.Errorf("nil globs → nil entry; got %+v", entry)
	}
	if entry := expandAppsRoute([]string{}, rule.Target(3)); entry != nil {
		t.Errorf("empty globs → nil entry; got %+v", entry)
	}
}

// NOTE: a former TestExpandAppsRoute_UnsupportedPlatformReturnsNil asserted
// linux returned nil here. That codified the App Bypass-on-Linux bug (overrides
// silently dropped) as expected behavior. Linux is now supported
// (TestExpandAppsRoute_LinuxBuildsProcessNames), and iOS never compiles this
// file (//go:build !ios), so the "unsupported desktop platform" case no longer
// has a live target — the test was removed rather than left asserting a bug.

func equalStrSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestBuildRouteEntries_ExpandsMatchRegion(t *testing.T) {
	// Populate the app patterns for every app-attribution platform (darwin,
	// windows, android) so the region bundle expands into a meta entry on each —
	// matching the wantEntries=2 switch below. (A darwin-only fixture made
	// Windows/Android correctly yield 1 entry while the switch expected 2.)
	cnBundle := fakeBundle("cn-sites",
		[]string{"WeChat"},          // darwin
		[]string{"wechat.exe"},      // windows
		[]string{"com.tencent.mm"},  // android
		nil,
	)
	regionMap := map[string]*krs.Bundle{"cn": cnBundle}

	cfgRoutes := []config.RouteConfig{
		{Via: "direct", Match: rule.MatchConfig{Region: "cn"}},
		{Match: rule.MatchConfig{All: true}, Via: "k2v5://x"},
	}
	outboundMap := map[string]rule.Target{"k2v5://x": rule.Target(2)}

	entries, _, _ := buildRouteEntries(cfgRoutes, nil, outboundMap, heapRegionExpander(regionMap))

	wantEntries := 1
	switch runtime.GOOS {
	case "darwin", "windows", "android":
		wantEntries = 2
	}
	if len(entries) != wantEntries {
		t.Fatalf("len(entries) = %d, want %d; entries=%+v", len(entries), wantEntries, entries)
	}
	if len(entries[0].Sets) != 1 {
		t.Errorf("entries[0] (host) Sets = %d, want 1", len(entries[0].Sets))
	}
}

func TestBuildRouteEntries_UnknownRegionLogsAndSkips(t *testing.T) {
	regionMap := map[string]*krs.Bundle{} // empty — no "cn" loaded
	cfgRoutes := []config.RouteConfig{
		{Via: "direct", Match: rule.MatchConfig{Region: "cn"}},
		{Match: rule.MatchConfig{All: true}, Via: "k2v5://x"},
	}
	outboundMap := map[string]rule.Target{"k2v5://x": rule.Target(2)}

	entries, fallback, _ := buildRouteEntries(cfgRoutes, nil, outboundMap, heapRegionExpander(regionMap))

	if len(entries) != 0 {
		t.Errorf("unknown region must produce 0 entries; got %+v", entries)
	}
	if fallback != rule.Target(2) {
		t.Errorf("fallback should still be set from catch-all; got %v", fallback)
	}
}

func TestBuildRouteEntries_ExpandsMatchApps(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin-only smoke for apps expansion")
	}
	cfgRoutes := []config.RouteConfig{
		{Via: "k2v5://x", Match: rule.MatchConfig{Apps: []string{"Firefox*"}}},
		{Match: rule.MatchConfig{All: true}, Via: "k2v5://x"},
	}
	outboundMap := map[string]rule.Target{"k2v5://x": rule.Target(2)}

	entries, _, _ := buildRouteEntries(cfgRoutes, nil, outboundMap, heapRegionExpander(nil))

	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1; entries=%+v", len(entries), entries)
	}
	if !slices.Contains(entries[0].ProcessNamePrefixes, "firefox") {
		t.Errorf("ProcessNamePrefixes = %v, want contains firefox", entries[0].ProcessNamePrefixes)
	}
}

// TestBuildRuleEngine_CNFallbackForMissingRegion is the composition proof: with
// only cn.krs on disk and a route requiring region=my, both the heap (desktop/
// Android) and disk (iOS NE) builders must wire cn's data into the my-region
// direct route — yielding a working split-tunnel engine instead of dropping the
// route (all-proxy) or 504-failing. CN traffic routes direct via the fallback;
// everything else falls through to the all-proxy catch-all.
func TestBuildRuleEngine_CNFallbackForMissingRegion(t *testing.T) {
	const directTarget = rule.Target(0) // buildOutboundMap: 0=direct
	for _, useDisk := range []bool{false, true} {
		name := "heap"
		if useDisk {
			name = "disk"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeDiskKRS(t, dir, "cn.krs", &krs.Bundle{Sets: []krs.NamedSet{
				{Name: "cn", DomainSuffixes: []string{"qq.com"}},
			}}) // no my.krs

			client := &config.ClientConfig{
				CacheDir: dir,
				Routes: []config.RouteConfig{
					{Via: "direct", Match: rule.MatchConfig{Region: "my"}},
					{Via: "k2v5://x", Match: rule.MatchConfig{All: true}},
				},
			}
			outboundMap := buildOutboundMap(client.Routes)
			proxy := outboundMap["k2v5://x"]

			re := buildRuleEngine(client, outboundMap, useDisk)
			if re == nil {
				t.Fatal("nil engine")
			}
			defer re.Close()

			if got := re.Match("www.qq.com"); got != directTarget {
				t.Fatalf("cn domain via cn-fallback → direct(%v), got %v", directTarget, got)
			}
			if got := re.Match("google.com"); got != proxy {
				t.Fatalf("non-cn domain → proxy(%v), got %v", proxy, got)
			}
		})
	}
}

// TestBuildRuleEngine_RealBundleWinsOverCNFallback proves the cn fallback is
// transient: once the region's own bundle is present (as the desktop/Android
// updater's rebuildFn produces after downloading it over the tunnel), the region
// route uses ITS data, not cn's. my-domains route direct via the real my bundle;
// cn-domains no longer get the fallback's direct treatment and fall through to the
// catch-all proxy. This is the deterministic stand-in for the self-heal hot-swap.
func TestBuildRuleEngine_RealBundleWinsOverCNFallback(t *testing.T) {
	const directTarget = rule.Target(0)
	for _, useDisk := range []bool{false, true} {
		name := "heap"
		if useDisk {
			name = "disk"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeDiskKRS(t, dir, "cn.krs", &krs.Bundle{Sets: []krs.NamedSet{
				{Name: "cn", DomainSuffixes: []string{"qq.com"}},
			}})
			writeDiskKRS(t, dir, "my.krs", &krs.Bundle{Sets: []krs.NamedSet{
				{Name: "my", DomainSuffixes: []string{"astro.com.my"}},
			}}) // real region bundle now present

			client := &config.ClientConfig{
				CacheDir: dir,
				Routes: []config.RouteConfig{
					{Via: "direct", Match: rule.MatchConfig{Region: "my"}},
					{Via: "k2v5://x", Match: rule.MatchConfig{All: true}},
				},
			}
			outboundMap := buildOutboundMap(client.Routes)
			proxy := outboundMap["k2v5://x"]

			re := buildRuleEngine(client, outboundMap, useDisk)
			if re == nil {
				t.Fatal("nil engine")
			}
			defer re.Close()

			if got := re.Match("astro.com.my"); got != directTarget {
				t.Fatalf("my domain via real my bundle → direct(%v), got %v", directTarget, got)
			}
			if got := re.Match("www.qq.com"); got != proxy {
				t.Fatalf("cn domain must NOT get fallback-direct once my present → proxy(%v), got %v", proxy, got)
			}
		})
	}
}

// TestHeapRegionExpander_CNFallbackForMissingRegion verifies the cn-fallback
// safety net: when a non-cn region route has no bundle on disk, the expander
// substitutes the always-embedded cn bundle instead of dropping the route. This
// lets an abroad user (e.g. region=my) connect on the embedded cn.krs until the
// background updater downloads their real region bundle, after which rebuildFn
// re-runs buildRouteEntries and hot-swaps to the correct data — no reconnect.
func TestHeapRegionExpander_CNFallbackForMissingRegion(t *testing.T) {
	cnBundle := fakeBundle("geoip-cn", nil, nil, nil, nil)
	regionMap := map[string]*krs.Bundle{"cn": cnBundle} // no "my"

	entries := heapRegionExpander(regionMap)("my", rule.Target(2))
	if len(entries) == 0 {
		t.Fatal("missing region 'my' must fall back to cn bundle, got 0 entries")
	}
	if ns, ok := entries[0].Sets[0].(*krs.NamedSet); !ok || ns.Name != "geoip-cn" {
		t.Errorf("fallback host set = %+v, want geoip-cn", entries[0].Sets)
	}
}

// TestHeapRegionExpander_NoCNNoFallback verifies that with neither the requested
// region nor cn present, the expander yields nil (route drops). In production
// missingConfiguredRegions has already 504'd before reaching here, preserving the
// original all-proxy disaster guard.
func TestHeapRegionExpander_NoCNNoFallback(t *testing.T) {
	if entries := heapRegionExpander(map[string]*krs.Bundle{})("my", rule.Target(2)); entries != nil {
		t.Errorf("no cn fallback available must yield nil; got %+v", entries)
	}
}

//go:build !ios

package engine

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/kaitu-io/k2-rules/krs"
	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/rule"
)

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

func TestExpandRegionRoute_AndroidMetaUsesAndroidAppsAndInstallers(t *testing.T) {
	if runtime.GOOS != "android" {
		t.Skip("Android-only — Android.Apps + Installers")
	}
	b := fakeBundle("cn-sites", nil, nil,
		[]string{"com.tencent.mm", "com.alibaba.*"},
		[]string{"com.tencent.android.qqdownloader"},
	)
	_, meta := expandRegionRoute(b, rule.Target(2))
	if meta == nil {
		t.Fatal("meta expected")
	}
	if !slices.Contains(meta.PackageNames, "com.tencent.mm") {
		t.Errorf("PackageNames missing com.tencent.mm: %v", meta.PackageNames)
	}
	if !slices.Contains(meta.PackageNamePrefixes, "com.alibaba.") {
		t.Errorf("PackageNamePrefixes missing com.alibaba.: %v", meta.PackageNamePrefixes)
	}
	if !slices.Contains(meta.InstallerPackages, "com.tencent.android.qqdownloader") {
		t.Errorf("InstallerPackages missing entry: %v", meta.InstallerPackages)
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
	cnBundle := fakeBundle("cn-sites", []string{"WeChat"}, nil, nil, nil)
	regionMap := map[string]*krs.Bundle{"cn": cnBundle}

	cfgRoutes := []config.RouteConfig{
		{Via: "direct", Match: rule.MatchConfig{Region: "cn"}},
		{Match: rule.MatchConfig{All: true}, Via: "k2v5://x"},
	}
	outboundMap := map[string]rule.Target{"k2v5://x": rule.Target(2)}

	entries, _, _ := buildRouteEntries(cfgRoutes, nil, outboundMap, regionMap)

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

	entries, fallback, _ := buildRouteEntries(cfgRoutes, nil, outboundMap, regionMap)

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

	entries, _, _ := buildRouteEntries(cfgRoutes, nil, outboundMap, nil)

	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1; entries=%+v", len(entries), entries)
	}
	if !slices.Contains(entries[0].ProcessNamePrefixes, "firefox") {
		t.Errorf("ProcessNamePrefixes = %v, want contains firefox", entries[0].ProcessNamePrefixes)
	}
}

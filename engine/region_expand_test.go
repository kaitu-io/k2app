//go:build !ios

package engine

import (
	"runtime"
	"testing"

	"github.com/kaitu-io/k2-rules/krs"
	"github.com/kaitu-io/k2/rule"
)

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
	if len(host.Sets) != 1 || host.Sets[0].Name != "geoip-cn" {
		t.Errorf("host.Sets = %+v, want [geoip-cn]", host.Sets)
	}
	if host.Target != target {
		t.Errorf("host.Target = %v, want %v", host.Target, target)
	}
	if meta != nil && len(meta.ProcessNames) == 0 && len(meta.ProcessNamePrefixes) == 0 &&
		len(meta.PackageNames) == 0 && len(meta.PackageNamePrefixes) == 0 &&
		len(meta.InstallerPackages) == 0 {
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

func TestExpandRegionRoute_WindowsMetaUsesWindowsApps(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only — Windows.Apps expansion")
	}
	b := fakeBundle("cn-sites", nil,
		[]string{"qq.exe", "wechat*.exe"},
		nil, nil,
	)
	_, meta := expandRegionRoute(b, rule.Target(2))
	if meta == nil {
		t.Fatal("meta expected")
	}
	if !containsStr(meta.ProcessNames, "qq.exe") {
		t.Errorf("ProcessNames missing qq.exe: %v", meta.ProcessNames)
	}
	if !containsStr(meta.ProcessNamePrefixes, "wechat") {
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
	if !containsStr(meta.PackageNames, "com.tencent.mm") {
		t.Errorf("PackageNames missing com.tencent.mm: %v", meta.PackageNames)
	}
	if !containsStr(meta.PackageNamePrefixes, "com.alibaba.") {
		t.Errorf("PackageNamePrefixes missing com.alibaba.: %v", meta.PackageNamePrefixes)
	}
	if !containsStr(meta.InstallerPackages, "com.tencent.android.qqdownloader") {
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
	if !containsStr(entry.ProcessNames, "Firefox") {
		t.Errorf("ProcessNames = %v, want contains Firefox", entry.ProcessNames)
	}
	if !containsStr(entry.ProcessNamePrefixes, "chrome") {
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

// containsStr is declared in appbypass_test.go (same package).

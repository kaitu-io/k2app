package appbypass

import (
	"reflect"
	"sort"
	"testing"
)

func TestMatchInstalled_DesktopProcessPrefixHit(t *testing.T) {
	preset := &Preset{
		Region:  "cn",
		Desktop: DesktopPatterns{ProcessPrefix: []string{"wechat", "qq"}},
	}
	apps := []MatchableApp{
		{ID: "/Applications/WeChat.app", Label: "WeChat", ProcessNames: []string{"wechat", "wechathelper"}},
		{ID: "/Applications/Firefox.app", Label: "Firefox", ProcessNames: []string{"firefox"}},
	}
	got := MatchInstalled(preset, apps, "darwin")
	if len(got) != 1 {
		t.Fatalf("want 1 match, got %d: %+v", len(got), got)
	}
	if got[0].HitKind != "process_prefix" {
		t.Errorf("hit kind %q", got[0].HitKind)
	}
	if got[0].HitPattern != "wechat" {
		t.Errorf("hit pattern %q", got[0].HitPattern)
	}
}

func TestMatchInstalled_DesktopProcessExactHit(t *testing.T) {
	preset := &Preset{
		Desktop: DesktopPatterns{ProcessExact: []string{"firefox"}},
	}
	apps := []MatchableApp{
		{ID: "/Applications/Firefox.app", Label: "Firefox", ProcessNames: []string{"firefox"}},
	}
	got := MatchInstalled(preset, apps, "darwin")
	if len(got) != 1 {
		t.Fatalf("want 1, got %d", len(got))
	}
	if got[0].HitKind != "process_exact" {
		t.Errorf("hit kind %q want process_exact", got[0].HitKind)
	}
}

func TestMatchInstalled_AndroidInstallerHit(t *testing.T) {
	preset := &Preset{
		Region:  "cn",
		Android: AndroidPatterns{InstallerExact: []string{"com.xiaomi.market"}},
	}
	apps := []MatchableApp{
		{ID: "com.tencent.mm", Label: "WeChat", InstallerPackageName: "com.xiaomi.market"},
		{ID: "com.example", Label: "Other", InstallerPackageName: "com.android.vending"},
	}
	got := MatchInstalled(preset, apps, "android")
	if len(got) != 1 || got[0].HitKind != "installer_exact" {
		t.Fatalf("want installer match, got %+v", got)
	}
}

func TestMatchInstalled_AndroidPackageExactHit(t *testing.T) {
	preset := &Preset{
		Android: AndroidPatterns{PackageExact: []string{"com.tencent.mm"}},
	}
	apps := []MatchableApp{
		{ID: "com.tencent.mm", Label: "WeChat"},
		{ID: "com.example", Label: "Other"},
	}
	got := MatchInstalled(preset, apps, "android")
	if len(got) != 1 || got[0].HitKind != "package_exact" {
		t.Fatalf("want package_exact, got %+v", got)
	}
}

func TestMatchInstalled_AndroidPackagePrefixHit(t *testing.T) {
	preset := &Preset{
		Android: AndroidPatterns{PackagePrefix: []string{"com.tencent."}},
	}
	apps := []MatchableApp{
		{ID: "com.tencent.mm", Label: "WeChat"},
		{ID: "com.tencent.qq", Label: "QQ"},
		{ID: "com.other", Label: "Other"},
	}
	got := MatchInstalled(preset, apps, "android")
	if len(got) != 2 {
		t.Fatalf("want 2 prefix matches, got %d: %+v", len(got), got)
	}
}

func TestMatchInstalled_NilPresetReturnsEmpty(t *testing.T) {
	got := MatchInstalled(nil, []MatchableApp{{ID: "x"}}, "darwin")
	if len(got) != 0 {
		t.Fatalf("nil preset should match nothing, got %+v", got)
	}
}

func TestMatchInstalled_UnsupportedGoosReturnsEmpty(t *testing.T) {
	preset := &Preset{Desktop: DesktopPatterns{ProcessPrefix: []string{"wechat"}}}
	apps := []MatchableApp{{ID: "x", ProcessNames: []string{"wechat"}}}
	got := MatchInstalled(preset, apps, "ios")
	if len(got) != 0 {
		t.Fatalf("ios should match nothing here, got %+v", got)
	}
}

func TestMatchInstalled_PrefixDoesNotOverflow(t *testing.T) {
	preset := &Preset{Desktop: DesktopPatterns{ProcessPrefix: []string{"tencent"}}}
	apps := []MatchableApp{
		{ID: "ok", Label: "Tencent Meeting", ProcessNames: []string{"tencentmeeting"}},
		{ID: "fp", Label: "TencentVibe", ProcessNames: []string{"tencentvibe"}}, // also matches
		{ID: "no", Label: "Unrelated", ProcessNames: []string{"tencen"}},        // shorter — must not match
	}
	got := MatchInstalled(preset, apps, "darwin")
	ids := []string{}
	for _, m := range got {
		ids = append(ids, m.ID)
	}
	sort.Strings(ids)
	want := []string{"fp", "ok"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("got %v want %v", ids, want)
	}
}

func TestMatchInstalled_EmptyAppsReturnsNil(t *testing.T) {
	preset := &Preset{Desktop: DesktopPatterns{ProcessPrefix: []string{"x"}}}
	got := MatchInstalled(preset, nil, "darwin")
	if got != nil {
		t.Fatalf("empty apps should return nil, got %+v", got)
	}
}

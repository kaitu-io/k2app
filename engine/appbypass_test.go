package engine

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/kaitu-io/k2/config"
)

func TestBuildAppBypassRoute_NoRegion_ReturnsNil(t *testing.T) {
	cfg := Config{Client: &config.ClientConfig{}}
	rt, err := buildAppBypassRoute(&cfg)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if rt != nil {
		t.Errorf("expected nil route when region empty")
	}
}

func TestBuildAppBypassRoute_RegionWithoutPreset(t *testing.T) {
	tmp := t.TempDir()
	cfg := Config{
		AppBypassRegion: "cn",
		Client:          &config.ClientConfig{CacheDir: tmp},
	}
	rt, _ := buildAppBypassRoute(&cfg)
	if rt != nil {
		t.Errorf("expected nil when no preset present")
	}
}

func TestBuildAppBypassRoute_FixtureCN(t *testing.T) {
	tmp := t.TempDir()
	copyFixture(t, "../appbypass/testdata/app-bypass-cn.yaml",
		filepath.Join(tmp, "app-bypass-cn.yaml"))

	cfg := Config{
		AppBypassRegion: "cn",
		Client:          &config.ClientConfig{CacheDir: tmp},
		AppBypassCustom: AppBypassCustom{
			ProcessAdds: []string{"Steam"},
			PackageAdds: []string{"com.user.app"},
		},
	}
	rt, err := buildAppBypassRoute(&cfg)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if rt == nil {
		t.Fatal("expected non-nil route")
	}
	if rt.Via != "direct" {
		t.Errorf("Via = %q want direct", rt.Via)
	}

	// Installer (Android exact, sorted by compile)
	wantInstaller := []string{"com.huawei.appmarket", "com.xiaomi.market"}
	if !reflect.DeepEqual(wantInstaller, rt.Match.InstallerPackage) {
		t.Errorf("InstallerPackage = %v, want %v", rt.Match.InstallerPackage, wantInstaller)
	}
	if !containsStr(rt.Match.ProcessName, "Steam") {
		t.Errorf("ProcessName should contain custom Steam: %v", rt.Match.ProcessName)
	}
	if !containsStr(rt.Match.PackageName, "com.user.app") {
		t.Errorf("PackageName should contain custom com.user.app")
	}
	// Lowercased preset prefix should be present
	if !containsStr(rt.Match.ProcessNamePrefix, "wechat") {
		t.Errorf("ProcessNamePrefix should contain lowercased 'wechat': %v", rt.Match.ProcessNamePrefix)
	}
	if !containsStr(rt.Match.PackageNamePrefix, "com.tencent.") {
		t.Errorf("PackageNamePrefix should contain com.tencent.")
	}
}

func containsStr(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func copyFixture(t *testing.T, src, dst string) {
	t.Helper()
	in, err := os.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		t.Fatal(err)
	}
}

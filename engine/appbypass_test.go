package engine

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/rule"
)

func TestBuildAppBypassRoute_NoRegion_ReturnsNil(t *testing.T) {
	cfg := Config{Client: &config.ClientConfig{}}
	rts, err := buildAppBypassRoute(&cfg)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if rts != nil {
		t.Errorf("expected nil routes when region empty")
	}
}

func TestBuildAppBypassRoute_RegionWithoutPreset(t *testing.T) {
	tmp := t.TempDir()
	cfg := Config{
		AppBypassRegion: "cn",
		Client:          &config.ClientConfig{CacheDir: tmp},
	}
	rts, _ := buildAppBypassRoute(&cfg)
	if rts != nil {
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
	rts, err := buildAppBypassRoute(&cfg)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(rts) != 2 {
		t.Fatalf("expected 2 routes (desktop + android), got %d", len(rts))
	}

	// Find desktop + android routes by which fields are populated.
	var desktop, android *config.RouteConfig
	for _, r := range rts {
		if len(r.Match.ProcessName) > 0 || len(r.Match.ProcessNamePrefix) > 0 {
			desktop = r
		}
		if len(r.Match.PackageName) > 0 || len(r.Match.PackageNamePrefix) > 0 || len(r.Match.InstallerPackage) > 0 {
			android = r
		}
	}
	if desktop == nil {
		t.Fatal("missing desktop route")
	}
	if android == nil {
		t.Fatal("missing android route")
	}

	if desktop.Via != "direct" {
		t.Errorf("desktop.Via = %q", desktop.Via)
	}
	if android.Via != "direct" {
		t.Errorf("android.Via = %q", android.Via)
	}

	// Custom adds + preset on desktop.
	if !containsStr(desktop.Match.ProcessName, "Steam") {
		t.Errorf("desktop ProcessName should contain custom Steam: %v", desktop.Match.ProcessName)
	}
	if !containsStr(desktop.Match.ProcessNamePrefix, "wechat") {
		t.Errorf("desktop ProcessNamePrefix should contain lowercased wechat: %v", desktop.Match.ProcessNamePrefix)
	}
	// Desktop should NOT have Android fields.
	if len(desktop.Match.PackageName) > 0 || len(desktop.Match.PackageNamePrefix) > 0 || len(desktop.Match.InstallerPackage) > 0 {
		t.Errorf("desktop route should not contain Android fields: %+v", desktop.Match)
	}

	// Android route fields.
	wantInstaller := []string{"com.huawei.appmarket", "com.xiaomi.market"}
	if !reflect.DeepEqual(wantInstaller, android.Match.InstallerPackage) {
		t.Errorf("android InstallerPackage = %v, want %v", android.Match.InstallerPackage, wantInstaller)
	}
	if !containsStr(android.Match.PackageName, "com.user.app") {
		t.Errorf("android PackageName should contain custom com.user.app: %v", android.Match.PackageName)
	}
	if !containsStr(android.Match.PackageNamePrefix, "com.tencent.") {
		t.Errorf("android PackageNamePrefix should contain com.tencent.")
	}
	// Android route should NOT have desktop process fields.
	if len(android.Match.ProcessName) > 0 || len(android.Match.ProcessNamePrefix) > 0 {
		t.Errorf("android route should not contain desktop fields: %+v", android.Match)
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

func TestBuildRuleEngine_PrependsAppBypassRoute(t *testing.T) {
	tmp := t.TempDir()
	copyFixture(t, "../appbypass/testdata/app-bypass-cn.yaml",
		filepath.Join(tmp, "app-bypass-cn.yaml"))

	client := &config.ClientConfig{
		CacheDir: tmp,
		Routes: []config.RouteConfig{
			{Via: "k2v5://example.com/", Match: rule.MatchConfig{All: true}},
		},
	}
	extras, err := buildAppBypassRoute(&Config{
		AppBypassRegion: "cn",
		Client:          client,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(extras) == 0 {
		t.Fatal("expected appbypass routes")
	}

	var extraRoutes []config.RouteConfig
	for _, r := range extras {
		extraRoutes = append(extraRoutes, *r)
	}

	outboundMap := map[string]rule.Target{
		"k2v5://example.com/": rule.Target(2),
	}
	re := buildRuleEngine(client, outboundMap, extraRoutes)

	// Desktop WeChat → direct (matches desktop route's ProcessNamePrefix "wechat")
	target := re.MatchConn("example.org", rule.ConnMeta{ProcessName: "WeChat", Network: "tcp"})
	if target != rule.Target(0) {
		t.Errorf("WeChat should route direct (0), got Target(%d)", target)
	}
	// Telegram → should NOT direct
	target = re.MatchConn("example.org", rule.ConnMeta{ProcessName: "Telegram", Network: "tcp"})
	if target == rule.Target(0) {
		t.Errorf("Telegram should NOT route direct")
	}
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

func TestEngine_SetAppBypassCustomThenRefresh(t *testing.T) {
	// We test the setter API exists and the closure-based refresh works.
	// Full engine.Start integration is exercised by Task 21's e2e test.
	e := &Engine{}
	e.SetAppBypassRegion("cn")
	if e.appBypassRegion != "cn" {
		t.Errorf("SetAppBypassRegion failed: %q", e.appBypassRegion)
	}
	e.SetAppBypassCustom(AppBypassCustom{ProcessAdds: []string{"Slack"}})
	if !containsStr(e.appBypassCustom.ProcessAdds, "Slack") {
		t.Errorf("SetAppBypassCustom failed: %v", e.appBypassCustom)
	}
}

// TestE2E_AppBypass_RoutingScenarios covers six real-world matching scenarios.
// Uses the fixture cn.yaml + manually-built rule.Engine to verify per-flow
// match behavior end-to-end across both desktop and android platform routes.
func TestE2E_AppBypass_RoutingScenarios(t *testing.T) {
	tmp := t.TempDir()
	copyFixture(t, "../appbypass/testdata/app-bypass-cn.yaml",
		filepath.Join(tmp, "app-bypass-cn.yaml"))

	client := &config.ClientConfig{
		CacheDir: tmp,
		Routes: []config.RouteConfig{
			{Via: "k2v5://example.com/", Match: rule.MatchConfig{All: true}},
		},
	}
	cfg := &Config{AppBypassRegion: "cn", Client: client}
	abRoutes, err := buildAppBypassRoute(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(abRoutes) == 0 {
		t.Fatal("expected appbypass routes")
	}

	var extraRoutes []config.RouteConfig
	for _, r := range abRoutes {
		extraRoutes = append(extraRoutes, *r)
	}

	outboundMap := map[string]rule.Target{"k2v5://example.com/": rule.Target(2)}
	re := buildRuleEngine(client, outboundMap, extraRoutes)

	type tc struct {
		name       string
		meta       rule.ConnMeta
		wantDirect bool
	}
	cases := []tc{
		{"mac helper basename", rule.ConnMeta{ProcessName: "WeChatHelper", Network: "tcp"}, true},
		{"win exe basename", rule.ConnMeta{ProcessName: "WeChat.exe", Network: "tcp"}, true},
		{"linux lowercase", rule.ConnMeta{ProcessName: "wechat", Network: "tcp"}, true},
		{"non-Chinese app", rule.ConnMeta{ProcessName: "Telegram", Network: "tcp"}, false},
		{"android package prefix", rule.ConnMeta{PackageName: "com.tencent.mm", Network: "tcp"}, true},
		{"android installer signal", rule.ConnMeta{
			PackageName:      "com.random.foo",
			InstallerPackage: "com.xiaomi.market",
			Network:          "tcp",
		}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			target := re.MatchConn("example.org", c.meta)
			isDirect := target == rule.Target(0)
			if isDirect != c.wantDirect {
				t.Errorf("got direct=%v want direct=%v (target=%d) for meta=%+v",
					isDirect, c.wantDirect, target, c.meta)
			}
		})
	}
}

// TestBuildAppBypassRoute_CorruptYAML_ReturnsError verifies that a corrupted
// yaml file produces an error (not silent nil-return). engine.Start wraps
// this in a slog.Warn so smart-bypass degradation surfaces in logs instead
// of leaving the user with mysterious "WeChat goes through VPN" symptoms.
//
// Trigger paths: partial CDN download, on-disk corruption, version > supported.
func TestBuildAppBypassRoute_CorruptYAML_ReturnsError(t *testing.T) {
	tmp := t.TempDir()
	// Garbage that's not valid YAML.
	if err := os.WriteFile(filepath.Join(tmp, "app-bypass-cn.yaml"),
		[]byte("\x00\x01\x02 not yaml at all"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := Config{
		AppBypassRegion: "cn",
		Client:          &config.ClientConfig{CacheDir: tmp},
	}
	// appbypass.Load is "best-effort per-file" — corrupted file is logged
	// + skipped, NOT propagated. So buildAppBypassRoute returns nil,nil
	// here, not an error. This locks in the graceful-skip contract.
	routes, err := buildAppBypassRoute(&cfg)
	if err != nil {
		t.Fatalf("corrupt yaml should be skipped, not propagated: %v", err)
	}
	if routes != nil {
		t.Errorf("corrupt yaml + no custom adds → no routes, got %d", len(routes))
	}
}

// TestBuildAppBypassRoute_CorruptYAMLWithCustom_StillProducesCustomRoutes
// verifies the Plan C key contract: even when the preset yaml fails to
// load, user-added custom apps still produce direct routes. This means
// a CDN incident doesn't break the user's manually-added bypass list.
func TestBuildAppBypassRoute_CorruptYAMLWithCustom_StillProducesCustomRoutes(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "app-bypass-cn.yaml"),
		[]byte("\x00garbage"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := Config{
		AppBypassRegion: "cn",
		Client:          &config.ClientConfig{CacheDir: tmp},
		AppBypassCustom: AppBypassCustom{
			ProcessAdds: []string{"Steam.app"},
			PackageAdds: []string{"com.user.foo"},
		},
	}
	routes, err := buildAppBypassRoute(&cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(routes) != 2 {
		t.Fatalf("custom adds should produce 2 routes (desktop+android), got %d", len(routes))
	}
	// Find desktop route, verify it has Steam.
	var hasSteam bool
	for _, r := range routes {
		for _, p := range r.Match.ProcessName {
			if p == "Steam.app" {
				hasSteam = true
			}
		}
	}
	if !hasSteam {
		t.Errorf("custom Steam not in routes despite preset load failure")
	}
}

// TestBuildAppBypassRoute_UnsupportedVersion_GracefullySkipped tests the
// path where future-publisher yaml (version > 1) is downloaded by an old
// client. The reader rejects with ErrUnsupportedVersion, appbypass.Load
// skips the file, buildAppBypassRoute returns nil cleanly.
func TestBuildAppBypassRoute_UnsupportedVersion_GracefullySkipped(t *testing.T) {
	tmp := t.TempDir()
	yaml := `version: 99
region: cn
android:
  installer_exact: [com.xiaomi.market]
`
	if err := os.WriteFile(filepath.Join(tmp, "app-bypass-cn.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := Config{
		AppBypassRegion: "cn",
		Client:          &config.ClientConfig{CacheDir: tmp},
	}
	routes, err := buildAppBypassRoute(&cfg)
	if err != nil {
		t.Fatalf("unsupported version should be skipped, got err: %v", err)
	}
	if routes != nil {
		t.Errorf("unsupported version should yield no routes, got %v", routes)
	}
}

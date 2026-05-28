package daemon

import (
	"runtime"
	"testing"
)

func TestAPI_ClassifyApps_RegionMatchesProcessName(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin-specific case-preserved input check")
	}
	d := New()
	cacheDir := t.TempDir()
	d.ruleCacheDir = cacheDir
	writeCNBundle(t, cacheDir)

	params := map[string]any{
		"region": "cn",
		"installed": []map[string]any{
			{"id": "/Applications/WeChat.app", "label": "WeChat",
				"process_names": []string{"WeChat", "WeChatHelper"}},
		},
	}
	resp := callCore(t, d, "classify-apps", params)
	if resp.Code != 0 {
		t.Fatalf("code %d msg %q", resp.Code, resp.Message)
	}
	data := decodeData[classifyAppsData](t, resp)
	if len(data.Classifications) != 1 {
		t.Fatalf("want 1 classification; got %d: %+v", len(data.Classifications), data.Classifications)
	}
	got := data.Classifications[0]
	if got.ID != "/Applications/WeChat.app" {
		t.Errorf("ID = %q, want WeChat path", got.ID)
	}
	if got.Default != "direct" {
		t.Errorf("Default = %q, want direct", got.Default)
	}
	if got.HitPattern == "" {
		t.Errorf("HitPattern should be populated; got empty")
	}
}

func TestAPI_ClassifyApps_LowercaseProcessNameDoesNotMatchDarwin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin case-sensitivity regression guard")
	}
	d := New()
	cacheDir := t.TempDir()
	d.ruleCacheDir = cacheDir
	writeCNBundle(t, cacheDir)

	params := map[string]any{
		"region": "cn",
		"installed": []map[string]any{
			{"id": "/Applications/WeChat.app", "label": "WeChat",
				"process_names": []string{"wechat", "wechathelper"}},
		},
	}
	resp := callCore(t, d, "classify-apps", params)
	if resp.Code != 0 {
		t.Fatalf("code %d msg %q", resp.Code, resp.Message)
	}
	data := decodeData[classifyAppsData](t, resp)
	if data.Classifications[0].Default != "proxy" {
		t.Errorf("lowercase input must classify as proxy; got %+v", data.Classifications[0])
	}
}

func TestAPI_ClassifyApps_UnknownRegionReturnsAllProxy(t *testing.T) {
	d := New()
	cacheDir := t.TempDir()
	d.ruleCacheDir = cacheDir
	// No bundle written — every app falls through to default proxy.

	params := map[string]any{
		"region": "zz",
		"installed": []map[string]any{
			{"id": "x", "label": "X", "process_names": []string{"x"}},
		},
	}
	resp := callCore(t, d, "classify-apps", params)
	if resp.Code != 0 {
		t.Fatalf("code %d msg %q", resp.Code, resp.Message)
	}
	data := decodeData[classifyAppsData](t, resp)
	if len(data.Classifications) != 1 || data.Classifications[0].Default != "proxy" {
		t.Errorf("unknown region → all proxy; got %+v", data.Classifications)
	}
}

func TestAPI_ClassifyApps_RespectsAndroidInstallerPriority(t *testing.T) {
	if runtime.GOOS != "android" {
		t.Skip("Android installer-priority test")
		// NOTE: desktop CI always skips this — exercise via an Android test run.
	}
	d := New()
	cacheDir := t.TempDir()
	d.ruleCacheDir = cacheDir
	writeCNBundleAndroid(t, cacheDir)

	params := map[string]any{
		"region": "cn",
		"installed": []map[string]any{
			{"id": "com.foo.bar", "label": "Foo",
				"installer_package_name": "com.tencent.android.qqdownloader",
				"process_names":          []string{"com.foo.bar"}},
		},
	}
	resp := callCore(t, d, "classify-apps", params)
	if resp.Code != 0 {
		t.Fatalf("code %d", resp.Code)
	}
	data := decodeData[classifyAppsData](t, resp)
	if data.Classifications[0].HitKind != "installer" {
		t.Errorf("HitKind = %q, want installer", data.Classifications[0].HitKind)
	}
}

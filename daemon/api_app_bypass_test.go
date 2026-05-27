package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/kaitu-io/k2-rules/krs"
	"github.com/kaitu-io/k2/provider"
)

// writeCNBundle drops a minimal cn.krs file in dir with WeChat patterns for
// Darwin + Windows. Used by app-bypass-preview tests.
func writeCNBundle(t *testing.T, dir string) {
	t.Helper()
	var buf bytes.Buffer
	if err := krs.WriteBundle(&buf, &krs.Bundle{
		Apps: &krs.AppPatterns{
			Darwin:  krs.DarwinPatterns{Apps: []string{"WeChat*"}},
			Windows: krs.WindowsPatterns{Apps: []string{"WeChat*"}},
		},
	}); err != nil {
		t.Fatalf("krs.WriteBundle: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cn.krs"), buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write preset: %v", err)
	}
}

// callCore posts a {action, params} body to handleCore and returns the
// decoded Response. Mirrors how the webapp talks to daemon.
func callCore(t *testing.T, d *Daemon, action string, params map[string]any) Response {
	t.Helper()
	body, _ := json.Marshal(Request{Action: action, Params: params})
	req := httptest.NewRequest(http.MethodPost, "/api/core", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	d.handleCore(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("HTTP status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp Response
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, rec.Body.String())
	}
	return resp
}

// decodeData re-marshals resp.Data into the requested type. Daemon's
// Response.Data is `any`, so callers do a second-pass unmarshal — same
// pattern the webapp uses.
func decodeData[T any](t *testing.T, resp Response) T {
	t.Helper()
	raw, _ := json.Marshal(resp.Data)
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	return out
}

func TestAPI_AppBypassGet_Empty(t *testing.T) {
	d := New()
	resp := callCore(t, d, "app-bypass-get", nil)
	if resp.Code != 0 {
		t.Errorf("Response.Code = %d, want 0; message=%s", resp.Code, resp.Message)
	}
	state := decodeData[appBypassState](t, resp)
	if state.FeatureSupported != isAppBypassPlatformSupported() {
		t.Errorf("FeatureSupported mismatch")
	}
	if state.Region != "" {
		t.Errorf("Region should be empty on fresh daemon, got %q", state.Region)
	}
}

func TestAPI_AppBypassGet_Populated(t *testing.T) {
	d := New()
	d.appBypassOverrides = AppBypassOverrides{
		Region:      "cn",
		ProcessAdds: []string{"Steam"},
	}
	resp := callCore(t, d, "app-bypass-get", nil)
	state := decodeData[appBypassState](t, resp)
	if state.Region != "cn" {
		t.Errorf("Region = %q, want cn", state.Region)
	}
	if !containsAppBypassStr(state.Custom.ProcessAdds, "Steam") {
		t.Errorf("ProcessAdds should contain Steam")
	}
}

func TestAPI_AppBypassSetCustom_AddRemove(t *testing.T) {
	// Isolated state.json
	tmpDir := t.TempDir()
	origDir := stateDir
	stateDir = func() string { return tmpDir }
	defer func() { stateDir = origDir }()

	d := New()

	resp := callCore(t, d, "app-bypass-set-custom", map[string]any{
		"add":    map[string]any{"process": []string{"Steam"}, "package": []string{"com.user.app"}},
		"remove": map[string]any{"process": []string{}, "package": []string{}},
	})
	if resp.Code != 0 {
		t.Fatalf("set-custom code = %d (%s)", resp.Code, resp.Message)
	}

	// Verify persistence via loadState (same path daemon uses on next startup).
	st, err := loadState()
	if err != nil {
		t.Fatalf("loadState: %v", err)
	}
	if st.AppBypass == nil {
		t.Fatal("AppBypass not persisted")
	}
	if !containsAppBypassStr(st.AppBypass.ProcessAdds, "Steam") {
		t.Errorf("persistence: ProcessAdds should contain Steam, got %v", st.AppBypass.ProcessAdds)
	}

	// Remove it.
	resp2 := callCore(t, d, "app-bypass-set-custom", map[string]any{
		"add":    map[string]any{"process": []string{}, "package": []string{}},
		"remove": map[string]any{"process": []string{"Steam"}, "package": []string{}},
	})
	if resp2.Code != 0 {
		t.Fatalf("remove code = %d", resp2.Code)
	}
	st2, _ := loadState()
	if st2.AppBypass != nil && containsAppBypassStr(st2.AppBypass.ProcessAdds, "Steam") {
		t.Errorf("Steam should have been removed; got %v", st2.AppBypass.ProcessAdds)
	}
}

func TestAPI_AppBypassSetCustom_DedupOnRepeat(t *testing.T) {
	tmpDir := t.TempDir()
	origDir := stateDir
	stateDir = func() string { return tmpDir }
	defer func() { stateDir = origDir }()

	d := New()
	payload := map[string]any{
		"add":    map[string]any{"process": []string{"Steam"}, "package": []string{}},
		"remove": map[string]any{"process": []string{}, "package": []string{}},
	}
	for i := 0; i < 3; i++ {
		callCore(t, d, "app-bypass-set-custom", payload)
	}
	st, _ := loadState()
	if st.AppBypass == nil {
		t.Fatal("not persisted")
	}
	count := 0
	for _, s := range st.AppBypass.ProcessAdds {
		if s == "Steam" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected Steam dedup'd to 1 entry, got %d", count)
	}
}

func TestAPI_AppBypassSetRegion(t *testing.T) {
	tmpDir := t.TempDir()
	origDir := stateDir
	stateDir = func() string { return tmpDir }
	defer func() { stateDir = origDir }()

	d := New()
	resp := callCore(t, d, "app-bypass-set-region", map[string]any{"region": "cn"})
	if resp.Code != 0 { t.Fatalf("code = %d (%s)", resp.Code, resp.Message) }
	st, _ := loadState()
	if st.AppBypass == nil || st.AppBypass.Region != "cn" {
		t.Errorf("Region should persist as cn, got %+v", st.AppBypass)
	}

	// Clear region
	callCore(t, d, "app-bypass-set-region", map[string]any{"region": ""})
	st2, _ := loadState()
	if st2.AppBypass != nil && st2.AppBypass.Region != "" {
		t.Errorf("Region should clear, got %q", st2.AppBypass.Region)
	}
}

func TestAPI_AppBypassSetRegion_RejectsInvalidLength(t *testing.T) {
	tmpDir := t.TempDir()
	origDir := stateDir
	stateDir = func() string { return tmpDir }
	defer func() { stateDir = origDir }()

	d := New()
	resp := callCore(t, d, "app-bypass-set-region", map[string]any{"region": "CHINA"})
	if resp.Code == 0 {
		t.Errorf("expected error code for invalid region length")
	}
}

type appBypassPreviewData struct {
	Matched []map[string]any `json:"matched"`
	Region  string           `json:"region"`
}

// fakeAppListProvider returns a fixed list for tests.
type fakeAppListProvider struct {
	apps []provider.InstalledApp
	err  error
}

func (f fakeAppListProvider) ListInstalled(_ context.Context) ([]provider.InstalledApp, error) {
	return f.apps, f.err
}

func TestAPI_AppBypassPreview_NoRegion_EmptyMatched(t *testing.T) {
	d := New()
	d.appListProvider = fakeAppListProvider{apps: []provider.InstalledApp{
		{ID: "/Applications/WeChat.app", Label: "WeChat", ProcessNames: []string{"wechat"}},
	}}
	resp := callCore(t, d, "app-bypass-preview", nil)
	if resp.Code != 0 {
		t.Fatalf("code %d msg %q", resp.Code, resp.Message)
	}
	preview := decodeData[appBypassPreviewData](t, resp)
	if preview.Region != "" {
		t.Errorf("region should be empty, got %q", preview.Region)
	}
	if len(preview.Matched) != 0 {
		t.Fatalf("want empty (no region), got %d", len(preview.Matched))
	}
}

func TestAPI_AppBypassPreview_PresetNotLoaded_EmptyMatched(t *testing.T) {
	d := New()
	// Point at an empty cache dir so no preset is found.
	d.ruleCacheDir = t.TempDir()
	d.appBypassOverrides.Region = "cn"
	d.appListProvider = fakeAppListProvider{apps: []provider.InstalledApp{
		{ID: "/Applications/WeChat.app", Label: "WeChat", ProcessNames: []string{"wechat"}},
	}}
	resp := callCore(t, d, "app-bypass-preview", nil)
	if resp.Code != 0 {
		t.Fatalf("code %d", resp.Code)
	}
	preview := decodeData[appBypassPreviewData](t, resp)
	if preview.Region != "cn" {
		t.Errorf("region should be 'cn', got %q", preview.Region)
	}
	if len(preview.Matched) != 0 {
		t.Errorf("want empty (no preset on disk), got %d", len(preview.Matched))
	}
}

func TestAPI_AppBypassPreview_MatchesPreset(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("hit_pattern assertion uses Darwin case-sensitive form; krs lowercases Windows and has no Linux patterns")
	}
	d := New()
	cacheDir := t.TempDir()
	d.ruleCacheDir = cacheDir
	writeCNBundle(t, cacheDir)

	d.appBypassOverrides.Region = "cn"
	d.appListProvider = fakeAppListProvider{apps: []provider.InstalledApp{
		{ID: "/Applications/WeChat.app", Label: "WeChat", ProcessNames: []string{"WeChat", "WeChatHelper"}},
		{ID: "/Applications/Firefox.app", Label: "Firefox", ProcessNames: []string{"firefox"}},
	}}

	resp := callCore(t, d, "app-bypass-preview", nil)
	if resp.Code != 0 {
		t.Fatalf("code %d msg %q", resp.Code, resp.Message)
	}
	preview := decodeData[appBypassPreviewData](t, resp)
	if preview.Region != "cn" {
		t.Errorf("region %q want cn", preview.Region)
	}
	if len(preview.Matched) != 1 {
		t.Fatalf("want 1 match, got %d: %v", len(preview.Matched), preview.Matched)
	}
	hit := preview.Matched[0]
	if hit["hit_kind"] != "app" {
		t.Errorf("hit_kind %v want app", hit["hit_kind"])
	}
	if hit["hit_pattern"] != "WeChat*" {
		t.Errorf("hit_pattern %v want WeChat*", hit["hit_pattern"])
	}
	if hit["label"] != "WeChat" {
		t.Errorf("label %v want WeChat", hit["label"])
	}
}

func TestAPI_AppBypassPreview_ProviderError_EmptyMatched(t *testing.T) {
	d := New()
	cacheDir := t.TempDir()
	d.ruleCacheDir = cacheDir
	writeCNBundle(t, cacheDir)
	d.appBypassOverrides.Region = "cn"
	d.appListProvider = fakeAppListProvider{err: context.Canceled}

	resp := callCore(t, d, "app-bypass-preview", nil)
	if resp.Code != 0 {
		t.Fatalf("code %d", resp.Code)
	}
	preview := decodeData[appBypassPreviewData](t, resp)
	if len(preview.Matched) != 0 {
		t.Errorf("provider error should yield empty list, got %v", preview.Matched)
	}
	if preview.Region != "cn" {
		t.Errorf("region should still be reported, got %q", preview.Region)
	}
}

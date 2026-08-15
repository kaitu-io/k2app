//go:build linux

package webui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestHasFileExtension(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/", false},
		{"/dashboard", false},
		{"/account/settings", false},
		{"/assets/app.js", true},
		{"/assets/style.css", true},
		{"/favicon.png", true},
		{"/assets/chunk-abc123.js", true},
	}
	for _, tc := range cases {
		got := hasFileExtension(tc.path)
		if got != tc.want {
			t.Errorf("hasFileExtension(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestNewWebappHandlerNilFS(t *testing.T) {
	orig := webappFS
	webappFS = nil
	defer func() { webappFS = orig }()

	h := NewWebappHandler("1.0.0", "abc123", "amd64")
	if h != nil {
		t.Errorf("expected nil handler when webappFS is nil, got %T", h)
	}
}

func TestWebappHandlerInjectsGatewayGlobal(t *testing.T) {
	if webappFS == nil {
		t.Skip("webappFS is nil (nowebapp build)")
	}

	h := NewWebappHandler("1.2.3", "deadbeef", "arm64")
	if h == nil {
		t.Fatal("NewWebappHandler returned nil")
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if ct != "text/html; charset=utf-8" {
		t.Errorf("unexpected Content-Type: %q", ct)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	bodyStr := string(body)

	checks := []string{
		"__K2_GATEWAY__",
		"1.2.3",
	}
	for _, needle := range checks {
		if !strings.Contains(bodyStr, needle) {
			t.Errorf("response body missing %q", needle)
		}
	}
}

func TestWebappHandlerSPAFallback(t *testing.T) {
	if webappFS == nil {
		t.Skip("webappFS is nil (nowebapp build)")
	}

	h := NewWebappHandler("0.0.1", "cafebabe", "amd64")
	if h == nil {
		t.Fatal("NewWebappHandler returned nil")
	}

	req := httptest.NewRequest(http.MethodGet, "/dashboard/settings", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for SPA fallback, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if ct != "text/html; charset=utf-8" {
		t.Errorf("unexpected Content-Type for SPA fallback: %q", ct)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	if !strings.Contains(string(body), "<html") {
		t.Errorf("SPA fallback did not return HTML content")
	}
}

// --- Web OTA disk override ---

// fakeEmbed swaps webappFS for an in-memory FS so override tests do not
// depend on a populated dist/. Restores the original via t.Cleanup.
func fakeEmbed(t *testing.T) {
	t.Helper()
	orig := webappFS
	webappFS = fstest.MapFS{
		"dist/index.html":          &fstest.MapFile{Data: []byte("<html><head></head><body>EMBEDDED-UI</body></html>")},
		"dist/assets/embed-only.js": &fstest.MapFile{Data: []byte("console.log('embed')")},
	}
	t.Cleanup(func() { webappFS = orig })
}

// writeOverride creates a web-ota "current" bundle on disk.
func writeOverride(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"index.html":         "<html><head></head><body>OTA-UI</body></html>",
		"assets/ota-only.js": "console.log('ota')",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func get(t *testing.T, h http.Handler, target string) (*http.Response, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	resp := w.Result()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, string(body)
}

func TestOverrideServesDiskIndexWithInjection(t *testing.T) {
	fakeEmbed(t)
	h := NewWebappHandlerWithOverride("1.2.3", "cafe", "amd64", writeOverride(t))
	resp, body := get(t, h, "/")
	if !strings.Contains(body, "OTA-UI") {
		t.Errorf("expected disk index, got %q", body)
	}
	if !strings.Contains(body, "__K2_GATEWAY__") || !strings.Contains(body, "1.2.3") {
		t.Errorf("disk index missing __K2_GATEWAY__ injection: %q", body)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("HTML Cache-Control = %q, want no-cache", cc)
	}
}

func TestOverrideSPAFallbackServesDiskIndex(t *testing.T) {
	fakeEmbed(t)
	h := NewWebappHandlerWithOverride("1.2.3", "cafe", "amd64", writeOverride(t))
	_, body := get(t, h, "/dashboard/settings")
	if !strings.Contains(body, "OTA-UI") {
		t.Errorf("SPA fallback should serve disk index, got %q", body)
	}
}

func TestEmbeddedEscapeHatch(t *testing.T) {
	fakeEmbed(t)
	h := NewWebappHandlerWithOverride("1.2.3", "cafe", "amd64", writeOverride(t))
	_, body := get(t, h, "/?ui=embedded")
	if !strings.Contains(body, "EMBEDDED-UI") {
		t.Errorf("?ui=embedded should force embedded index, got %q", body)
	}
	if !strings.Contains(body, "__K2_GATEWAY__") {
		t.Errorf("embedded escape hatch lost injection: %q", body)
	}
}

func TestOverrideAssetDiskFirstEmbedFallback(t *testing.T) {
	fakeEmbed(t)
	h := NewWebappHandlerWithOverride("1.2.3", "cafe", "amd64", writeOverride(t))

	resp, body := get(t, h, "/assets/ota-only.js")
	if !strings.Contains(body, "console.log('ota')") {
		t.Errorf("disk asset not served: %q", body)
	}
	if cc := resp.Header.Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("asset Cache-Control = %q, want immutable", cc)
	}

	_, body = get(t, h, "/assets/embed-only.js")
	if !strings.Contains(body, "console.log('embed')") {
		t.Errorf("embed fallback asset not served: %q", body)
	}
}

func TestOverrideDirAbsentServesEmbedded(t *testing.T) {
	fakeEmbed(t)
	// 目录不存在（poller 从未跑过）与空串（gateway 调用路径）都应回落 embed。
	for _, dir := range []string{"", filepath.Join(t.TempDir(), "never-created")} {
		h := NewWebappHandlerWithOverride("1.2.3", "cafe", "amd64", dir)
		_, body := get(t, h, "/")
		if !strings.Contains(body, "EMBEDDED-UI") {
			t.Errorf("overrideDir=%q: expected embedded index, got %q", dir, body)
		}
	}
}

func TestOverrideSwapVisibleWithoutReconstruction(t *testing.T) {
	fakeEmbed(t)
	dir := writeOverride(t)
	h := NewWebappHandlerWithOverride("1.2.3", "cafe", "amd64", dir)
	if _, body := get(t, h, "/"); !strings.Contains(body, "OTA-UI") {
		t.Fatalf("precondition failed: %q", body)
	}
	// 模拟 poller swap：index.html 内容变化，同一个 handler 下一次请求即见新内容。
	if err := os.WriteFile(filepath.Join(dir, "index.html"),
		[]byte("<html><head></head><body>OTA-UI-V2</body></html>"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, body := get(t, h, "/"); !strings.Contains(body, "OTA-UI-V2") {
		t.Errorf("swap not visible on next page load: %q", body)
	}
}

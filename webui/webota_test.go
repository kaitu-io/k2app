//go:build linux

package webui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

const testManifestJSON = `{
	"version": "0.4.9.1234",
	"url": "0.4.9.1234/web.zip",
	"hash": "sha256:deadbeef",
	"size": 42,
	"sig": "",
	"min_linux": "0.4.9",
	"min_bridge": 1,
	"released_at": "2026-08-14T12:00:00Z",
	"min_native": "0.4.8"
}`

func TestFetchManifestFallsBackToSecondEndpoint(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer bad.Close()
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/kaitu/web/latest.json" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(testManifestJSON))
	}))
	defer good.Close()

	o := NewWebOTA(WebOTAConfig{
		Endpoints:    []string{bad.URL + "/kaitu", good.URL + "/kaitu"},
		Dir:          t.TempDir(),
		ShellVersion: "0.4.9",
	})
	m, endpoint, err := o.fetchManifest()
	if err != nil {
		t.Fatalf("fetchManifest: %v", err)
	}
	if endpoint != good.URL+"/kaitu" {
		t.Errorf("endpoint = %q, want the good one", endpoint)
	}
	// 断言解析后的字段值（不要对原始 JSON 字符串断言 —— Go 会把 & 转义成 &）
	if m.Version != "0.4.9.1234" || m.URL != "0.4.9.1234/web.zip" ||
		m.MinLinux != "0.4.9" || m.MinBridge != 1 || m.Size != 42 {
		t.Errorf("manifest fields wrong: %+v", m)
	}
}

func TestFetchManifestAllEndpointsFail(t *testing.T) {
	bad := httptest.NewServer(http.NotFoundHandler())
	defer bad.Close()
	o := NewWebOTA(WebOTAConfig{Endpoints: []string{bad.URL + "/kaitu"}, Dir: t.TempDir(), ShellVersion: "0.4.9"})
	if _, _, err := o.fetchManifest(); err == nil {
		t.Fatal("expected error when all endpoints fail")
	}
}

func TestLocalVersionFallsBackToShellVersion(t *testing.T) {
	o := NewWebOTA(WebOTAConfig{Dir: t.TempDir(), ShellVersion: "0.4.9"})
	if v := o.localVersion(); v != "0.4.9" {
		t.Errorf("localVersion = %q, want shell fallback 0.4.9", v)
	}
}

func TestLocalVersionReadsVersionTxt(t *testing.T) {
	dir := t.TempDir()
	o := NewWebOTA(WebOTAConfig{Dir: dir, ShellVersion: "0.4.9"})
	if err := os.MkdirAll(o.CurrentDir(), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(o.CurrentDir(), "version.txt"), []byte("0.4.9.1000\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if v := o.localVersion(); v != "0.4.9.1000" {
		t.Errorf("localVersion = %q, want 0.4.9.1000", v)
	}
}

func TestShouldApplyGates(t *testing.T) {
	o := NewWebOTA(WebOTAConfig{Dir: t.TempDir(), ShellVersion: "0.4.9"})
	cases := []struct {
		name string
		m    WebOTAManifest
		want bool
	}{
		{"newer passes", WebOTAManifest{Version: "0.4.9.1234", MinLinux: "0.4.9", MinBridge: 1}, true},
		{"min_linux blocks", WebOTAManifest{Version: "0.4.9.1234", MinLinux: "0.5.0", MinBridge: 1}, false},
		{"min_bridge blocks", WebOTAManifest{Version: "0.4.9.1234", MinLinux: "0.4.9", MinBridge: LinuxBridgeVersion + 1}, false},
		{"not newer blocks", WebOTAManifest{Version: "0.4.9", MinLinux: "", MinBridge: 0}, false},
		{"empty gates pass", WebOTAManifest{Version: "0.4.9.1", MinLinux: "", MinBridge: 0}, true},
	}
	for _, tc := range cases {
		if got, reason := o.shouldApply(&tc.m); got != tc.want {
			t.Errorf("%s: shouldApply = %v (%s), want %v", tc.name, got, reason, tc.want)
		}
	}
}

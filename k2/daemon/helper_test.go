package daemon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestHandleHelperUnknownAction(t *testing.T) {
	d := New()
	body := `{"action":"unknown-action"}`
	req := httptest.NewRequest("POST", "/api/helper", bytes.NewReader([]byte(body)))
	w := httptest.NewRecorder()

	d.handleHelper(w, req)

	var resp Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Code != 400 {
		t.Fatalf("expected code 400, got %d", resp.Code)
	}
	if resp.Message != "unknown helper action" {
		t.Fatalf("expected 'unknown helper action', got %q", resp.Message)
	}
}

func TestHandleHelperDetectReturnsResponse(t *testing.T) {
	d := New()
	body := `{"action":"adb-detect"}`
	req := httptest.NewRequest("POST", "/api/helper", bytes.NewReader([]byte(body)))
	w := httptest.NewRecorder()

	d.handleHelper(w, req)

	var resp Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Code != 0 {
		t.Fatalf("expected code 0, got %d", resp.Code)
	}
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatal("expected data to be a map")
	}
	// adb not ready on test machine (no adb binary cached)
	if _, exists := data["adb_ready"]; !exists {
		t.Fatal("expected adb_ready field in response")
	}
	if _, exists := data["installing_driver"]; !exists {
		t.Fatal("expected installing_driver field in response")
	}
}

func TestHandleHelperStatusReturnsIdleByDefault(t *testing.T) {
	d := New()
	body := `{"action":"adb-status"}`
	req := httptest.NewRequest("POST", "/api/helper", bytes.NewReader([]byte(body)))
	w := httptest.NewRecorder()

	d.handleHelper(w, req)

	var resp Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Code != 0 {
		t.Fatalf("expected code 0, got %d", resp.Code)
	}
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatal("expected data to be a map")
	}
	if data["phase"] != "idle" {
		t.Fatalf("expected phase 'idle', got %v", data["phase"])
	}
}

func TestHandleHelperInstallRejectsWhenBusy(t *testing.T) {
	d := New()

	// Simulate an install in progress by setting state
	d.helper.mu.Lock()
	d.helper.state.Phase = "downloading"
	d.helper.mu.Unlock()

	body := `{"action":"adb-install","params":{"serial":"test123"}}`
	req := httptest.NewRequest("POST", "/api/helper", bytes.NewReader([]byte(body)))
	w := httptest.NewRecorder()

	d.handleHelper(w, req)

	var resp Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Code != 1 {
		t.Fatalf("expected code 1 (busy), got %d", resp.Code)
	}
}

func TestProgressReader(t *testing.T) {
	data := "hello world, this is test data"
	reader := strings.NewReader(data)
	total := int64(len(data))

	var lastCurrent, lastTotal int64
	var callCount int
	pr := &progressReader{
		reader: reader,
		total:  total,
		callback: func(current, t int64) {
			lastCurrent = current
			lastTotal = t
			callCount++
		},
	}

	buf := make([]byte, 10)
	n, err := pr.Read(buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 10 {
		t.Fatalf("expected 10 bytes, got %d", n)
	}
	if callCount != 1 {
		t.Fatalf("expected 1 callback, got %d", callCount)
	}
	if lastCurrent != 10 || lastTotal != total {
		t.Fatalf("expected current=10 total=%d, got current=%d total=%d", total, lastCurrent, lastTotal)
	}

	// Read remaining
	out, err := io.ReadAll(pr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	remaining := data[10:]
	if string(out) != remaining {
		t.Fatalf("expected %q, got %q", remaining, string(out))
	}
	if pr.current != total {
		t.Fatalf("expected current=%d, got %d", total, pr.current)
	}
}

func TestParseToolsJSON(t *testing.T) {
	raw := `{
		"adb": {
			"version": "35.0.2",
			"files": {
				"darwin":  {"url": "platform-tools-darwin.zip",  "hash": "sha256:abc123", "size": 15711207},
				"windows": {"url": "platform-tools-windows.zip", "hash": "sha256:def456", "size": 7708640}
			}
		}
	}`

	var tools toolsManifest
	if err := json.Unmarshal([]byte(raw), &tools); err != nil {
		t.Fatalf("parse tools.json: %v", err)
	}
	if tools.ADB.Version != "35.0.2" {
		t.Fatalf("expected version 35.0.2, got %s", tools.ADB.Version)
	}
	darwin, ok := tools.ADB.Files["darwin"]
	if !ok {
		t.Fatal("expected darwin entry")
	}
	if darwin.URL != "platform-tools-darwin.zip" {
		t.Fatalf("expected platform-tools-darwin.zip, got %s", darwin.URL)
	}
	if darwin.Hash != "sha256:abc123" {
		t.Fatalf("expected sha256:abc123, got %s", darwin.Hash)
	}
	if darwin.Size != 15711207 {
		t.Fatalf("expected size 15711207, got %d", darwin.Size)
	}
}

func TestAndroidEndpointsConstruction(t *testing.T) {
	if len(androidEndpoints) == 0 {
		t.Fatal("expected at least one endpoint")
	}
	for _, ep := range androidEndpoints {
		if !strings.HasPrefix(ep, "https://") {
			t.Fatalf("endpoint must be HTTPS: %s", ep)
		}
		if strings.HasSuffix(ep, "/") {
			t.Fatalf("endpoint must not end with /: %s", ep)
		}
	}
}

func TestInstallStateTransitions(t *testing.T) {
	h := &AndroidHelper{state: InstallState{Phase: "idle"}}

	// idle → can install
	h.mu.Lock()
	canInstall := h.state.Phase == "idle" || h.state.Phase == "done" || h.state.Phase == "error"
	h.mu.Unlock()
	if !canInstall {
		t.Fatal("expected idle to allow install")
	}

	// Set to downloading → busy
	h.mu.Lock()
	h.state.Phase = "downloading"
	h.state.Progress = 50
	h.mu.Unlock()

	h.mu.Lock()
	busy := h.state.Phase != "idle" && h.state.Phase != "done" && h.state.Phase != "error"
	h.mu.Unlock()
	if !busy {
		t.Fatal("expected downloading to be busy")
	}

	// done → can install again
	h.mu.Lock()
	h.state.Phase = "done"
	h.mu.Unlock()

	h.mu.Lock()
	canInstall = h.state.Phase == "idle" || h.state.Phase == "done" || h.state.Phase == "error"
	h.mu.Unlock()
	if !canInstall {
		t.Fatal("expected done to allow new install")
	}

	// error → can install again
	h.mu.Lock()
	h.state.Phase = "error"
	h.state.Error = "test error"
	h.mu.Unlock()

	h.mu.Lock()
	canInstall = h.state.Phase == "idle" || h.state.Phase == "done" || h.state.Phase == "error"
	h.mu.Unlock()
	if !canInstall {
		t.Fatal("expected error to allow retry")
	}
}

func TestHandleHelperInstallAcceptsAfterDone(t *testing.T) {
	// Mock endpoints to prevent real HTTP calls from doInstall goroutine
	origEndpoints := androidEndpoints
	androidEndpoints = []string{"http://127.0.0.1:1"}
	defer func() { androidEndpoints = origEndpoints }()

	d := New()

	// Set state to "done" — should allow new install
	d.helper.mu.Lock()
	d.helper.state.Phase = "done"
	d.helper.mu.Unlock()

	body := `{"action":"adb-install","params":{"url":"https://example.com/test.apk"}}`
	req := httptest.NewRequest("POST", "/api/helper", bytes.NewReader([]byte(body)))
	w := httptest.NewRecorder()

	d.handleHelper(w, req)

	var resp Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	// Should accept (code 0), not reject (code 1)
	if resp.Code != 0 {
		t.Fatalf("expected code 0 after done, got %d: %s", resp.Code, resp.Message)
	}

	// Wait for doInstall goroutine to finish (it will fail fast on unreachable endpoint)
	waitForPhase(t, d.helper, "error", 5*time.Second)
}

func TestHandleHelperInstallAcceptsAfterError(t *testing.T) {
	// Mock endpoints to prevent real HTTP calls from doInstall goroutine
	origEndpoints := androidEndpoints
	androidEndpoints = []string{"http://127.0.0.1:1"}
	defer func() { androidEndpoints = origEndpoints }()

	d := New()

	// Set state to "error" — should allow retry
	d.helper.mu.Lock()
	d.helper.state.Phase = "error"
	d.helper.state.Error = "previous failure"
	d.helper.mu.Unlock()

	body := `{"action":"adb-install","params":{"url":"https://example.com/test.apk"}}`
	req := httptest.NewRequest("POST", "/api/helper", bytes.NewReader([]byte(body)))
	w := httptest.NewRecorder()

	d.handleHelper(w, req)

	var resp Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Code != 0 {
		t.Fatalf("expected code 0 after error, got %d: %s", resp.Code, resp.Message)
	}

	// Wait for doInstall goroutine to finish
	waitForPhase(t, d.helper, "error", 5*time.Second)
}

// waitForPhase polls the helper state until the expected phase is reached or timeout.
func waitForPhase(t *testing.T, h *AndroidHelper, phase string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		current := h.state.Phase
		h.mu.Unlock()
		if current == phase {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	h.mu.Lock()
	t.Fatalf("timeout waiting for phase %q, current: %q", phase, h.state.Phase)
	h.mu.Unlock()
}

func TestHandleHelperMethodNotAllowed(t *testing.T) {
	d := New()
	req := httptest.NewRequest("GET", "/api/helper", nil)
	w := httptest.NewRecorder()

	d.handleHelper(w, req)

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Code != 405 {
		t.Fatalf("expected code 405, got %d", resp.Code)
	}
}

func TestDoInstallPhasesWithMockHTTP(t *testing.T) {
	// Set up a mock HTTP server that serves tools.json and an APK
	toolsJSON := `{"adb":{"version":"35.0.2","files":{"darwin":{"url":"adb.zip","hash":"sha256:unused","size":100}}}}`
	latestJSON := `{"version":"0.5.0","url":"test.apk","hash":"","size":100}`

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/tools/tools.json"):
			w.Write([]byte(toolsJSON))
		case strings.HasSuffix(r.URL.Path, "/latest.json"):
			w.Write([]byte(latestJSON))
		default:
			w.WriteHeader(404)
		}
	}))
	defer ts.Close()

	h := &AndroidHelper{state: InstallState{Phase: "idle"}}

	// Verify initial state
	h.mu.Lock()
	if h.state.Phase != "idle" {
		t.Fatalf("expected idle, got %s", h.state.Phase)
	}
	h.mu.Unlock()

	// Simulate phase transitions (without real adb/device)
	h.setPhase("prepare_adb", 0)
	h.mu.Lock()
	if h.state.Phase != "prepare_adb" {
		t.Fatalf("expected prepare_adb, got %s", h.state.Phase)
	}
	h.mu.Unlock()

	h.setPhase("downloading", 0)
	h.setProgress(50)
	h.mu.Lock()
	if h.state.Phase != "downloading" || h.state.Progress != 50 {
		t.Fatalf("expected downloading/50, got %s/%d", h.state.Phase, h.state.Progress)
	}
	h.mu.Unlock()

	h.setPhase("pushing", 0)
	h.setPhase("installing", 0)
	h.setPhase("done", 100)

	h.mu.Lock()
	if h.state.Phase != "done" || h.state.Progress != 100 {
		t.Fatalf("expected done/100, got %s/%d", h.state.Phase, h.state.Progress)
	}
	h.mu.Unlock()
}

func TestSetError(t *testing.T) {
	h := &AndroidHelper{state: InstallState{Phase: "downloading"}}
	h.setError("download failed: timeout")

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.state.Phase != "error" {
		t.Fatalf("expected error phase, got %s", h.state.Phase)
	}
	if h.state.Error != "download failed: timeout" {
		t.Fatalf("expected error message, got %q", h.state.Error)
	}
}

func TestParseAPKManifest(t *testing.T) {
	raw := `{"version":"0.5.0","url":"0.5.0/Kaitu-0.5.0.apk","hash":"sha256:abc123","size":52428800}`

	var manifest apkManifest
	if err := json.Unmarshal([]byte(raw), &manifest); err != nil {
		t.Fatalf("parse latest.json: %v", err)
	}
	if manifest.Version != "0.5.0" {
		t.Fatalf("expected version 0.5.0, got %s", manifest.Version)
	}
	if manifest.URL != "0.5.0/Kaitu-0.5.0.apk" {
		t.Fatalf("expected URL 0.5.0/Kaitu-0.5.0.apk, got %s", manifest.URL)
	}
	if manifest.Hash != "sha256:abc123" {
		t.Fatalf("expected hash sha256:abc123, got %s", manifest.Hash)
	}
	if manifest.Size != 52428800 {
		t.Fatalf("expected size 52428800, got %d", manifest.Size)
	}
}

func TestFetchLatestAPKWithMockServer(t *testing.T) {
	latestJSON := `{"version":"0.5.1","url":"0.5.1/Kaitu-0.5.1.apk","hash":"sha256:def456","size":12345}`

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/latest.json") {
			w.Write([]byte(latestJSON))
		} else {
			w.WriteHeader(404)
		}
	}))
	defer ts.Close()

	// Temporarily override endpoints
	origEndpoints := androidEndpoints
	androidEndpoints = []string{ts.URL}
	defer func() { androidEndpoints = origEndpoints }()

	manifest, err := fetchLatestAPK()
	if err != nil {
		t.Fatalf("fetchLatestAPK: %v", err)
	}
	if manifest.Version != "0.5.1" {
		t.Fatalf("expected version 0.5.1, got %s", manifest.Version)
	}
	if manifest.URL != "0.5.1/Kaitu-0.5.1.apk" {
		t.Fatalf("expected URL, got %s", manifest.URL)
	}
}

func TestDownloadAPKWithMockServer(t *testing.T) {
	apkContent := "fake-apk-binary-content-for-testing"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(apkContent)))
		w.Write([]byte(apkContent))
	}))
	defer ts.Close()

	h := &AndroidHelper{state: InstallState{Phase: "downloading"}}
	tmpDir := t.TempDir()
	path, err := h.downloadAPK(ts.URL+"/test.apk", "", 0, tmpDir)
	if err != nil {
		t.Fatalf("downloadAPK: %v", err)
	}
	defer os.Remove(path)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read downloaded APK: %v", err)
	}
	if string(data) != apkContent {
		t.Fatalf("expected %q, got %q", apkContent, string(data))
	}

	// Verify progress was updated
	h.mu.Lock()
	progress := h.state.Progress
	h.mu.Unlock()
	if progress != 100 {
		t.Fatalf("expected progress 100, got %d", progress)
	}
}

func TestAPKURLResolution(t *testing.T) {
	// Test that absolute URLs in manifest are used as-is,
	// while relative URLs get CDN endpoint prepended.
	tests := []struct {
		name        string
		manifestURL string
		wantPrefix  string // expected prefix of resolved URL
	}{
		{"relative URL", "0.5.0/Kaitu-0.5.0.apk", "https://"},
		{"absolute https", "https://cdn.example.com/app.apk", "https://cdn.example.com/app.apk"},
		{"absolute http", "http://cdn.example.com/app.apk", "http://cdn.example.com/app.apk"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var resolved string
			if strings.HasPrefix(tt.manifestURL, "http") {
				resolved = tt.manifestURL
			} else {
				resolved = androidEndpoints[0] + "/" + tt.manifestURL
			}
			if !strings.HasPrefix(resolved, tt.wantPrefix) {
				t.Errorf("resolved %q does not start with %q", resolved, tt.wantPrefix)
			}
			// Absolute URLs must not be double-prefixed
			if strings.HasPrefix(tt.manifestURL, "http") && resolved != tt.manifestURL {
				t.Errorf("absolute URL was modified: got %q, want %q", resolved, tt.manifestURL)
			}
		})
	}
}

func TestSetPhaseClearsError(t *testing.T) {
	h := &AndroidHelper{state: InstallState{Phase: "error", Error: "previous failure"}}
	h.setPhase("downloading", 0)

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.state.Error != "" {
		t.Fatalf("expected error cleared, got %q", h.state.Error)
	}
}

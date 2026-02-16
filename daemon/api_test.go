package daemon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/kaitu-io/k2/engine"
)

func TestHandlePing(t *testing.T) {
	d := New()
	req := httptest.NewRequest("GET", "/ping", nil)
	w := httptest.NewRecorder()

	d.handlePing(w, req)

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.Code != 0 {
		t.Fatalf("expected code 0, got %d", resp.Code)
	}
	if resp.Message != "pong" {
		t.Fatalf("expected pong, got %s", resp.Message)
	}
}

func TestHandleMetrics(t *testing.T) {
	d := New()
	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()

	d.handleMetrics(w, req)

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.Code != 0 {
		t.Fatalf("expected code 0, got %d", resp.Code)
	}

	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatal("expected data to be a map")
	}
	if _, ok := data["goroutines"]; !ok {
		t.Fatal("expected goroutines in metrics")
	}
}

func TestHandleCoreStatus(t *testing.T) {
	d := New()
	body := `{"action":"status"}`
	req := httptest.NewRequest("POST", "/api/core", bytes.NewReader([]byte(body)))
	w := httptest.NewRecorder()

	d.handleCore(w, req)

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.Code != 0 {
		t.Fatalf("expected code 0, got %d", resp.Code)
	}

	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatal("expected data to be a map")
	}
	if data["state"] != StateStopped {
		t.Fatalf("expected state %s, got %v", StateStopped, data["state"])
	}
}

// mockEngineStarter returns an engine without actually starting network connections.
// The engine is in disconnected state, but the daemon tracks state separately.
func mockEngineStarter(cfg engine.Config) (*engine.Engine, error) {
	return engine.New(), nil
}

func TestHandleCoreUpDown(t *testing.T) {
	d := New()
	d.EngineStarter = mockEngineStarter

	// Up with URL.
	upBody := `{"action":"up","params":{"wire_url":"k2v5://test:token@host:443"}}`
	req := httptest.NewRequest("POST", "/api/core", bytes.NewReader([]byte(upBody)))
	w := httptest.NewRecorder()
	d.handleCore(w, req)

	var upResp Response
	json.NewDecoder(w.Body).Decode(&upResp)
	if upResp.Code != 0 {
		t.Fatalf("up: expected code 0, got %d: %s", upResp.Code, upResp.Message)
	}

	if d.State() != StateConnected {
		t.Fatalf("expected state connected, got %s", d.State())
	}

	// Down.
	downBody := `{"action":"down"}`
	req = httptest.NewRequest("POST", "/api/core", bytes.NewReader([]byte(downBody)))
	w = httptest.NewRecorder()
	d.handleCore(w, req)

	var downResp Response
	json.NewDecoder(w.Body).Decode(&downResp)
	if downResp.Code != 0 {
		t.Fatalf("down: expected code 0, got %d", downResp.Code)
	}

	if d.State() != StateStopped {
		t.Fatalf("expected state stopped, got %s", d.State())
	}
}

func TestHandleCoreUpNoConfig(t *testing.T) {
	d := New()

	body := `{"action":"up","params":{}}`
	req := httptest.NewRequest("POST", "/api/core", bytes.NewReader([]byte(body)))
	w := httptest.NewRecorder()
	d.handleCore(w, req)

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Code != 510 {
		t.Fatalf("expected code 510, got %d: %s", resp.Code, resp.Message)
	}
}

func TestHandleCoreVersion(t *testing.T) {
	d := New()
	body := `{"action":"version"}`
	req := httptest.NewRequest("POST", "/api/core", bytes.NewReader([]byte(body)))
	w := httptest.NewRecorder()
	d.handleCore(w, req)

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Code != 0 {
		t.Fatalf("expected code 0, got %d", resp.Code)
	}
}

func TestHandleCoreUnknownAction(t *testing.T) {
	d := New()
	body := `{"action":"unknown"}`
	req := httptest.NewRequest("POST", "/api/core", bytes.NewReader([]byte(body)))
	w := httptest.NewRecorder()
	d.handleCore(w, req)

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Code != 400 {
		t.Fatalf("expected code 400, got %d", resp.Code)
	}
}

func TestHandleCoreMethodNotAllowed(t *testing.T) {
	d := New()
	req := httptest.NewRequest("GET", "/api/core", nil)
	w := httptest.NewRecorder()
	d.handleCore(w, req)

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Code != 405 {
		t.Fatalf("expected code 405, got %d", resp.Code)
	}
}

func TestCORSMiddleware(t *testing.T) {
	handler := corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))

	// Allowed origin.
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Fatalf("expected CORS origin http://localhost:5173, got %s", got)
	}

	// Tauri origin.
	req = httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Origin", "tauri://localhost")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "tauri://localhost" {
		t.Fatalf("expected CORS origin tauri://localhost, got %s", got)
	}

	// Disallowed origin.
	req = httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Origin", "http://evil.com")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected no CORS header, got %s", got)
	}

	// Preflight.
	req = httptest.NewRequest("OPTIONS", "/", nil)
	req.Header.Set("Origin", "http://127.0.0.1:5173")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for preflight, got %d", w.Code)
	}
}

func TestHandleCoreGetConfig(t *testing.T) {
	d := New()
	d.EngineStarter = mockEngineStarter

	// Set some config via up.
	upBody := `{"action":"up","params":{"wire_url":"k2v5://test:tok@host:443"}}`
	req := httptest.NewRequest("POST", "/api/core", bytes.NewReader([]byte(upBody)))
	w := httptest.NewRecorder()
	d.handleCore(w, req)

	// Get config.
	body := `{"action":"get_config"}`
	req = httptest.NewRequest("POST", "/api/core", bytes.NewReader([]byte(body)))
	w = httptest.NewRecorder()
	d.handleCore(w, req)

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Code != 0 {
		t.Fatalf("expected code 0, got %d", resp.Code)
	}

	data := resp.Data.(map[string]any)
	if data["wire_url"] != "k2v5://test:tok@host:443" {
		t.Fatalf("expected wire_url, got %v", data["wire_url"])
	}
}

func TestHandleDeviceUDID(t *testing.T) {
	d := New()
	req := httptest.NewRequest("GET", "/api/device/udid", nil)
	w := httptest.NewRecorder()

	d.handleDeviceUDID(w, req)

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.Code != 0 {
		t.Fatalf("expected code 0, got %d", resp.Code)
	}
	if resp.Message != "ok" {
		t.Fatalf("expected message ok, got %s", resp.Message)
	}

	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatal("expected data to be a map")
	}
	udid, ok := data["udid"].(string)
	if !ok || udid == "" {
		t.Fatal("expected non-empty udid string")
	}
	if len(udid) != 64 {
		t.Fatalf("expected 64-char udid, got %d chars: %s", len(udid), udid)
	}
}

func TestWebappServing(t *testing.T) {
	handler := webappHandler()

	// GET / should return index.html content.
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /: expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "k2") {
		t.Fatalf("GET /: expected body to contain 'k2', got %s", body)
	}
}

func TestWebappServing_SPAFallback(t *testing.T) {
	handler := webappHandler()

	// GET /dashboard (non-existent file) should fallback to index.html.
	req := httptest.NewRequest("GET", "/dashboard", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /dashboard: expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "k2") {
		t.Fatalf("GET /dashboard: expected index.html content, got %s", body)
	}
}

func TestWebappServing_APIpriority(t *testing.T) {
	// Build full mux like daemon.Run() does to verify API routes take priority.
	d := New()
	mux := http.NewServeMux()
	mux.HandleFunc("/ping", d.handlePing)
	mux.HandleFunc("/metrics", d.handleMetrics)
	mux.HandleFunc("/api/core", d.handleCore)
	mux.HandleFunc("/api/device/udid", d.handleDeviceUDID)
	mux.Handle("/", webappHandler())

	// /ping should return pong, not index.html.
	req := httptest.NewRequest("GET", "/ping", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Message != "pong" {
		t.Fatalf("/ping: expected pong, got %s", resp.Message)
	}

	// /api/core POST should work as before.
	coreBody := `{"action":"status"}`
	req = httptest.NewRequest("POST", "/api/core", bytes.NewReader([]byte(coreBody)))
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Code != 0 {
		t.Fatalf("/api/core: expected code 0, got %d", resp.Code)
	}

	// /api/device/udid should return UDID, not index.html.
	req = httptest.NewRequest("GET", "/api/device/udid", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Code != 0 {
		t.Fatalf("/api/device/udid: expected code 0, got %d", resp.Code)
	}
	data, _ := resp.Data.(map[string]any)
	if _, ok := data["udid"]; !ok {
		t.Fatal("/api/device/udid: expected udid in data")
	}
}

// failingEngineStarter always returns an error.
func failingEngineStarter(cfg engine.Config) (*engine.Engine, error) {
	return nil, fmt.Errorf("simulated failure")
}

func TestDoUpFromError(t *testing.T) {
	d := New()

	// First attempt: fail to connect.
	d.EngineStarter = failingEngineStarter
	err := d.doUp("k2v5://test:tok@host:443", "", 0)
	if err == nil {
		t.Fatal("expected error from failing builder")
	}

	// State should be stopped with lastError set.
	if d.State() != StateStopped {
		t.Fatalf("expected stopped after failure, got %s", d.State())
	}
	info := d.statusInfo()
	if info["error"] == nil || info["error"] == "" {
		t.Fatal("expected lastError to be set after failure")
	}

	// Second attempt: succeed. Old error should be cleared.
	d.EngineStarter = mockEngineStarter
	err = d.doUp("k2v5://test:tok@host:443", "", 0)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	if d.State() != StateConnected {
		t.Fatalf("expected connected, got %s", d.State())
	}
	info = d.statusInfo()
	if info["error"] != nil {
		t.Fatalf("expected lastError cleared, got %v", info["error"])
	}
}

func TestConcurrentDoUp(t *testing.T) {
	// Track how many engines are created to verify serialization.
	var created atomic.Int32

	trackingStarter := func(cfg engine.Config) (*engine.Engine, error) {
		created.Add(1)
		return engine.New(), nil
	}

	d := New()
	d.EngineStarter = trackingStarter

	// Launch 5 concurrent doUp calls.
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.doUp("k2v5://test:tok@host:443", "", 0)
		}()
	}
	wg.Wait()

	// Final state must be connected.
	if d.State() != StateConnected {
		t.Fatalf("expected connected, got %s", d.State())
	}

	// All 5 calls should have created engines (opMu serializes, each tears down previous).
	if created.Load() != 5 {
		t.Fatalf("expected 5 engines created, got %d", created.Load())
	}

	// Daemon must hold exactly one engine reference.
	d.mu.RLock()
	hasEngine := d.engine != nil
	d.mu.RUnlock()
	if !hasEngine {
		t.Fatal("expected daemon to hold an engine after concurrent doUp")
	}
}

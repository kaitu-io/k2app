package daemon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/engine"
)

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
	if data["state"] != engine.StateDisconnected {
		t.Fatalf("expected state %s, got %v", engine.StateDisconnected, data["state"])
	}
}

// mockStartFunc simulates a successful engine start by forcing the connected state.
// The engine fires OnStatus(connected) to the daemon's EventHandler.
func mockStartFunc(eng *engine.Engine, cfg engine.Config) error {
	eng.ForceConnected()
	return nil
}

func TestHandleCoreUpDown(t *testing.T) {
	d := New()
	d.StartFunc = mockStartFunc

	// Up with config JSON.
	upBody := `{"action":"up","params":{"config":{"routes":[{"via":"k2v5://test:token@host:443","match":{"all":true}}]}}}`
	req := httptest.NewRequest("POST", "/api/core", bytes.NewReader([]byte(upBody)))
	w := httptest.NewRecorder()
	d.handleCore(w, req)

	var upResp Response
	json.NewDecoder(w.Body).Decode(&upResp)
	if upResp.Code != 0 {
		t.Fatalf("up: expected code 0, got %d: %s", upResp.Code, upResp.Message)
	}

	if d.State() != engine.StateConnected {
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

	if d.State() != engine.StateDisconnected {
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

func TestGetConfigReturns400(t *testing.T) {
	d := New()

	body := `{"action":"get_config"}`
	req := httptest.NewRequest("POST", "/api/core", bytes.NewReader([]byte(body)))
	w := httptest.NewRecorder()
	d.handleCore(w, req)

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Code != 400 {
		t.Fatalf("expected code 400, got %d: %s", resp.Code, resp.Message)
	}
}

func TestStatusIncludesConfig(t *testing.T) {
	d := New()
	d.StartFunc = mockStartFunc

	// Connect with config.
	upBody := `{"action":"up","params":{"config":{"routes":[{"via":"k2v5://test:tok@host:443","match":{"all":true}}]}}}`
	req := httptest.NewRequest("POST", "/api/core", bytes.NewReader([]byte(upBody)))
	w := httptest.NewRecorder()
	d.handleCore(w, req)

	var upResp Response
	json.NewDecoder(w.Body).Decode(&upResp)
	if upResp.Code != 0 {
		t.Fatalf("up: expected code 0, got %d: %s", upResp.Code, upResp.Message)
	}

	// Status should include config.
	statusBody := `{"action":"status"}`
	req = httptest.NewRequest("POST", "/api/core", bytes.NewReader([]byte(statusBody)))
	w = httptest.NewRecorder()
	d.handleCore(w, req)

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Code != 0 {
		t.Fatalf("status: expected code 0, got %d", resp.Code)
	}

	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatal("expected data to be a map")
	}

	cfgRaw, ok := data["config"]
	if !ok {
		t.Fatal("expected config field in status response")
	}

	cfg, ok := cfgRaw.(map[string]any)
	if !ok {
		t.Fatalf("expected config to be a map, got %T", cfgRaw)
	}

	// Server is derived (json:"-"), should NOT appear in JSON response.
	// Check routes instead.
	routesRaw, ok := cfg["routes"]
	if !ok {
		t.Fatal("expected routes field in status config")
	}
	routes, ok := routesRaw.([]any)
	if !ok || len(routes) == 0 {
		t.Fatalf("expected non-empty routes array, got %v", routesRaw)
	}
}


func TestHandleLogLevel(t *testing.T) {
	d := New()

	// POST with valid level.
	body := `{"level":"debug"}`
	req := httptest.NewRequest("POST", "/api/log-level", bytes.NewReader([]byte(body)))
	w := httptest.NewRecorder()
	d.handleLogLevel(w, req)

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Code != 0 {
		t.Fatalf("expected code 0, got %d: %s", resp.Code, resp.Message)
	}

	// Verify the level actually changed.
	if config.LogLevel.Level() != -4 { // slog.LevelDebug = -4
		t.Fatalf("expected debug level (-4), got %d", config.LogLevel.Level())
	}

	// Switch to error.
	body = `{"level":"error"}`
	req = httptest.NewRequest("POST", "/api/log-level", bytes.NewReader([]byte(body)))
	w = httptest.NewRecorder()
	d.handleLogLevel(w, req)

	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Code != 0 {
		t.Fatalf("expected code 0, got %d", resp.Code)
	}
	if config.LogLevel.Level() != 8 { // slog.LevelError = 8
		t.Fatalf("expected error level (8), got %d", config.LogLevel.Level())
	}

	// Restore to info for other tests.
	config.SetLogLevel("info")
}

func TestHandleLogLevel_MethodNotAllowed(t *testing.T) {
	d := New()

	req := httptest.NewRequest("GET", "/api/log-level", nil)
	w := httptest.NewRecorder()
	d.handleLogLevel(w, req)

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Code != 405 {
		t.Fatalf("expected code 405, got %d", resp.Code)
	}
}

func TestHandleLogLevel_InvalidJSON(t *testing.T) {
	d := New()

	req := httptest.NewRequest("POST", "/api/log-level", bytes.NewReader([]byte("not-json")))
	w := httptest.NewRecorder()
	d.handleLogLevel(w, req)

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Code != 400 {
		t.Fatalf("expected code 400, got %d", resp.Code)
	}
}

// failingStartFunc always returns an error.
func failingStartFunc(eng *engine.Engine, cfg engine.Config) error {
	return fmt.Errorf("simulated failure")
}

func TestDoUpFromError(t *testing.T) {
	d := New()
	cfg := config.ClientFromURL("k2v5://test:tok@host:443")

	// First attempt: fail to connect.
	d.StartFunc = failingStartFunc
	err := d.doUp(cfg, 0)
	if err == nil {
		t.Fatal("expected error from failing builder")
	}

	// State should be stopped with lastError set.
	if d.State() != engine.StateDisconnected {
		t.Fatalf("expected stopped after failure, got %s", d.State())
	}
	info := d.statusInfo()
	if info["error"] == nil || info["error"] == "" {
		t.Fatal("expected lastError to be set after failure")
	}

	// Second attempt: succeed. Old error should be cleared.
	d.StartFunc = mockStartFunc
	err = d.doUp(cfg, 0)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	if d.State() != engine.StateConnected {
		t.Fatalf("expected connected, got %s", d.State())
	}
	info = d.statusInfo()
	if info["error"] != nil {
		t.Fatalf("expected lastError cleared, got %v", info["error"])
	}
}

func TestHandleCore_RelayFetch(t *testing.T) {
	d := &Daemon{} // handleRelay must not touch engine state
	body := `{"action":"relay-fetch","params":{"method":"GET"}}` // no ip/centerHost → error envelope
	req := httptest.NewRequest("POST", "/api/core", bytes.NewReader([]byte(body)))
	w := httptest.NewRecorder()
	d.handleCore(w, req)

	var resp Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Code != 502 {
		t.Fatalf("relay-fetch with missing ECH should route to handleRelay and yield wire code 502, got %d (msg=%q)", resp.Code, resp.Message)
	}
}

func TestConcurrentDoUp(t *testing.T) {
	// Track how many engines are created to verify serialization.
	var created atomic.Int32

	trackingStarter := func(eng *engine.Engine, cfg engine.Config) error {
		created.Add(1)
		eng.ForceConnected()
		return nil
	}

	d := New()
	d.StartFunc = trackingStarter
	clientCfg := config.ClientFromURL("k2v5://test:tok@host:443")

	// Launch 5 concurrent doUp calls.
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.doUp(clientCfg, 0)
		}()
	}
	wg.Wait()

	// Final state must be connected.
	if d.State() != engine.StateConnected {
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

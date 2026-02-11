package daemon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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

func TestHandleCoreUpDown(t *testing.T) {
	d := New()

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

func TestSpeedtestLifecycle(t *testing.T) {
	d := New()

	// Start speedtest.
	body := `{"action":"speedtest","params":{}}`
	req := httptest.NewRequest("POST", "/api/core", bytes.NewReader([]byte(body)))
	w := httptest.NewRecorder()
	d.handleCore(w, req)

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Code != 0 {
		t.Fatalf("expected code 0, got %d: %s", resp.Code, resp.Message)
	}

	// Get status (may be running or completed since it's a stub).
	statusBody := `{"action":"get_speedtest_status"}`
	req = httptest.NewRequest("POST", "/api/core", bytes.NewReader([]byte(statusBody)))
	w = httptest.NewRecorder()
	d.handleCore(w, req)

	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Code != 0 {
		t.Fatalf("expected code 0, got %d", resp.Code)
	}
}

func TestHandleCoreReconnectNoConfig(t *testing.T) {
	d := New()
	body := `{"action":"reconnect"}`
	req := httptest.NewRequest("POST", "/api/core", bytes.NewReader([]byte(body)))
	w := httptest.NewRecorder()
	d.handleCore(w, req)

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Code != 510 {
		t.Fatalf("expected code 510, got %d: %s", resp.Code, resp.Message)
	}
}

func TestHandleCoreGetConfig(t *testing.T) {
	d := New()

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

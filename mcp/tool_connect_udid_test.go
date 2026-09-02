package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// When the session was restored from the desktop app, connect must build the
// k2v5 URL with the desktop's 32-char hashed UDID: the token was issued to that
// device, and node auth (api/slave_api_device_auth.go) rejects any other UDID
// with "UDID mismatch". Before the fix, connect used MCP's own 16-char id.
func TestToolConnect_UsesSharedDesktopUDID(t *testing.T) {
	const desktopUDID = "932a7cc1a75b5830a1dd59f057b608d3"
	tunnels := []tunnelEntry{
		{ID: 7, Name: "Tokyo 1", Domain: "jp1.example.com", ServerURL: "k2v5://jp1.example.com", Node: tunnelNode{Country: "JP"}},
	}
	centerSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := json.Marshal(tunnelListResponse{Items: tunnels})
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(centerResponse{Code: 0, Message: "ok", Data: json.RawMessage(data)})
	}))
	defer centerSrv.Close()

	var upServerURL string
	daemonSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ping" {
			w.WriteHeader(http.StatusOK)
			return
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if params, ok := body["params"].(map[string]any); ok {
			if cfg, ok := params["config"].(map[string]any); ok {
				if routes, ok := cfg["routes"].([]any); ok && len(routes) > 0 {
					if route, ok := routes[0].(map[string]any); ok {
						upServerURL, _ = route["via"].(string)
					}
				}
			}
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer daemonSrv.Close()

	app := newTestApp(t, centerSrv.URL)
	app.daemon = &DaemonClient{Addr: daemonSrv.URL}
	app.udid = desktopUDID // what main() resolves when Tauri storage carries a UDID
	app.session.SetTokens("tok", "ref", "", time.Now())
	app.center.SetToken("tok")

	result, _, err := app.toolConnect(context.Background(), nil, ConnectInput{ServerID: 7})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got: %s", textContent(t, result))
	}
	want := desktopUDID + ":tok@jp1.example.com"
	if !strings.Contains(upServerURL, want) {
		t.Fatalf("connect must use the shared desktop UDID: want %q in %q", want, upServerURL)
	}
	if strings.Contains(upServerURL, app.session.UDID()+":") {
		t.Fatalf("connect used MCP's own 16-char UDID instead of the desktop's: %q", upServerURL)
	}
}

// Without a desktop UDID, connect keeps using MCP's own device id (standalone login).
func TestToolConnect_FallsBackToOwnUDID(t *testing.T) {
	app := newTestApp(t, "http://127.0.0.1:1")
	if got, want := app.deviceUDID(), app.session.UDID(); got != want {
		t.Fatalf("deviceUDID() = %q, want session UDID %q", got, want)
	}
}

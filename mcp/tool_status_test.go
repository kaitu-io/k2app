package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestToolStatus_Connected(t *testing.T) {
	daemonSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/core" && r.Method == http.MethodPost {
			status := DaemonStatus{
				State:         "connected",
				ConnectedAt:   time.Now().Add(-120 * time.Second),
				UptimeSeconds: 120,
				Config:        &DaemonConfig{Server: "k2v5://jp1.example.com"},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(status)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer daemonSrv.Close()

	app := newTestApp(t, "http://127.0.0.1:0")
	app.daemon = &DaemonClient{Addr: daemonSrv.URL}

	// Pre-populate server cache.
	app.serversMu.Lock()
	app.servers = []Server{
		{ID: 1, Name: "Tokyo 1", Domain: "jp1.example.com", ServerURL: "k2v5://jp1.example.com"},
	}
	app.serversCachedAt = time.Now()
	app.serversMu.Unlock()

	result, _, err := app.toolStatus(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", textContent(t, result))
	}

	var out map[string]any
	if err := json.Unmarshal([]byte(textContent(t, result)), &out); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if out["state"] != "connected" {
		t.Errorf("expected state='connected', got %v", out["state"])
	}
	if out["server"] != "Tokyo 1" {
		t.Errorf("expected server='Tokyo 1', got %v", out["server"])
	}
	if out["uptime_seconds"] == nil {
		t.Error("expected uptime_seconds field")
	}
}

func TestToolStatus_Disconnected(t *testing.T) {
	daemonSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/core" && r.Method == http.MethodPost {
			status := DaemonStatus{State: "disconnected"}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(status)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer daemonSrv.Close()

	app := newTestApp(t, "http://127.0.0.1:0")
	app.daemon = &DaemonClient{Addr: daemonSrv.URL}

	result, _, err := app.toolStatus(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", textContent(t, result))
	}

	var out map[string]any
	if err := json.Unmarshal([]byte(textContent(t, result)), &out); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if out["state"] != "disconnected" {
		t.Errorf("expected state='disconnected', got %v", out["state"])
	}
}

func TestToolStatus_WithError(t *testing.T) {
	daemonSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/core" && r.Method == http.MethodPost {
			status := DaemonStatus{
				State: "error",
				Error: &DaemonStatusError{Code: 503, Message: "TLS handshake failed"},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(status)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer daemonSrv.Close()

	app := newTestApp(t, "http://127.0.0.1:0")
	app.daemon = &DaemonClient{Addr: daemonSrv.URL}

	result, _, err := app.toolStatus(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", textContent(t, result))
	}

	var out map[string]any
	if err := json.Unmarshal([]byte(textContent(t, result)), &out); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if out["state"] != "error" {
		t.Errorf("expected state='error', got %v", out["state"])
	}
	if out["error"] != "TLS handshake failed" {
		t.Errorf("expected error='TLS handshake failed', got %v", out["error"])
	}
	if out["error_code"] != float64(503) {
		t.Errorf("expected error_code=503, got %v", out["error_code"])
	}
}

func TestToolStatus_DaemonNotRunning(t *testing.T) {
	app := newTestApp(t, "http://127.0.0.1:0")
	// Use unreachable daemon.
	app.daemon = &DaemonClient{Addr: "http://127.0.0.1:1"}

	result, _, err := app.toolStatus(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success even with unreachable daemon, got error: %s", textContent(t, result))
	}

	var out map[string]any
	if err := json.Unmarshal([]byte(textContent(t, result)), &out); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if out["state"] != "disconnected" {
		t.Errorf("expected state='disconnected', got %v", out["state"])
	}
}

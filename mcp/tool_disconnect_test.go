package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestToolDisconnect_Success(t *testing.T) {
	var downCalled bool
	daemonSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/core" && r.Method == http.MethodPost {
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if body["action"] == "down" {
				downCalled = true
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer daemonSrv.Close()

	app := newTestApp(t, "http://127.0.0.1:0")
	app.daemon = &DaemonClient{Addr: daemonSrv.URL}

	result, _, err := app.toolDisconnect(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", textContent(t, result))
	}

	var out map[string]string
	if err := json.Unmarshal([]byte(textContent(t, result)), &out); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if out["status"] != "disconnected" {
		t.Errorf("expected status='disconnected', got %q", out["status"])
	}
	if !downCalled {
		t.Error("expected daemon Down to be called")
	}
}

func TestToolDisconnect_DaemonNotRunning(t *testing.T) {
	app := newTestApp(t, "http://127.0.0.1:0")
	// Use unreachable daemon.
	app.daemon = &DaemonClient{Addr: "http://127.0.0.1:1"}

	result, _, err := app.toolDisconnect(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should still succeed even if daemon is unreachable.
	if result.IsError {
		t.Fatalf("expected success even with unreachable daemon, got error: %s", textContent(t, result))
	}

	var out map[string]string
	if err := json.Unmarshal([]byte(textContent(t, result)), &out); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if out["status"] != "disconnected" {
		t.Errorf("expected status='disconnected', got %q", out["status"])
	}
}

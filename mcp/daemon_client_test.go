package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDaemonClient_Ping_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ping" {
			t.Errorf("expected /ping, got %s", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := &DaemonClient{Addr: srv.URL}
	if err := c.Ping(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDaemonClient_Ping_Unreachable(t *testing.T) {
	c := &DaemonClient{Addr: "http://127.0.0.1:19999"}
	if err := c.Ping(); err == nil {
		t.Fatal("expected error for unreachable addr, got nil")
	}
}

func TestDaemonClient_Up(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/core" {
			t.Errorf("expected /api/core, got %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("failed to decode body: %v", err)
		}
		if body["action"] != "up" {
			t.Errorf("expected action 'up', got %v", body["action"])
		}
		params, ok := body["params"].(map[string]any)
		if !ok || params == nil {
			t.Error("expected params field to be present")
		} else if params["config"] == nil {
			t.Error("expected params.config field to be present")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := &DaemonClient{Addr: srv.URL}
	if err := c.Up("k2v5://server.example.com"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDaemonClient_Down(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/core" {
			t.Errorf("expected /api/core, got %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("failed to decode body: %v", err)
		}
		if body["action"] != "down" {
			t.Errorf("expected action 'down', got %v", body["action"])
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := &DaemonClient{Addr: srv.URL}
	if err := c.Down(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDaemonClient_Status(t *testing.T) {
	connectedAt := time.Now().UTC().Truncate(time.Second)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/core" {
			t.Errorf("expected /api/core, got %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("failed to decode body: %v", err)
		}
		if body["action"] != "status" {
			t.Errorf("expected action 'status', got %v", body["action"])
		}
		status := DaemonStatus{
			State:         "connected",
			ConnectedAt:   connectedAt,
			UptimeSeconds: 42,
			Config:        &DaemonConfig{Server: "k2v5://server.example.com"},
		}
		data, _ := json.Marshal(status)
		envelope := daemonEnvelope{Code: 0, Message: "ok", Data: json.RawMessage(data)}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(envelope)
	}))
	defer srv.Close()

	c := &DaemonClient{Addr: srv.URL}
	status, err := c.Status()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.State != "connected" {
		t.Errorf("expected state 'connected', got '%s'", status.State)
	}
	if status.UptimeSeconds != 42 {
		t.Errorf("expected uptime 42, got %d", status.UptimeSeconds)
	}
	if status.Config == nil {
		t.Fatal("expected config to be non-nil")
	}
	if status.Config.Server != "k2v5://server.example.com" {
		t.Errorf("expected server 'k2v5://server.example.com', got '%s'", status.Config.Server)
	}
}

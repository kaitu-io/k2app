package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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
			t.Fatal("expected params field to be present")
		}
		cfg, ok := params["config"].(map[string]any)
		if !ok || cfg == nil {
			t.Fatal("expected params.config field to be present")
		}

		// The outbound must arrive as routes[].via. ClientConfig.Server is
		// tagged json:"-" — a top-level "server" key is silently dropped by
		// the daemon's Unmarshal, leaving routes empty, and the engine then
		// fails 570 "no k2v5 outbound configured" long after the HTTP call
		// already returned success. Asserting only that "config" exists (what
		// this test used to do) is what let that ship.
		if _, bad := cfg["server"]; bad {
			t.Error("config carries a top-level \"server\" key; ClientConfig has no such field (json:\"-\")")
		}
		routes, ok := cfg["routes"].([]any)
		if !ok || len(routes) == 0 {
			t.Fatalf("expected config.routes to be a non-empty array, got %#v", cfg["routes"])
		}
		route, ok := routes[0].(map[string]any)
		if !ok {
			t.Fatalf("expected routes[0] to be an object, got %#v", routes[0])
		}
		if route["via"] != "k2v5://server.example.com" {
			t.Errorf("routes[0].via = %v, want the server URL", route["via"])
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

// daemonStatusPayload is a byte-for-byte copy of what k2's daemon actually
// answers on action=status: statusInfo() hand-builds this map, and
// k2/daemon/sse.go documents the same shape in its payload example.
//
// It is a raw literal on purpose. The previous version of this test built a
// DaemonStatus, marshalled it, and parsed it back — a round-trip through our
// own struct, which is self-consistent under ANY field names and therefore
// could never detect that we had drifted from the daemon. It did not: the
// struct asked for `connected_at` / `uptime_seconds` / `server` while the
// daemon sent `startAt` / `uptimeSeconds` / `routes`, and the test stayed
// green through all of it.
const daemonStatusPayload = `{
  "state": "connected",
  "startAt": 1755300000,
  "uptimeSeconds": 42,
  "config": {
    "mode": "tun",
    "routes": [
      {"via": "direct", "match": {"preset": "cn-access"}},
      {"via": "k2v5://server.example.com"}
    ]
  }
}`

func TestDaemonClient_Status(t *testing.T) {
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
		envelope := daemonEnvelope{Code: 0, Message: "ok", Data: json.RawMessage(daemonStatusPayload)}
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
	if status.StartAt != 1755300000 {
		t.Errorf("expected startAt 1755300000, got %d", status.StartAt)
	}
	if status.Config == nil {
		t.Fatal("expected config to be non-nil")
	}
	// "direct" comes first in the route table and is not an outbound.
	if got := status.Config.Server(); got != "k2v5://server.example.com" {
		t.Errorf("expected server 'k2v5://server.example.com', got '%s'", got)
	}
}

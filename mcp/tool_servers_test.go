package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestToolListServers_Success(t *testing.T) {
	tunnels := []tunnelEntry{
		{
			ID:        1,
			Name:      "Tokyo 1",
			Domain:    "jp1.example.com",
			ServerURL: "k2v5://jp1.example.com",
			Node:      tunnelNode{Name: "node-jp1", Country: "JP", Region: "Asia", TrafficUsagePercent: 42.5, BandwidthUsagePercent: 30.0},
		},
		{
			ID:        2,
			Name:      "",
			Domain:    "us1.example.com",
			ServerURL: "k2v5://us1.example.com",
			Node:      tunnelNode{Name: "node-us1", Country: "US", Region: "Americas"},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tunnels/k2v5" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		data, _ := json.Marshal(tunnelListResponse{Items: tunnels})
		resp := centerResponse{Code: 0, Message: "ok", Data: json.RawMessage(data)}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	app := newTestApp(t, srv.URL)
	app.session.SetTokens("tok", "ref", "user@example.com", time.Now())
	app.center.SetToken("tok")

	result, _, err := app.toolListServers(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", textContent(t, result))
	}

	var out struct {
		Servers []serverOutput `json:"servers"`
	}
	if err := json.Unmarshal([]byte(textContent(t, result)), &out); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if len(out.Servers) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(out.Servers))
	}

	s0 := out.Servers[0]
	if s0.ID != 1 {
		t.Errorf("expected ID=1, got %d", s0.ID)
	}
	if s0.Name != "Tokyo 1" {
		t.Errorf("expected Name='Tokyo 1', got %q", s0.Name)
	}
	if s0.Country != "JP" {
		t.Errorf("expected Country='JP', got %q", s0.Country)
	}
	if s0.Load != "medium" {
		t.Errorf("expected Load='medium' (42.5%% traffic), got %q", s0.Load)
	}

	// Second server: name falls back to node.name, load is low (0%).
	s1 := out.Servers[1]
	if s1.Name != "node-us1" {
		t.Errorf("expected Name='node-us1' (fallback), got %q", s1.Name)
	}
	if s1.Load != "low" {
		t.Errorf("expected Load='low', got %q", s1.Load)
	}
}

func TestToolListServers_CacheHit(t *testing.T) {
	var callCount int64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&callCount, 1)
		data, _ := json.Marshal(tunnelListResponse{Items: []tunnelEntry{
			{ID: 1, Name: "Server 1", Domain: "s1.example.com", ServerURL: "k2v5://s1.example.com"},
		}})
		resp := centerResponse{Code: 0, Message: "ok", Data: json.RawMessage(data)}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	app := newTestApp(t, srv.URL)
	app.session.SetTokens("tok", "ref", "user@example.com", time.Now())
	app.center.SetToken("tok")

	// First call — should hit API.
	result1, _, err := app.toolListServers(context.Background(), nil, nil)
	if err != nil || result1.IsError {
		t.Fatalf("first call failed")
	}

	// Second call — should use cache.
	result2, _, err := app.toolListServers(context.Background(), nil, nil)
	if err != nil || result2.IsError {
		t.Fatalf("second call failed")
	}

	if atomic.LoadInt64(&callCount) != 1 {
		t.Errorf("expected exactly 1 API call, got %d", callCount)
	}
}

func TestToolListServers_NotLoggedIn(t *testing.T) {
	app := newTestApp(t, "http://127.0.0.1:0")

	result, _, err := app.toolListServers(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true when not logged in")
	}
}

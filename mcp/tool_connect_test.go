package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestToolConnect_Success(t *testing.T) {
	tunnels := []tunnelEntry{
		{
			ID:        42,
			Name:      "Tokyo 1",
			Domain:    "jp1.example.com",
			ServerURL: "k2v5://jp1.example.com",
			Node:      tunnelNode{Country: "JP"},
		},
	}

	centerSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := json.Marshal(tunnelListResponse{Items: tunnels})
		resp := centerResponse{Code: 0, Message: "ok", Data: json.RawMessage(data)}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer centerSrv.Close()

	var upCalled bool
	var upServerURL string
	daemonSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ping" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path == "/api/core" && r.Method == http.MethodPost {
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if body["action"] == "up" {
				upCalled = true
				if params, ok := body["params"].(map[string]any); ok {
					if cfg, ok := params["config"].(map[string]any); ok {
						upServerURL, _ = cfg["server"].(string)
					}
				}
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer daemonSrv.Close()

	app := newTestApp(t, centerSrv.URL)
	app.daemon = &DaemonClient{Addr: daemonSrv.URL}
	app.session.SetTokens("tok", "ref", "user@example.com", time.Now())
	app.center.SetToken("tok")

	result, _, err := app.toolConnect(context.Background(), nil, ConnectInput{ServerID: 42})
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
	if out["state"] != "connecting" {
		t.Errorf("expected state='connecting', got %q", out["state"])
	}
	if out["server"] != "Tokyo 1" {
		t.Errorf("expected server='Tokyo 1', got %q", out["server"])
	}
	if !upCalled {
		t.Error("expected daemon Up to be called")
	}
	if upServerURL != "k2v5://jp1.example.com" {
		t.Errorf("expected server URL 'k2v5://jp1.example.com', got %q", upServerURL)
	}
}

func TestToolConnect_ServerNotFound(t *testing.T) {
	centerSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return empty server list.
		resp := centerResponse{Code: 0, Message: "ok", Data: json.RawMessage(`{"items":[]}`)}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer centerSrv.Close()

	app := newTestApp(t, centerSrv.URL)
	app.session.SetTokens("tok", "ref", "user@example.com", time.Now())
	app.center.SetToken("tok")

	result, _, err := app.toolConnect(context.Background(), nil, ConnectInput{ServerID: 99})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true for unknown server")
	}
}

func TestToolConnect_DaemonNotRunning(t *testing.T) {
	tunnels := []tunnelEntry{
		{ID: 1, Name: "Server 1", Domain: "s1.example.com", ServerURL: "k2v5://s1.example.com"},
	}

	centerSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := json.Marshal(tunnelListResponse{Items: tunnels})
		resp := centerResponse{Code: 0, Message: "ok", Data: json.RawMessage(data)}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer centerSrv.Close()

	app := newTestApp(t, centerSrv.URL)
	// Use an unreachable daemon address.
	app.daemon = &DaemonClient{Addr: "http://127.0.0.1:1"}
	app.session.SetTokens("tok", "ref", "user@example.com", time.Now())
	app.center.SetToken("tok")

	result, _, err := app.toolConnect(context.Background(), nil, ConnectInput{ServerID: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true when daemon not running")
	}
}

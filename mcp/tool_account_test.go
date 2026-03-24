package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestToolAccountInfo_Success(t *testing.T) {
	expiry := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/user" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		data, _ := json.Marshal(userResponse{
			LoginIdentifies: []struct {
				Type  string `json:"type"`
				Value string `json:"value"`
			}{
				{Type: "email", Value: "alice@example.com"},
			},
			ExpiredAt:   &expiry,
			IsActive:    true,
			DeviceCount: 2,
			InviteCode:  "ALICE2024",
		})
		resp := centerResponse{Code: 0, Message: "ok", Data: json.RawMessage(data)}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	app := newTestApp(t, srv.URL)
	// Simulate logged-in state.
	app.session.SetTokens("access-token", "refresh-token", "alice@example.com", time.Now())
	app.center.SetToken("access-token")

	result, _, err := app.toolAccountInfo(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", textContent(t, result))
	}

	var out accountInfoOutput
	if err := json.Unmarshal([]byte(textContent(t, result)), &out); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if out.Email != "alice@example.com" {
		t.Errorf("expected email 'alice@example.com', got %q", out.Email)
	}
	if out.ExpiredAt != "2026-12-31T00:00:00Z" {
		t.Errorf("expected expired_at '2026-12-31T00:00:00Z', got %q", out.ExpiredAt)
	}
	if !out.IsActive {
		t.Error("expected is_active=true")
	}
	if out.DeviceCount != 2 {
		t.Errorf("expected device_count=2, got %d", out.DeviceCount)
	}
	if out.DeviceLimit != 5 {
		t.Errorf("expected device_limit=5, got %d", out.DeviceLimit)
	}
	if out.InviteCode != "ALICE2024" {
		t.Errorf("expected invite_code='ALICE2024', got %q", out.InviteCode)
	}
}

func TestToolAccountInfo_NotLoggedIn(t *testing.T) {
	app := newTestApp(t, "http://127.0.0.1:0")

	result, _, err := app.toolAccountInfo(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true when not logged in")
	}

	var out map[string]string
	if err := json.Unmarshal([]byte(textContent(t, result)), &out); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if out["error"] == "" {
		t.Error("expected non-empty error message")
	}
}

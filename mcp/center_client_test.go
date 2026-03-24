package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestCenterClient_Get_Success(t *testing.T) {
	type result struct {
		Name string `json:"name"`
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("expected Authorization: Bearer test-token, got %s", r.Header.Get("Authorization"))
		}
		if r.Header.Get("X-UDID") != "test-udid" {
			t.Errorf("expected X-UDID: test-udid, got %s", r.Header.Get("X-UDID"))
		}
		data, _ := json.Marshal(result{Name: "kaitu"})
		resp := centerResponse{Code: 0, Message: "ok", Data: json.RawMessage(data)}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := NewCenterClient(srv.URL)
	c.SetToken("test-token")
	c.SetUDID("test-udid")

	var res result
	if err := c.Get("/test", &res); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Name != "kaitu" {
		t.Errorf("expected name 'kaitu', got '%s'", res.Name)
	}
}

func TestCenterClient_Get_AuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := centerResponse{Code: 401, Message: "unauthorized", Data: json.RawMessage("null")}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := NewCenterClient(srv.URL)

	var res struct{}
	err := c.Get("/test", &res)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	ce, ok := err.(*CenterError)
	if !ok {
		t.Fatalf("expected *CenterError, got %T: %v", err, err)
	}
	if ce.Code != 401 {
		t.Errorf("expected code 401, got %d", ce.Code)
	}
}

func TestCenterClient_Post_WithBody(t *testing.T) {
	type reqBody struct {
		Action string `json:"action"`
	}
	type result struct {
		OK bool `json:"ok"`
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", r.Header.Get("Content-Type"))
		}
		var body reqBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("failed to decode body: %v", err)
		}
		if body.Action != "login" {
			t.Errorf("expected action 'login', got '%s'", body.Action)
		}
		data, _ := json.Marshal(result{OK: true})
		resp := centerResponse{Code: 0, Message: "ok", Data: json.RawMessage(data)}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := NewCenterClient(srv.URL)

	var res result
	if err := c.Post("/test", reqBody{Action: "login"}, &res); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.OK {
		t.Error("expected ok=true")
	}
}

func TestCenterClient_AutoRefresh_Get(t *testing.T) {
	type userResult struct {
		ID int `json:"id"`
	}

	var userCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/auth/refresh":
			// Verify the refresh token was sent.
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode refresh body: %v", err)
			}
			if body["refreshToken"] != "old-refresh" {
				t.Errorf("expected refreshToken 'old-refresh', got %q", body["refreshToken"])
			}
			data, _ := json.Marshal(refreshResponse{
				AccessToken:  "new-access",
				RefreshToken: "new-refresh",
			})
			json.NewEncoder(w).Encode(centerResponse{Code: 0, Data: json.RawMessage(data)})

		case "/api/user":
			count := userCalls.Add(1)
			if count == 1 {
				// First call: return 401 to trigger refresh.
				json.NewEncoder(w).Encode(centerResponse{Code: 401, Message: "unauthorized"})
			} else {
				// Second call after refresh: verify new token, return success.
				if r.Header.Get("Authorization") != "Bearer new-access" {
					t.Errorf("expected new-access token on retry, got %q", r.Header.Get("Authorization"))
				}
				data, _ := json.Marshal(userResult{ID: 42})
				json.NewEncoder(w).Encode(centerResponse{Code: 0, Data: json.RawMessage(data)})
			}

		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	sess := NewSession(dir)
	sess.mu.Lock()
	sess.AccessToken = "old-access"
	sess.RefreshToken = "old-refresh"
	sess.mu.Unlock()

	c := NewCenterClient(srv.URL)
	c.SetToken("old-access")
	c.SetRefreshSource(sess)

	var res userResult
	if err := c.Get("/api/user", &res); err != nil {
		t.Fatalf("unexpected error after auto-refresh: %v", err)
	}
	if res.ID != 42 {
		t.Errorf("expected ID 42, got %d", res.ID)
	}
	if userCalls.Load() != 2 {
		t.Errorf("expected 2 calls to /api/user, got %d", userCalls.Load())
	}

	// Verify in-memory token updated.
	if c.Token() != "new-access" {
		t.Errorf("expected center token 'new-access', got %q", c.Token())
	}

	// Verify session persisted new tokens.
	sess.mu.RLock()
	gotAccess := sess.AccessToken
	gotRefresh := sess.RefreshToken
	sess.mu.RUnlock()

	if gotAccess != "new-access" {
		t.Errorf("expected session AccessToken 'new-access', got %q", gotAccess)
	}
	if gotRefresh != "new-refresh" {
		t.Errorf("expected session RefreshToken 'new-refresh', got %q", gotRefresh)
	}
}

func TestCenterClient_AutoRefresh_Post(t *testing.T) {
	type orderResult struct {
		OrderID string `json:"orderId"`
	}

	var orderCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/auth/refresh":
			data, _ := json.Marshal(refreshResponse{
				AccessToken:  "new-access",
				RefreshToken: "new-refresh",
			})
			json.NewEncoder(w).Encode(centerResponse{Code: 0, Data: json.RawMessage(data)})

		case "/api/user/orders":
			count := orderCalls.Add(1)

			// Always decode the body to verify it is replayed correctly.
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode order body call %d: %v", count, err)
			}
			if body["plan"] != "pro-monthly" {
				t.Errorf("expected plan 'pro-monthly' on call %d, got %v", count, body["plan"])
			}

			if count == 1 {
				json.NewEncoder(w).Encode(centerResponse{Code: 401, Message: "unauthorized"})
			} else {
				if r.Header.Get("Authorization") != "Bearer new-access" {
					t.Errorf("expected new-access token on retry, got %q", r.Header.Get("Authorization"))
				}
				data, _ := json.Marshal(orderResult{OrderID: "ord-123"})
				json.NewEncoder(w).Encode(centerResponse{Code: 0, Data: json.RawMessage(data)})
			}

		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	sess := NewSession(dir)
	sess.mu.Lock()
	sess.AccessToken = "old-access"
	sess.RefreshToken = "old-refresh"
	sess.mu.Unlock()

	c := NewCenterClient(srv.URL)
	c.SetToken("old-access")
	c.SetRefreshSource(sess)

	body := map[string]any{"plan": "pro-monthly", "forMyself": true}
	var res orderResult
	if err := c.Post("/api/user/orders", body, &res); err != nil {
		t.Fatalf("unexpected error after auto-refresh: %v", err)
	}
	if res.OrderID != "ord-123" {
		t.Errorf("expected OrderID 'ord-123', got %q", res.OrderID)
	}
	if orderCalls.Load() != 2 {
		t.Errorf("expected 2 calls to /api/user/orders, got %d", orderCalls.Load())
	}

	// Verify token updated in both places.
	if c.Token() != "new-access" {
		t.Errorf("expected center token 'new-access', got %q", c.Token())
	}

	sess.mu.RLock()
	gotAccess := sess.AccessToken
	sess.mu.RUnlock()
	if gotAccess != "new-access" {
		t.Errorf("expected session AccessToken 'new-access', got %q", gotAccess)
	}
}

func TestCenterClient_AutoRefresh_NoRefreshSource(t *testing.T) {
	// Without a refresh source, a 401 should propagate as-is.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(centerResponse{Code: 401, Message: "unauthorized"})
	}))
	defer srv.Close()

	c := NewCenterClient(srv.URL)
	// No SetRefreshSource call.

	err := c.Get("/api/user", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var ce *CenterError
	if !errors.As(err, &ce) || ce.Code != 401 {
		t.Errorf("expected 401 CenterError, got %v", err)
	}
}
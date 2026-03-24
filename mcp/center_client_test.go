package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

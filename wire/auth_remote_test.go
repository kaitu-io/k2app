package wire

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewRemoteValidator_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			UDID  string `json:"udid"`
			Token string `json:"token"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if req.UDID == "dev1" && req.Token == "tok1" {
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{}})
		} else {
			json.NewEncoder(w).Encode(map[string]any{"code": 401})
		}
	}))
	defer srv.Close()

	v := NewRemoteValidator(srv.URL)
	if !v("tok1", "dev1") {
		t.Error("valid credentials should pass")
	}
}

func TestNewRemoteValidator_Rejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"code": 401})
	}))
	defer srv.Close()

	v := NewRemoteValidator(srv.URL)
	if v("bad", "dev1") {
		t.Error("rejected credentials should fail")
	}
}

func TestNewRemoteValidator_Expired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"code": 402})
	}))
	defer srv.Close()

	v := NewRemoteValidator(srv.URL)
	if v("tok1", "dev1") {
		t.Error("expired membership should fail")
	}
}

func TestNewRemoteValidator_Unreachable(t *testing.T) {
	v := NewRemoteValidator("http://127.0.0.1:1")
	if v("tok1", "dev1") {
		t.Error("unreachable server should fail")
	}
}

func TestNewRemoteValidator_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(6 * time.Second)
	}))
	defer srv.Close()

	v := NewRemoteValidator(srv.URL)
	if v("tok1", "dev1") {
		t.Error("timeout should fail")
	}
}

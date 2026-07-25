package wire

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const (
	testNodeIPv4   = "203.0.113.7"
	testNodeSecret = "node-secret-xyz"
)

func TestNewRemoteValidator_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// /slave/device-check-auth is behind SlaveAuthRequired() (BasicAuth:
		// ipv4 as username, NodeSecret as password). Without this header Center
		// returns 401 and every device auth silently fails — the second half of
		// the P0. Assert the node identity travels on the request.
		u, p, ok := r.BasicAuth()
		if !ok || u != testNodeIPv4 || p != testNodeSecret {
			t.Errorf("missing/wrong Basic auth: ok=%v user=%q pass=%q", ok, u, p)
		}
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

	v := NewRemoteValidator(srv.URL, func() string { return testNodeIPv4 }, testNodeSecret)
	// v(jwt, udid, mode) — jwt=token, udid=device
	if !v("tok1", "dev1", "") {
		t.Error("valid credentials should pass")
	}
}

func TestNewRemoteValidator_Rejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"code": 401})
	}))
	defer srv.Close()

	v := NewRemoteValidator(srv.URL, func() string { return testNodeIPv4 }, testNodeSecret)
	if v("bad", "dev1", "") {
		t.Error("rejected credentials should fail")
	}
}

func TestNewRemoteValidator_Expired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"code": 402})
	}))
	defer srv.Close()

	v := NewRemoteValidator(srv.URL, func() string { return testNodeIPv4 }, testNodeSecret)
	if v("tok1", "dev1", "") {
		t.Error("expired membership should fail")
	}
}

func TestNewRemoteValidator_Unreachable(t *testing.T) {
	v := NewRemoteValidator("http://127.0.0.1:1", func() string { return testNodeIPv4 }, testNodeSecret)
	if v("tok1", "dev1", "") {
		t.Error("unreachable server should fail")
	}
}

func TestNewRemoteValidator_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(6 * time.Second)
	}))
	defer srv.Close()

	v := NewRemoteValidator(srv.URL, func() string { return testNodeIPv4 }, testNodeSecret)
	if v("tok1", "dev1", "") {
		t.Error("timeout should fail")
	}
}

// TestNewRemoteValidator_EmptySecret pins fail-closed behavior: with no node
// secret the validator must reject WITHOUT hitting Center — an unauthenticated
// request would 401 anyway, and firing it would leak a per-connection request
// storm at Center for zero benefit.
func TestNewRemoteValidator_EmptySecret(t *testing.T) {
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		json.NewEncoder(w).Encode(map[string]any{"code": 0})
	}))
	defer srv.Close()

	v := NewRemoteValidator(srv.URL, func() string { return testNodeIPv4 }, "")
	if v("tok1", "dev1", "") {
		t.Error("empty node secret must reject")
	}
	if hit {
		t.Error("empty node secret must not send any request to Center")
	}
}

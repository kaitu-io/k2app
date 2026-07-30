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
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"userID": 42, "udid": "dev1", "serviceExpiredAt": 1999999999}})
		} else {
			json.NewEncoder(w).Encode(map[string]any{"code": 401})
		}
	}))
	defer srv.Close()

	v := NewRemoteValidator(srv.URL, func() string { return testNodeIPv4 }, testNodeSecret)
	// v(token, udid, mode)
	got := v("tok1", "dev1", "")
	if !got.OK || got.UserID != 42 || got.SvcExp != 1999999999 {
		t.Errorf("valid credentials should pass with userID+svcExp, got %+v", got)
	}
}

func TestNewRemoteValidator_Rejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"code": 401})
	}))
	defer srv.Close()

	v := NewRemoteValidator(srv.URL, func() string { return testNodeIPv4 }, testNodeSecret)
	got := v("bad", "dev1", "")
	if got.Code != 401 {
		t.Errorf("rejected credentials should carry code 401, got %+v", got)
	}
}

func TestNewRemoteValidator_Expired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"code": 402})
	}))
	defer srv.Close()

	v := NewRemoteValidator(srv.URL, func() string { return testNodeIPv4 }, testNodeSecret)
	got := v("tok1", "dev1", "")
	if got.Code != 402 {
		t.Errorf("expired membership should carry code 402, got %+v", got)
	}
}

// TestNewRemoteValidator_Unreachable pins §6.4: a transport error is
// "cannot determine" (-1), NOT an explicit rejection — the chain tail must
// still be able to distinguish "Center said no" from "Center didn't answer".
func TestNewRemoteValidator_Unreachable(t *testing.T) {
	v := NewRemoteValidator("http://127.0.0.1:1", func() string { return testNodeIPv4 }, testNodeSecret)
	got := v("tok1", "dev1", "")
	if got.OK || got.Code != -1 {
		t.Errorf("unreachable server should be undetermined (-1), got %+v", got)
	}
}

func TestNewRemoteValidator_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(6 * time.Second)
	}))
	defer srv.Close()

	v := NewRemoteValidator(srv.URL, func() string { return testNodeIPv4 }, testNodeSecret)
	got := v("tok1", "dev1", "")
	if got.OK || got.Code != -1 {
		t.Errorf("timeout should be undetermined (-1), got %+v", got)
	}
}

// TestNewRemoteValidator_EmptySecret pins fail-closed behavior: with no node
// secret the validator must reject WITHOUT hitting Center — an unauthenticated
// request would 401 anyway, and firing it would leak a per-connection request
// storm at Center for zero benefit. This is "no opinion" (node's own fault),
// not an explicit rejection — the chain tail fail-closes it to 401.
func TestNewRemoteValidator_EmptySecret(t *testing.T) {
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		json.NewEncoder(w).Encode(map[string]any{"code": 0})
	}))
	defer srv.Close()

	v := NewRemoteValidator(srv.URL, func() string { return testNodeIPv4 }, "")
	got := v("tok1", "dev1", "")
	if got.OK || got.Code != 0 {
		t.Errorf("empty node secret must be no-opinion, got %+v", got)
	}
	if hit {
		t.Error("empty node secret must not send any request to Center")
	}
}

// TestNewRemoteValidator_ServerError5xx pins §6.4: an HTTP 5xx means Center
// itself is broken, not that it rejected the device — undetermined, not 401.
func TestNewRemoteValidator_ServerError5xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	v := NewRemoteValidator(srv.URL, func() string { return testNodeIPv4 }, testNodeSecret)
	got := v("tok1", "dev1", "")
	if got.OK || got.Code != -1 {
		t.Errorf("HTTP 5xx should be undetermined (-1), got %+v", got)
	}
}

// TestNewRemoteValidator_UnexpectedJSONCode pins §6.4: a JSON code that is
// neither 0 nor a known rejection (401/402/403) means Center answered but not
// with a verdict about this device — undetermined, not 401.
func TestNewRemoteValidator_UnexpectedJSONCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"code": 500})
	}))
	defer srv.Close()

	v := NewRemoteValidator(srv.URL, func() string { return testNodeIPv4 }, testNodeSecret)
	got := v("tok1", "dev1", "")
	if got.OK || got.Code != -1 {
		t.Errorf("unexpected JSON code should be undetermined (-1), got %+v", got)
	}
}

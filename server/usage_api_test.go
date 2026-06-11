package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestUsageAPI drives the loopback usage API mux directly (no real network
// egress). Counters are set via the same-package atomic fields, which is the
// cleanest way to seed traffic without a live relay.
func TestUsageAPI(t *testing.T) {
	h := NewProxyHandler()
	h.rxBytes.Store(1000)
	h.txBytes.Store(500)

	srv := newUsageAPI(h, "127.0.0.1:0")
	mux := srv.Handler

	// GET /usage reflects the counters.
	{
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/usage", nil)
		req.RemoteAddr = "127.0.0.1:54321"
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /usage: code=%d body=%s", rec.Code, rec.Body.String())
		}
		var got struct {
			RX    int64 `json:"rx"`
			TX    int64 `json:"tx"`
			Total int64 `json:"total"`
			TS    int64 `json:"ts"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode usage: %v body=%s", err, rec.Body.String())
		}
		if got.RX != 1000 || got.TX != 500 || got.Total != 1500 {
			t.Fatalf("usage rx/tx/total = %d/%d/%d, want 1000/500/1500", got.RX, got.TX, got.Total)
		}
		if got.TS == 0 {
			t.Fatalf("usage ts should be nonzero unix time, got 0")
		}
	}

	// POST /verdict {"accepting":false} flips the gate off.
	{
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/verdict", strings.NewReader(`{"accepting":false}`))
		req.RemoteAddr = "127.0.0.1:54321"
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("POST /verdict false: code=%d body=%s", rec.Code, rec.Body.String())
		}
		if h.Accepting() {
			t.Fatalf("after verdict false, Accepting() should be false")
		}
	}

	// POST /verdict {"accepting":true} flips it back on.
	{
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/verdict", strings.NewReader(`{"accepting":true}`))
		req.RemoteAddr = "127.0.0.1:54321"
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("POST /verdict true: code=%d body=%s", rec.Code, rec.Body.String())
		}
		if !h.Accepting() {
			t.Fatalf("after verdict true, Accepting() should be true")
		}
	}

	// POST /reset zeroes the counters.
	{
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/reset", nil)
		req.RemoteAddr = "127.0.0.1:54321"
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("POST /reset: code=%d body=%s", rec.Code, rec.Body.String())
		}
		var ok struct {
			OK bool `json:"ok"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &ok); err != nil || !ok.OK {
			t.Fatalf("reset response = %s err=%v", rec.Body.String(), err)
		}
		if rx, tx := h.TrafficBytes(); rx != 0 || tx != 0 {
			t.Fatalf("after reset, traffic = %d/%d, want 0/0", rx, tx)
		}
	}
}

// TestUsageAPILoopbackGuard checks the isLoopback helper and that a request
// arriving with a non-loopback RemoteAddr is rejected with 403.
func TestUsageAPILoopbackGuard(t *testing.T) {
	cases := []struct {
		remoteAddr string
		want       bool
	}{
		{"127.0.0.1:1234", true},
		{"[::1]:1234", true},
		{"8.8.8.8:1234", false},
		{"203.0.113.7:443", false},
		// host with no port (defense-in-depth; ParseIP path)
		{"127.0.0.1", true},
		{"::1", true},
		{"garbage", false},
	}
	for _, c := range cases {
		if got := isLoopback(c.remoteAddr); got != c.want {
			t.Errorf("isLoopback(%q) = %v, want %v", c.remoteAddr, got, c.want)
		}
	}

	// A non-loopback request to a real route is rejected 403.
	h := NewProxyHandler()
	srv := newUsageAPI(h, "127.0.0.1:0")
	mux := srv.Handler

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/usage", nil)
	req.RemoteAddr = "203.0.113.7:55555"
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-loopback GET /usage: code=%d, want 403", rec.Code)
	}

	// Verify a verdict from a non-loopback addr does NOT mutate the gate.
	before := h.Accepting()
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/verdict", bytes.NewReader([]byte(`{"accepting":false}`)))
	req.RemoteAddr = "203.0.113.7:55555"
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-loopback POST /verdict: code=%d, want 403", rec.Code)
	}
	if h.Accepting() != before {
		t.Fatalf("non-loopback verdict mutated the gate")
	}
}

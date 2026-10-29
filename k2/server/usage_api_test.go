package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
}

// TestUsageAPIMutatingEndpointsRemoved asserts the Option D trim: the old
// mutating loopback endpoints (POST /reset, POST /verdict) no longer exist, so
// even a loopback caller gets 404 (route unregistered) — the only way to flip
// the gate / reset counters is now the in-process reporter.
func TestUsageAPIMutatingEndpointsRemoved(t *testing.T) {
	h := NewProxyHandler()
	h.rxBytes.Store(1234)
	srv := newUsageAPI(h, "127.0.0.1:0")
	mux := srv.Handler

	for _, path := range []string{"/reset", "/verdict"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.RemoteAddr = "127.0.0.1:54321"
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("POST %s: code=%d, want 404 (endpoint removed under Option D)", path, rec.Code)
		}
	}

	// Counters/gate untouched by the (now-absent) mutating routes.
	if rx, _ := h.TrafficBytes(); rx != 1234 {
		t.Errorf("rx mutated unexpectedly = %d, want 1234", rx)
	}
	if !h.Accepting() {
		t.Errorf("accept gate should still be its default (true)")
	}
}

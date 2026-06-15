package sidecar

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// fakeMeter is a scripted nicMeter for the reporter tests.
type fakeMeter struct {
	mu          sync.Mutex
	value       int64
	rebaselines int
}

func (f *fakeMeter) CumulativeBytes() (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.value, nil
}
func (f *fakeMeter) Rebaseline() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rebaselines++
	f.value = 0
}

// usageServer is a fake Center /slave/usage returning a scripted response and
// capturing the last request body.
type usageServer struct {
	mu       sync.Mutex
	lastReq  NodeUsageRequest
	resp     NodeUsageResponse
	hits     int
	wantUser string // expected Basic-auth username (ipv4)
}

func (s *usageServer) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.hits++
		_ = json.NewDecoder(r.Body).Decode(&s.lastReq)
		if u, _, ok := r.BasicAuth(); !ok || (s.wantUser != "" && u != s.wantUser) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(usageEnvelope{Code: 0, Data: &s.resp})
	}
}

func TestUsageReporter_ReportsCumulativeWithAuth(t *testing.T) {
	us := &usageServer{resp: NodeUsageResponse{Verdict: "serve", EpochID: 0, NextReportInterval: 60}, wantUser: "1.2.3.4"}
	srv := httptest.NewServer(us.handler())
	defer srv.Close()

	meter := &fakeMeter{value: 4242}
	r := NewUsageReporter(meter, srv.URL, "1.2.3.4", "secret-xyz")

	sleep := r.runOnce(context.Background())

	us.mu.Lock()
	defer us.mu.Unlock()
	if us.hits != 1 {
		t.Fatalf("hits = %d, want 1", us.hits)
	}
	if us.lastReq.CumulativeBytes != 4242 {
		t.Fatalf("CumulativeBytes = %d, want 4242", us.lastReq.CumulativeBytes)
	}
	if sleep != 60*time.Second {
		t.Fatalf("sleep = %v, want 60s", sleep)
	}
}

func TestUsageReporter_EpochChangeRebaselines(t *testing.T) {
	us := &usageServer{resp: NodeUsageResponse{Verdict: "serve", EpochID: 7, NextReportInterval: 30}}
	srv := httptest.NewServer(us.handler())
	defer srv.Close()

	meter := &fakeMeter{value: 999}
	r := NewUsageReporter(meter, srv.URL, "1.2.3.4", "s")

	r.runOnce(context.Background()) // adopts epoch 7 → rebaseline

	meter.mu.Lock()
	defer meter.mu.Unlock()
	if meter.rebaselines != 1 {
		t.Fatalf("rebaselines = %d, want 1 (epoch 0→7)", meter.rebaselines)
	}
}

func TestUsageReporter_CenterFailureBacksOff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	meter := &fakeMeter{value: 100}
	r := NewUsageReporter(meter, srv.URL, "1.2.3.4", "s")

	sleep := r.runOnce(context.Background())
	if sleep != usageReportMaxBackoff {
		t.Fatalf("sleep on failure = %v, want %v", sleep, usageReportMaxBackoff)
	}
}

func TestUsageReporter_PayloadTagsMatchCenter(t *testing.T) {
	// Guard: the JSON tags must match center.NodeUsageRequest exactly, else
	// metering silently breaks. Marshal and assert the wire keys.
	b, _ := json.Marshal(NodeUsageRequest{EpochID: 1, CumulativeBytes: 2, Seq: 3, Ts: 4})
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	for _, k := range []string{"epoch_id", "cumulative_bytes", "seq", "ts"} {
		if _, ok := m[k]; !ok {
			t.Fatalf("NodeUsageRequest missing JSON key %q (got %v)", k, m)
		}
	}
}

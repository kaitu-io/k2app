package sidecar

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// captureSlog redirects the default slog logger to buf and returns a restore
// func. Used to assert the node secret never appears in log output.
func captureSlog(buf *strings.Builder) func() {
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	return func() { slog.SetDefault(prev) }
}

// k2sStub stands in for the loopback k2s usage API. It serves GET /usage with a
// settable total, and records the bodies POSTed to /reset and /verdict so tests
// can assert the sidecar pushed the right control signals.
type k2sStub struct {
	mu sync.Mutex

	total int64 // current cumulative bytes returned by GET /usage

	resetCount   int
	verdictBody  []verdictRecord // every /verdict push, in order
	server       *httptest.Server
}

type verdictRecord struct {
	Accepting bool `json:"accepting"`
}

func newK2sStub(total int64) *k2sStub {
	s := &k2sStub{total: total}
	mux := http.NewServeMux()
	mux.HandleFunc("/usage", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		total := s.total
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]int64{
			"rx":    total / 2,
			"tx":    total - total/2,
			"total": total,
			"ts":    time.Now().Unix(),
		})
	})
	mux.HandleFunc("/reset", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.resetCount++
		s.total = 0
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	})
	mux.HandleFunc("/verdict", func(w http.ResponseWriter, r *http.Request) {
		var v verdictRecord
		_ = json.NewDecoder(r.Body).Decode(&v)
		s.mu.Lock()
		s.verdictBody = append(s.verdictBody, v)
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	})
	s.server = httptest.NewServer(mux)
	return s
}

func (s *k2sStub) URL() string { return s.server.URL }
func (s *k2sStub) Close()      { s.server.Close() }

func (s *k2sStub) setTotal(v int64) {
	s.mu.Lock()
	s.total = v
	s.mu.Unlock()
}

func (s *k2sStub) resets() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.resetCount
}

func (s *k2sStub) lastVerdict() (verdictRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.verdictBody) == 0 {
		return verdictRecord{}, false
	}
	return s.verdictBody[len(s.verdictBody)-1], true
}

func (s *k2sStub) verdictCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.verdictBody)
}

// centerStub stands in for Center's POST /slave/usage. It records every request
// body and replies with a scripted response (closure can vary per call).
type centerStub struct {
	mu sync.Mutex

	reqs []NodeUsageRequest

	// respFn produces the response for the Nth call (0-indexed). If it returns
	// fail=true, the handler writes a 500 to simulate a Center error.
	respFn func(n int, req NodeUsageRequest) (resp NodeUsageResponse, fail bool)

	server *httptest.Server
}

func newCenterStub(respFn func(n int, req NodeUsageRequest) (NodeUsageResponse, bool)) *centerStub {
	c := &centerStub{respFn: respFn}
	mux := http.NewServeMux()
	mux.HandleFunc("/slave/usage", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req NodeUsageRequest
		_ = json.Unmarshal(body, &req)
		c.mu.Lock()
		n := len(c.reqs)
		c.reqs = append(c.reqs, req)
		c.mu.Unlock()

		resp, fail := c.respFn(n, req)
		if fail {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(CenterResponse[NodeUsageResponse]{
			Code: 0,
			Data: &resp,
		})
	})
	c.server = httptest.NewServer(mux)
	return c
}

func (c *centerStub) URL() string { return c.server.URL }
func (c *centerStub) Close()      { c.server.Close() }

// rawReqs returns the deserialized request bodies as maps, for shape assertions.
func (c *centerStub) requests() []NodeUsageRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]NodeUsageRequest, len(c.reqs))
	copy(out, c.reqs)
	return out
}

// newTestHeartbeat builds a UsageHeartbeat wired to the two stubs.
func newTestHeartbeat(usageURL string, center *centerStub) *UsageHeartbeat {
	node := &Node{
		CenterURL: center.URL(),
		Secret:    "supersecret-xyz",
		IPv4:      "203.0.113.7",
	}
	return NewUsageHeartbeat(node, usageURL)
}

func TestUsageHeartbeat_ServeToStopPropagation(t *testing.T) {
	k2s := newK2sStub(1000)
	defer k2s.Close()
	center := newCenterStub(func(n int, req NodeUsageRequest) (NodeUsageResponse, bool) {
		return NodeUsageResponse{Verdict: "stop", EpochID: 0, EpochHardCeilingBytes: 10000, NextReportInterval: 60}, false
	})
	defer center.Close()

	hb := newTestHeartbeat(k2s.URL(), center)
	if _, err := hb.runOnce(context.Background()); err != nil {
		t.Fatalf("runOnce: %v", err)
	}

	v, ok := k2s.lastVerdict()
	if !ok {
		t.Fatal("expected a /verdict push to k2s, got none")
	}
	if v.Accepting {
		t.Fatalf("verdict=stop must push accepting:false, got accepting:%v", v.Accepting)
	}
}

func TestUsageHeartbeat_StopToServeRecovery(t *testing.T) {
	k2s := newK2sStub(1000)
	defer k2s.Close()
	verdict := "stop"
	center := newCenterStub(func(n int, req NodeUsageRequest) (NodeUsageResponse, bool) {
		return NodeUsageResponse{Verdict: verdict, EpochID: 0, EpochHardCeilingBytes: 10000, NextReportInterval: 60}, false
	})
	defer center.Close()

	hb := newTestHeartbeat(k2s.URL(), center)
	if _, err := hb.runOnce(context.Background()); err != nil {
		t.Fatalf("runOnce 1: %v", err)
	}
	v, _ := k2s.lastVerdict()
	if v.Accepting {
		t.Fatalf("first cycle: expected accepting:false")
	}

	verdict = "serve"
	if _, err := hb.runOnce(context.Background()); err != nil {
		t.Fatalf("runOnce 2: %v", err)
	}
	v, _ = k2s.lastVerdict()
	if !v.Accepting {
		t.Fatalf("recovery cycle: expected accepting:true, got accepting:%v", v.Accepting)
	}
}

func TestUsageHeartbeat_EpochChangeTriggersReset(t *testing.T) {
	k2s := newK2sStub(5000)
	defer k2s.Close()
	// First response stays on epoch 0; second response bumps to epoch 7.
	center := newCenterStub(func(n int, req NodeUsageRequest) (NodeUsageResponse, bool) {
		if n == 0 {
			return NodeUsageResponse{Verdict: "serve", EpochID: 0, NextReportInterval: 60}, false
		}
		return NodeUsageResponse{Verdict: "serve", EpochID: 7, NextReportInterval: 60}, false
	})
	defer center.Close()

	hb := newTestHeartbeat(k2s.URL(), center)
	if _, err := hb.runOnce(context.Background()); err != nil {
		t.Fatalf("runOnce 1: %v", err)
	}
	if k2s.resets() != 0 {
		t.Fatalf("epoch unchanged: expected 0 resets, got %d", k2s.resets())
	}

	if _, err := hb.runOnce(context.Background()); err != nil {
		t.Fatalf("runOnce 2: %v", err)
	}
	if k2s.resets() != 1 {
		t.Fatalf("epoch changed: expected exactly 1 reset, got %d", k2s.resets())
	}

	// Third cycle must carry the adopted epoch 7.
	if _, err := hb.runOnce(context.Background()); err != nil {
		t.Fatalf("runOnce 3: %v", err)
	}
	reqs := center.requests()
	if len(reqs) < 3 {
		t.Fatalf("expected >=3 Center reports, got %d", len(reqs))
	}
	if reqs[2].EpochID != 7 {
		t.Fatalf("third report must carry adopted epoch 7, got %d", reqs[2].EpochID)
	}
}

func TestUsageHeartbeat_ReportShape(t *testing.T) {
	k2s := newK2sStub(123456)
	defer k2s.Close()
	center := newCenterStub(func(n int, req NodeUsageRequest) (NodeUsageResponse, bool) {
		return NodeUsageResponse{Verdict: "serve", EpochID: 0, NextReportInterval: 60}, false
	})
	defer center.Close()

	hb := newTestHeartbeat(k2s.URL(), center)
	before := time.Now().Unix()
	if _, err := hb.runOnce(context.Background()); err != nil {
		t.Fatalf("runOnce 1: %v", err)
	}
	if _, err := hb.runOnce(context.Background()); err != nil {
		t.Fatalf("runOnce 2: %v", err)
	}
	after := time.Now().Unix()

	reqs := center.requests()
	if len(reqs) != 2 {
		t.Fatalf("expected 2 reports, got %d", len(reqs))
	}
	if reqs[0].CumulativeBytes != 123456 {
		t.Fatalf("cumulative_bytes must equal k2s total 123456, got %d", reqs[0].CumulativeBytes)
	}
	if reqs[0].EpochID != 0 {
		t.Fatalf("first report epoch_id should be 0, got %d", reqs[0].EpochID)
	}
	// seq strictly increasing.
	if !(reqs[1].Seq > reqs[0].Seq) {
		t.Fatalf("seq must be monotonically increasing: %d then %d", reqs[0].Seq, reqs[1].Seq)
	}
	// ts within the wall-clock window of the run.
	if reqs[0].Ts < before || reqs[0].Ts > after {
		t.Fatalf("ts %d out of expected window [%d,%d]", reqs[0].Ts, before, after)
	}
}

func TestUsageHeartbeat_OfflineHardCeilingEnforcement(t *testing.T) {
	k2s := newK2sStub(1000)
	defer k2s.Close()
	// First cycle: Center succeeds, hands down a hard ceiling of 5000 and serve.
	// Second cycle: Center fails (offline) AND k2s total is now above ceiling.
	calls := 0
	center := newCenterStub(func(n int, req NodeUsageRequest) (NodeUsageResponse, bool) {
		calls++
		if n == 0 {
			return NodeUsageResponse{Verdict: "serve", EpochID: 0, EpochHardCeilingBytes: 5000, NextReportInterval: 60}, false
		}
		return NodeUsageResponse{}, true // offline
	})
	defer center.Close()

	hb := newTestHeartbeat(k2s.URL(), center)
	if _, err := hb.runOnce(context.Background()); err != nil {
		t.Fatalf("runOnce 1: %v", err)
	}
	// total now exceeds the learned ceiling.
	k2s.setTotal(6000)
	verdictsBefore := k2s.verdictCount()

	if _, err := hb.runOnce(context.Background()); err == nil {
		t.Fatalf("runOnce 2 should surface the Center error")
	}

	if k2s.verdictCount() <= verdictsBefore {
		t.Fatalf("offline + over-ceiling: expected a /verdict push to k2s, none happened")
	}
	v, _ := k2s.lastVerdict()
	if v.Accepting {
		t.Fatalf("offline over-ceiling must push accepting:false, got accepting:%v", v.Accepting)
	}
}

func TestUsageHeartbeat_OfflineUnderCeilingNoCutoff(t *testing.T) {
	k2s := newK2sStub(1000)
	defer k2s.Close()
	center := newCenterStub(func(n int, req NodeUsageRequest) (NodeUsageResponse, bool) {
		if n == 0 {
			return NodeUsageResponse{Verdict: "serve", EpochID: 0, EpochHardCeilingBytes: 5000, NextReportInterval: 60}, false
		}
		return NodeUsageResponse{}, true // offline
	})
	defer center.Close()

	hb := newTestHeartbeat(k2s.URL(), center)
	if _, err := hb.runOnce(context.Background()); err != nil {
		t.Fatalf("runOnce 1: %v", err)
	}
	// Still under ceiling.
	k2s.setTotal(2000)
	verdictsBefore := k2s.verdictCount()

	if _, err := hb.runOnce(context.Background()); err == nil {
		t.Fatalf("runOnce 2 should surface the Center error")
	}
	if k2s.verdictCount() != verdictsBefore {
		t.Fatalf("offline + under-ceiling: must NOT push a cutoff verdict, but verdict count grew")
	}
}

func TestUsageHeartbeat_DisabledWhenURLEmpty(t *testing.T) {
	center := newCenterStub(func(n int, req NodeUsageRequest) (NodeUsageResponse, bool) {
		return NodeUsageResponse{Verdict: "serve"}, false
	})
	defer center.Close()

	node := &Node{CenterURL: center.URL(), Secret: "s", IPv4: "203.0.113.9"}
	hb := NewUsageHeartbeat(node, "")
	if hb.Enabled() {
		t.Fatalf("empty UsageAPIURL must be disabled")
	}

	// Run() must be a no-op and return promptly even with a live context.
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { hb.Run(ctx); close(done) }()
	select {
	case <-done:
		// good — returned immediately
	case <-time.After(2 * time.Second):
		t.Fatal("Run() with empty URL should return immediately, but it blocked")
	}
	cancel()

	if len(center.requests()) != 0 {
		t.Fatalf("disabled heartbeat must never contact Center, got %d reports", len(center.requests()))
	}
}

func TestUsageHeartbeat_SecretNotLogged(t *testing.T) {
	k2s := newK2sStub(1000)
	defer k2s.Close()
	center := newCenterStub(func(n int, req NodeUsageRequest) (NodeUsageResponse, bool) {
		return NodeUsageResponse{Verdict: "stop", EpochID: 0, EpochHardCeilingBytes: 9000, NextReportInterval: 60}, false
	})
	defer center.Close()

	var buf strings.Builder
	restore := captureSlog(&buf)
	defer restore()

	const secret = "TOPSECRET-NODE-CRED-9988"
	node := &Node{CenterURL: center.URL(), Secret: secret, IPv4: "203.0.113.11"}
	hb := NewUsageHeartbeat(node, k2s.URL())
	if _, err := hb.runOnce(context.Background()); err != nil {
		t.Fatalf("runOnce: %v", err)
	}

	if strings.Contains(buf.String(), secret) {
		t.Fatalf("node secret leaked into logs:\n%s", buf.String())
	}
}

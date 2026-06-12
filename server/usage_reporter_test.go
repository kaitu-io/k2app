package server

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeController is a hand-written trafficController that records SetAccepting /
// ResetTraffic calls and returns scripted TrafficBytes. Single-goroutine tests
// drive it via runOnce, but it carries a mutex so a -race run stays clean if the
// loop is ever exercised concurrently.
type fakeController struct {
	mu sync.Mutex

	rx, tx int64

	acceptingCalls []bool
	resetCalls     int
}

func (f *fakeController) TrafficBytes() (int64, int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rx, f.tx
}

func (f *fakeController) ResetTraffic() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resetCalls++
	f.rx, f.tx = 0, 0
}

func (f *fakeController) SetAccepting(v bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.acceptingCalls = append(f.acceptingCalls, v)
}

func (f *fakeController) setTotal(rx, tx int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rx, f.tx = rx, tx
}

func (f *fakeController) lastAccepting() (bool, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.acceptingCalls) == 0 {
		return false, false
	}
	return f.acceptingCalls[len(f.acceptingCalls)-1], true
}

func (f *fakeController) resets() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.resetCalls
}

func (f *fakeController) acceptingHistory() []bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]bool, len(f.acceptingCalls))
	copy(out, f.acceptingCalls)
	return out
}

// centerStub is a controllable Center httptest.Server. capturedReq records the
// last deserialized request body so tests can assert on the report shape.
type centerStub struct {
	mu sync.Mutex

	// response the stub returns inside the {code,message,data} envelope.
	resp NodeUsageResponse
	code int    // envelope code (0 = success)
	msg  string // envelope message

	httpStatus int  // overridden HTTP status (0 → 200)
	failNext   bool // if true, return HTTP 500 to simulate a Center failure

	capturedReq  map[string]any
	capturedAuth string
	hits         int
}

func newCenterStub() *centerStub {
	return &centerStub{
		resp: NodeUsageResponse{Verdict: "serve", EpochID: 0, NextReportInterval: 0},
	}
}

func (s *centerStub) setResp(r NodeUsageResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resp = r
}

func (s *centerStub) setFail(v bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failNext = v
}

func (s *centerStub) setEnvelope(code int, msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.code = code
	s.msg = msg
}

func (s *centerStub) lastReq() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.capturedReq
}

func (s *centerStub) auth() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.capturedAuth
}

func (s *centerStub) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/slave/usage", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.hits++
		s.capturedAuth = r.Header.Get("Authorization")
		var m map[string]any
		_ = json.NewDecoder(r.Body).Decode(&m)
		s.capturedReq = m
		fail := s.failNext
		httpStatus := s.httpStatus
		code := s.code
		msg := s.msg
		resp := s.resp
		s.mu.Unlock()

		if fail {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if httpStatus != 0 {
			w.WriteHeader(httpStatus)
		}
		w.Header().Set("Content-Type", "application/json")
		env := map[string]any{"code": code, "message": msg, "data": resp}
		_ = json.NewEncoder(w).Encode(env)
	})
	return mux
}

func newStubServer(t *testing.T) (*centerStub, *httptest.Server) {
	t.Helper()
	stub := newCenterStub()
	srv := httptest.NewServer(stub.handler())
	t.Cleanup(srv.Close)
	return stub, srv
}

// TestUsageReporterServeToStop: Center returns verdict "stop" → SetAccepting(false).
func TestUsageReporterServeToStop(t *testing.T) {
	stub, srv := newStubServer(t)
	fake := &fakeController{rx: 100, tx: 200}
	r := newUsageReporter(fake, srv.URL, "1.2.3.4", "topsecret")

	// first cycle: serve
	stub.setResp(NodeUsageResponse{Verdict: "serve", EpochID: 0, NextReportInterval: 1})
	if _, err := r.runOnce(context.Background()); err != nil {
		t.Fatalf("runOnce serve: %v", err)
	}
	if last, ok := fake.lastAccepting(); !ok || last != true {
		t.Fatalf("after serve want SetAccepting(true), got %v ok=%v", last, ok)
	}

	// second cycle: stop
	stub.setResp(NodeUsageResponse{Verdict: "stop", EpochID: 0, NextReportInterval: 1})
	if _, err := r.runOnce(context.Background()); err != nil {
		t.Fatalf("runOnce stop: %v", err)
	}
	if last, ok := fake.lastAccepting(); !ok || last != false {
		t.Fatalf("after stop want SetAccepting(false), got %v ok=%v", last, ok)
	}
}

// TestUsageReporterStopToServe: stop then serve → SetAccepting(true) on recovery.
func TestUsageReporterStopToServe(t *testing.T) {
	stub, srv := newStubServer(t)
	fake := &fakeController{rx: 1, tx: 1}
	r := newUsageReporter(fake, srv.URL, "1.2.3.4", "topsecret")

	stub.setResp(NodeUsageResponse{Verdict: "stop", EpochID: 0, NextReportInterval: 1})
	if _, err := r.runOnce(context.Background()); err != nil {
		t.Fatalf("runOnce stop: %v", err)
	}
	stub.setResp(NodeUsageResponse{Verdict: "serve", EpochID: 0, NextReportInterval: 1})
	if _, err := r.runOnce(context.Background()); err != nil {
		t.Fatalf("runOnce serve: %v", err)
	}
	if last, ok := fake.lastAccepting(); !ok || last != true {
		t.Fatalf("after recovery want SetAccepting(true), got %v ok=%v", last, ok)
	}
}

// TestUsageReporterEpochChange: a new epoch_id → ResetTraffic() + next report
// carries the new epoch_id.
func TestUsageReporterEpochChange(t *testing.T) {
	stub, srv := newStubServer(t)
	fake := &fakeController{rx: 500, tx: 500}
	r := newUsageReporter(fake, srv.URL, "1.2.3.4", "topsecret")

	// first cycle still epoch 0 (reporter starts at 0) → no reset.
	stub.setResp(NodeUsageResponse{Verdict: "serve", EpochID: 0, NextReportInterval: 1})
	if _, err := r.runOnce(context.Background()); err != nil {
		t.Fatalf("runOnce epoch0: %v", err)
	}
	if got := fake.resets(); got != 0 {
		t.Fatalf("epoch unchanged want 0 resets, got %d", got)
	}

	// second cycle: Center bumps to epoch 7 → ResetTraffic must fire.
	stub.setResp(NodeUsageResponse{Verdict: "serve", EpochID: 7, NextReportInterval: 1})
	if _, err := r.runOnce(context.Background()); err != nil {
		t.Fatalf("runOnce epoch7: %v", err)
	}
	if got := fake.resets(); got != 1 {
		t.Fatalf("epoch change want 1 reset, got %d", got)
	}

	// third cycle: POST body must now carry epoch_id == 7.
	stub.setResp(NodeUsageResponse{Verdict: "serve", EpochID: 7, NextReportInterval: 1})
	if _, err := r.runOnce(context.Background()); err != nil {
		t.Fatalf("runOnce post-adopt: %v", err)
	}
	req := stub.lastReq()
	if req == nil {
		t.Fatal("no request captured")
	}
	// JSON numbers deserialize as float64.
	if ep, _ := req["epoch_id"].(float64); int64(ep) != 7 {
		t.Fatalf("after epoch adopt want epoch_id=7 in report, got %v", req["epoch_id"])
	}
}

// TestUsageReporterReportShape: body has correct epoch_id, cumulative_bytes ==
// total, monotonically increasing seq, ts > 0.
func TestUsageReporterReportShape(t *testing.T) {
	stub, srv := newStubServer(t)
	fake := &fakeController{rx: 1000, tx: 2345}
	r := newUsageReporter(fake, srv.URL, "9.9.9.9", "shh")
	stub.setResp(NodeUsageResponse{Verdict: "serve", EpochID: 0, NextReportInterval: 1})

	var lastSeq int64 = -1
	for i := 0; i < 3; i++ {
		if _, err := r.runOnce(context.Background()); err != nil {
			t.Fatalf("runOnce %d: %v", i, err)
		}
		req := stub.lastReq()
		if req == nil {
			t.Fatalf("cycle %d: no request captured", i)
		}
		if cb, _ := req["cumulative_bytes"].(float64); int64(cb) != 3345 {
			t.Fatalf("cycle %d: cumulative_bytes want 3345, got %v", i, req["cumulative_bytes"])
		}
		if ep, _ := req["epoch_id"].(float64); int64(ep) != 0 {
			t.Fatalf("cycle %d: epoch_id want 0, got %v", i, req["epoch_id"])
		}
		seqF, _ := req["seq"].(float64)
		seq := int64(seqF)
		if seq <= lastSeq {
			t.Fatalf("cycle %d: seq not monotonic: %d <= %d", i, seq, lastSeq)
		}
		lastSeq = seq
		if ts, _ := req["ts"].(float64); int64(ts) <= 0 {
			t.Fatalf("cycle %d: ts want > 0, got %v", i, req["ts"])
		}
	}
}

// TestUsageReporterOfflineHardCeiling: Center failure AND total >= last-seen
// ceiling → SetAccepting(false); epoch unchanged.
func TestUsageReporterOfflineHardCeiling(t *testing.T) {
	stub, srv := newStubServer(t)
	fake := &fakeController{rx: 400, tx: 400} // total 800 < ceiling 1000
	r := newUsageReporter(fake, srv.URL, "1.2.3.4", "topsecret")

	// successful cycle establishes a hard ceiling of 1000 and epoch 3.
	stub.setResp(NodeUsageResponse{Verdict: "serve", EpochID: 3, EpochHardCeilingBytes: 1000, NextReportInterval: 1})
	if _, err := r.runOnce(context.Background()); err != nil {
		t.Fatalf("runOnce establish ceiling: %v", err)
	}

	// now go offline AND push total above the ceiling.
	fake.setTotal(700, 700) // total 1400 >= 1000
	stub.setFail(true)
	if _, err := r.runOnce(context.Background()); err == nil {
		t.Fatal("offline cycle expected an error, got nil")
	}
	if last, ok := fake.lastAccepting(); !ok || last != false {
		t.Fatalf("offline over ceiling want SetAccepting(false), got %v ok=%v", last, ok)
	}

	// epoch must NOT have changed: next successful report must still carry the
	// previously adopted epoch 3.
	stub.setFail(false)
	stub.setResp(NodeUsageResponse{Verdict: "serve", EpochID: 3, EpochHardCeilingBytes: 1000, NextReportInterval: 1})
	if _, err := r.runOnce(context.Background()); err != nil {
		t.Fatalf("runOnce after offline: %v", err)
	}
	req := stub.lastReq()
	if ep, _ := req["epoch_id"].(float64); int64(ep) != 3 {
		t.Fatalf("epoch must be unchanged at 3 after offline, got %v", req["epoch_id"])
	}
}

// TestUsageReporterOfflineUnderCeiling: Center failure but total < ceiling → NO
// SetAccepting(false) cut.
func TestUsageReporterOfflineUnderCeiling(t *testing.T) {
	stub, srv := newStubServer(t)
	fake := &fakeController{rx: 100, tx: 100} // total 200
	r := newUsageReporter(fake, srv.URL, "1.2.3.4", "topsecret")

	stub.setResp(NodeUsageResponse{Verdict: "serve", EpochID: 1, EpochHardCeilingBytes: 1000, NextReportInterval: 1})
	if _, err := r.runOnce(context.Background()); err != nil {
		t.Fatalf("runOnce establish ceiling: %v", err)
	}
	// at this point SetAccepting(true) was recorded; remember count.
	beforeHistory := fake.acceptingHistory()

	// offline, still under ceiling (200 < 1000).
	stub.setFail(true)
	if _, err := r.runOnce(context.Background()); err == nil {
		t.Fatal("offline cycle expected an error, got nil")
	}
	afterHistory := fake.acceptingHistory()
	if len(afterHistory) != len(beforeHistory) {
		t.Fatalf("under-ceiling offline must NOT call SetAccepting; history grew %v → %v", beforeHistory, afterHistory)
	}
}

// TestUsageReporterCodeNonZeroIsError: envelope code != 0 is treated as a
// failure (offline path), not adopted.
func TestUsageReporterCodeNonZeroIsError(t *testing.T) {
	stub, srv := newStubServer(t)
	fake := &fakeController{rx: 10, tx: 10}
	r := newUsageReporter(fake, srv.URL, "1.2.3.4", "topsecret")

	stub.setEnvelope(401, "node context required")
	stub.setResp(NodeUsageResponse{Verdict: "stop"}) // data present but code!=0
	if _, err := r.runOnce(context.Background()); err == nil {
		t.Fatal("code!=0 must surface as an error")
	}
	// verdict must NOT have been applied from a code!=0 reply.
	if _, ok := fake.lastAccepting(); ok {
		t.Fatal("code!=0 reply must not drive SetAccepting")
	}
}

// TestUsageReporterSecretNotLogged: the secret string never appears in slog
// output across serve → stop → error.
func TestUsageReporterSecretNotLogged(t *testing.T) {
	const secret = "SUPER-SECRET-NODE-KEY-xyz789"

	var buf bytes.Buffer
	var bufMu sync.Mutex
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	slog.SetDefault(slog.New(slog.NewTextHandler(&lockedWriter{w: &buf, mu: &bufMu}, &slog.HandlerOptions{Level: slog.LevelDebug})))

	stub, srv := newStubServer(t)
	fake := &fakeController{rx: 5, tx: 5}
	r := newUsageReporter(fake, srv.URL, "1.2.3.4", secret)

	stub.setResp(NodeUsageResponse{Verdict: "serve", EpochID: 0, NextReportInterval: 1})
	_, _ = r.runOnce(context.Background())
	stub.setResp(NodeUsageResponse{Verdict: "stop", EpochID: 0, NextReportInterval: 1})
	_, _ = r.runOnce(context.Background())
	stub.setFail(true)
	_, _ = r.runOnce(context.Background())

	bufMu.Lock()
	logs := buf.String()
	bufMu.Unlock()
	if strings.Contains(logs, secret) {
		t.Fatalf("secret leaked into logs:\n%s", logs)
	}
	// the base64(ipv4:secret) credential must not leak either.
	if strings.Contains(logs, "Basic ") {
		t.Fatalf("auth header leaked into logs:\n%s", logs)
	}
}

// lockedWriter serializes writes to the underlying buffer so the slog handler is
// race-safe under -race.
type lockedWriter struct {
	w  *bytes.Buffer
	mu *sync.Mutex
}

func (lw *lockedWriter) Write(p []byte) (int, error) {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	return lw.w.Write(p)
}

// TestUsageReporterAuthHeader: the POST carries Basic base64(ipv4:secret).
func TestUsageReporterAuthHeader(t *testing.T) {
	stub, srv := newStubServer(t)
	fake := &fakeController{rx: 1, tx: 1}
	r := newUsageReporter(fake, srv.URL, "203.0.113.7", "pw")
	stub.setResp(NodeUsageResponse{Verdict: "serve", NextReportInterval: 1})
	if _, err := r.runOnce(context.Background()); err != nil {
		t.Fatalf("runOnce: %v", err)
	}
	want := "Basic " + base64Std("203.0.113.7:pw")
	if got := stub.auth(); got != want {
		t.Fatalf("auth header want %q, got %q", want, got)
	}
}

// TestUsageReporterDefaultInterval: next_report_interval <= 0 → 60s fallback.
func TestUsageReporterDefaultInterval(t *testing.T) {
	stub, srv := newStubServer(t)
	fake := &fakeController{rx: 1, tx: 1}
	r := newUsageReporter(fake, srv.URL, "1.2.3.4", "pw")
	stub.setResp(NodeUsageResponse{Verdict: "serve", NextReportInterval: 0})
	sleep, err := r.runOnce(context.Background())
	if err != nil {
		t.Fatalf("runOnce: %v", err)
	}
	if sleep != 60*time.Second {
		t.Fatalf("interval 0 want 60s fallback, got %v", sleep)
	}
}

func base64Std(s string) string {
	return base64StdEncode([]byte(s))
}

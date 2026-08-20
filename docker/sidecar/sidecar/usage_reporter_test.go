package sidecar

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// fakeStats is a scripted statsSource shared by the enforcer + reporter tests.
// Both stats and err are guarded so tests can flip them between reconcile calls
// under -race.
type fakeStats struct {
	mu    sync.Mutex
	stats TrafficStats
	err   error
}

func (f *fakeStats) GetTrafficStats() (TrafficStats, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stats, f.err
}

func (f *fakeStats) set(stats TrafficStats, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stats, f.err = stats, err
}

var errMeter = errors.New("meter fail")

// usageServer is a fake Center /slave/usage returning a scripted response and
// capturing the last request body.
type usageServer struct {
	mu       sync.Mutex
	lastReq  NodeUsageRequest
	resp     NodeUsageResponse
	hits     int
	status   int    // override response status (0 = 200)
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
		if s.status != 0 {
			w.WriteHeader(s.status)
			return
		}
		_ = json.NewEncoder(w).Encode(usageEnvelope{Code: 0, Data: &s.resp})
	}
}

func (s *usageServer) hitCount() int { s.mu.Lock(); defer s.mu.Unlock(); return s.hits }

func TestReporter_ReportsTrafficMonitorStats(t *testing.T) {
	us := &usageServer{resp: NodeUsageResponse{NextReportInterval: 60}, wantUser: "1.2.3.4"}
	srv := httptest.NewServer(us.handler())
	defer srv.Close()

	src := &fakeStats{stats: TrafficStats{
		BillingCycleEndAt:        1700000000,
		MonthlyTrafficLimitBytes: 2 << 40,
		UsedTrafficBytes:         3 << 30,
	}}
	r := NewUsageReporter(src, srv.URL, "1.2.3.4", "secret-xyz")

	r.runOnce(context.Background())

	us.mu.Lock()
	defer us.mu.Unlock()
	assert.Equal(t, 1, us.hits)
	assert.Equal(t, int64(1700000000), us.lastReq.EpochID)
	assert.Equal(t, int64(3<<30), us.lastReq.CumulativeBytes)
	assert.Equal(t, int64(2<<40), us.lastReq.QuotaTotalBytes)
}

func TestReporter_NoReportOnMeterError(t *testing.T) {
	us := &usageServer{resp: NodeUsageResponse{NextReportInterval: 60}}
	srv := httptest.NewServer(us.handler())
	defer srv.Close()

	src := &fakeStats{err: errMeter}
	r := NewUsageReporter(src, srv.URL, "1.2.3.4", "secret")

	sleep := r.runOnce(context.Background())

	assert.Equal(t, 0, us.hitCount(), "must NOT POST on meter error")
	assert.Equal(t, usageReportMaxBackoff, sleep)
}

func TestReporter_HonorsNextReportInterval(t *testing.T) {
	us := &usageServer{resp: NodeUsageResponse{NextReportInterval: 120}, wantUser: "1.2.3.4"}
	srv := httptest.NewServer(us.handler())
	defer srv.Close()

	src := &fakeStats{stats: TrafficStats{MonthlyTrafficLimitBytes: 2 << 40, UsedTrafficBytes: 1 << 30}}
	r := NewUsageReporter(src, srv.URL, "1.2.3.4", "secret")

	assert.Equal(t, 120*time.Second, r.runOnce(context.Background()))
}

func TestReporter_FloorsZeroInterval(t *testing.T) {
	us := &usageServer{resp: NodeUsageResponse{NextReportInterval: 0}, wantUser: "1.2.3.4"}
	srv := httptest.NewServer(us.handler())
	defer srv.Close()

	src := &fakeStats{stats: TrafficStats{MonthlyTrafficLimitBytes: 2 << 40, UsedTrafficBytes: 1 << 30}}
	r := NewUsageReporter(src, srv.URL, "1.2.3.4", "secret")

	assert.Equal(t, usageReportDefaultInterval, r.runOnce(context.Background()))
}

func TestUsageReporter_CenterFailureBacksOff(t *testing.T) {
	us := &usageServer{status: http.StatusInternalServerError}
	srv := httptest.NewServer(us.handler())
	defer srv.Close()

	src := &fakeStats{stats: TrafficStats{MonthlyTrafficLimitBytes: 2 << 40, UsedTrafficBytes: 1 << 30}}
	r := NewUsageReporter(src, srv.URL, "1.2.3.4", "secret")

	assert.Equal(t, usageReportMaxBackoff, r.runOnce(context.Background()))
}

func TestUsageReporter_PayloadTagsMatchCenter(t *testing.T) {
	// Guard: the JSON tags must match center.NodeUsageRequest exactly, else
	// metering silently breaks. Marshal and assert the wire keys.
	b, _ := json.Marshal(NodeUsageRequest{EpochID: 1, CumulativeBytes: 2, QuotaTotalBytes: 3, Seq: 4, Ts: 5})
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	for _, k := range []string{"epoch_id", "cumulative_bytes", "quota_total_bytes", "seq", "ts"} {
		if _, ok := m[k]; !ok {
			t.Fatalf("NodeUsageRequest missing JSON key %q (got %v)", k, m)
		}
	}
}

// fakeAdoptingStats is a statsSource that also records AdoptAuthoritativeUsed
// calls (mirrors the real TrafficMonitor's optional capability).
type fakeAdoptingStats struct {
	fakeStats
	adoptedBytes int64
	adoptedEpoch int64
	adoptCalls   int
}

func (f *fakeAdoptingStats) AdoptAuthoritativeUsed(authBytes int64, epochID int64) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.adoptCalls++
	f.adoptedBytes = authBytes
	f.adoptedEpoch = epochID
	return true, nil
}

// TestReporter_AdoptsAuthoritativeCorrection: when Center's response carries a
// provider figure ABOVE the self-report, the reporter ratchets it into the
// meter, pinned to the epoch it just reported.
func TestReporter_AdoptsAuthoritativeCorrection(t *testing.T) {
	us := &usageServer{resp: NodeUsageResponse{NextReportInterval: 60, AuthoritativeUsedBytes: 9 << 30}, wantUser: "1.2.3.4"}
	srv := httptest.NewServer(us.handler())
	defer srv.Close()

	src := &fakeAdoptingStats{fakeStats: fakeStats{stats: TrafficStats{
		BillingCycleEndAt:        1700000000,
		MonthlyTrafficLimitBytes: 2 << 40,
		UsedTrafficBytes:         3 << 30,
	}}}
	r := NewUsageReporter(src, srv.URL, "1.2.3.4", "secret")

	r.runOnce(context.Background())

	src.mu.Lock()
	defer src.mu.Unlock()
	assert.Equal(t, 1, src.adoptCalls)
	assert.Equal(t, int64(9<<30), src.adoptedBytes)
	assert.Equal(t, int64(1700000000), src.adoptedEpoch, "correction pinned to the reported epoch")
}

// TestReporter_IgnoresLowerOrAbsentCorrection: a figure at/below the self-report
// (or absent = 0) never reaches the adopter — the ratchet is upward-only.
func TestReporter_IgnoresLowerOrAbsentCorrection(t *testing.T) {
	for _, auth := range []int64{0, 3 << 30, 1 << 30} {
		us := &usageServer{resp: NodeUsageResponse{NextReportInterval: 60, AuthoritativeUsedBytes: auth}, wantUser: "1.2.3.4"}
		srv := httptest.NewServer(us.handler())

		src := &fakeAdoptingStats{fakeStats: fakeStats{stats: TrafficStats{
			BillingCycleEndAt: 1700000000,
			UsedTrafficBytes:  3 << 30,
		}}}
		r := NewUsageReporter(src, srv.URL, "1.2.3.4", "secret")
		r.runOnce(context.Background())
		srv.Close()

		src.mu.Lock()
		assert.Equal(t, 0, src.adoptCalls, "auth=%d must not trigger adoption", auth)
		src.mu.Unlock()
	}
}

// TestReporter_NoAdopterIsSafe: a plain statsSource (no adopter capability)
// with a correction in the response must not panic or misbehave.
func TestReporter_NoAdopterIsSafe(t *testing.T) {
	us := &usageServer{resp: NodeUsageResponse{NextReportInterval: 60, AuthoritativeUsedBytes: 9 << 30}, wantUser: "1.2.3.4"}
	srv := httptest.NewServer(us.handler())
	defer srv.Close()

	src := &fakeStats{stats: TrafficStats{UsedTrafficBytes: 1 << 30}}
	r := NewUsageReporter(src, srv.URL, "1.2.3.4", "secret")
	assert.NotPanics(t, func() { r.runOnce(context.Background()) })
	assert.Equal(t, 1, us.hitCount())
}

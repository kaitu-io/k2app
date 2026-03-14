package dns

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// --- pick() tests ---

func TestPick_AllHealthy(t *testing.T) {
	r := newRacingResolver(
		[]string{"a:53", "b:53", "c:53", "d:53", "e:53", "f:53"},
		nil, 2, 5*time.Second, "test",
	)
	// Set latencies: b fastest, d second
	r.servers[1].latencyNs.Store(10_000_000)  // 10ms
	r.servers[3].latencyNs.Store(20_000_000)  // 20ms
	r.servers[0].latencyNs.Store(50_000_000)  // 50ms

	selected := r.pick()
	if len(selected) != 2 {
		t.Fatalf("len = %d, want 2", len(selected))
	}
	if selected[0].addr != "b:53" || selected[1].addr != "d:53" {
		t.Errorf("selected = [%s, %s], want [b:53, d:53]", selected[0].addr, selected[1].addr)
	}
}

func TestPick_DegradedExcluded(t *testing.T) {
	r := newRacingResolver(
		[]string{"a:53", "b:53", "c:53"},
		nil, 2, 5*time.Second, "test",
	)
	r.servers[0].consecutiveFail.Store(3) // degraded
	r.servers[1].latencyNs.Store(10_000_000)
	r.servers[2].latencyNs.Store(20_000_000)

	selected := r.pick()
	if len(selected) != 2 {
		t.Fatalf("len = %d, want 2", len(selected))
	}
	for _, s := range selected {
		if s.addr == "a:53" {
			t.Error("degraded server a:53 should not be selected")
		}
	}
}

func TestPick_DegradedProbeWhenRoom(t *testing.T) {
	r := newRacingResolver(
		[]string{"a:53", "b:53", "c:53"},
		nil, 2, 5*time.Second, "test",
	)
	// a and b degraded, c healthy
	r.servers[0].consecutiveFail.Store(3)
	r.servers[1].consecutiveFail.Store(3)
	r.servers[2].latencyNs.Store(10_000_000)

	selected := r.pick()
	// 1 healthy (c) + 1 degraded probe (a) = 2
	if len(selected) != 2 {
		t.Fatalf("len = %d, want 2", len(selected))
	}
}

func TestPick_DegradedProbeRateLimited(t *testing.T) {
	r := newRacingResolver(
		[]string{"a:53", "b:53"},
		nil, 2, 5*time.Second, "test",
	)
	// a degraded but recently probed
	r.servers[0].consecutiveFail.Store(3)
	r.servers[0].lastProbe.Store(time.Now().UnixNano()) // just probed
	r.servers[1].latencyNs.Store(10_000_000)

	selected := r.pick()
	// Only b is healthy, a is not due for probe
	if len(selected) != 1 {
		t.Fatalf("len = %d, want 1", len(selected))
	}
	if selected[0].addr != "b:53" {
		t.Errorf("selected = %s, want b:53", selected[0].addr)
	}
}

func TestPick_AllDegraded(t *testing.T) {
	r := newRacingResolver(
		[]string{"a:53", "b:53", "c:53"},
		nil, 2, 5*time.Second, "test",
	)
	for _, s := range r.servers {
		s.consecutiveFail.Store(3)
		s.lastProbe.Store(time.Now().UnixNano()) // not due
	}

	selected := r.pick()
	// Fallback: race all
	if len(selected) != 3 {
		t.Fatalf("len = %d, want 3 (all degraded fallback)", len(selected))
	}
}

func TestPick_SingleServer(t *testing.T) {
	r := newRacingResolver(
		[]string{"a:53"},
		nil, 2, 5*time.Second, "test",
	)
	selected := r.pick()
	if len(selected) != 1 {
		t.Fatalf("len = %d, want 1", len(selected))
	}
}

func TestPick_ZeroServers(t *testing.T) {
	r := newRacingResolver(nil, nil, 2, 5*time.Second, "test")
	selected := r.pick()
	if len(selected) != 0 {
		t.Fatalf("len = %d, want 0", len(selected))
	}
}

// --- serverHealth tests ---

func TestServerHealth_RecordSuccess(t *testing.T) {
	s := &serverHealth{addr: "a:53"}
	s.consecutiveFail.Store(5)

	s.recordSuccess(10_000_000) // 10ms
	if s.consecutiveFail.Load() != 0 {
		t.Error("consecutiveFail should be 0 after success")
	}
	if s.latencyNs.Load() != 10_000_000 {
		t.Errorf("first latency should be stored raw, got %d", s.latencyNs.Load())
	}
	if s.lastSuccess.Load() == 0 {
		t.Error("lastSuccess should be set")
	}
}

func TestServerHealth_LatencyEWMA(t *testing.T) {
	s := &serverHealth{addr: "a:53"}
	s.recordSuccess(100_000_000) // 100ms
	s.recordSuccess(200_000_000) // 200ms

	// EWMA: 0.3*200 + 0.7*100 = 130ms
	got := s.latencyNs.Load()
	if got != 130_000_000 {
		t.Errorf("latency = %d, want 130000000", got)
	}
}

func TestServerHealth_DegradationAndRecovery(t *testing.T) {
	s := &serverHealth{addr: "a:53"}

	// 3 failures -> degraded
	for i := 0; i < 3; i++ {
		s.recordFailure()
	}
	if s.healthy() {
		t.Error("should be degraded after 3 failures")
	}

	// 1 success -> recovered
	s.recordSuccess(10_000_000)
	if !s.healthy() {
		t.Error("should be healthy after success")
	}
}

// --- Query() tests ---

func buildTestQuery() *dns.Msg {
	q := new(dns.Msg)
	q.SetQuestion("example.com.", dns.TypeA)
	q.Id = 1234
	return q
}

func buildTestResponse(query *dns.Msg) *dns.Msg {
	resp := new(dns.Msg)
	resp.SetReply(query)
	return resp
}

func TestRacingResolver_SingleServerSuccess(t *testing.T) {
	query := buildTestQuery()
	fn := func(ctx context.Context, server string, q *dns.Msg) (*dns.Msg, error) {
		return buildTestResponse(q), nil
	}
	r := newRacingResolver([]string{"a:53"}, fn, 2, 5*time.Second, "test")

	resp, err := r.Query(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	if resp == nil {
		t.Fatal("resp is nil")
	}
}

func TestRacingResolver_FirstSuccessWins(t *testing.T) {
	query := buildTestQuery()
	fn := func(ctx context.Context, server string, q *dns.Msg) (*dns.Msg, error) {
		if server == "slow:53" {
			select {
			case <-time.After(500 * time.Millisecond):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		return buildTestResponse(q), nil
	}
	r := newRacingResolver([]string{"fast:53", "slow:53"}, fn, 2, 5*time.Second, "test")

	start := time.Now()
	resp, err := r.Query(context.Background(), query)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatal(err)
	}
	if resp == nil {
		t.Fatal("resp is nil")
	}
	if elapsed > 400*time.Millisecond {
		t.Errorf("should return fast, took %v", elapsed)
	}
}

func TestRacingResolver_FailoverToSecond(t *testing.T) {
	query := buildTestQuery()
	fn := func(ctx context.Context, server string, q *dns.Msg) (*dns.Msg, error) {
		if server == "bad:53" {
			return nil, errors.New("refused")
		}
		return buildTestResponse(q), nil
	}
	r := newRacingResolver([]string{"bad:53", "good:53"}, fn, 2, 5*time.Second, "test")

	resp, err := r.Query(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	if resp == nil {
		t.Fatal("resp is nil")
	}
}

func TestRacingResolver_AllFail(t *testing.T) {
	query := buildTestQuery()
	fn := func(ctx context.Context, server string, q *dns.Msg) (*dns.Msg, error) {
		return nil, errors.New("fail")
	}
	r := newRacingResolver([]string{"a:53", "b:53"}, fn, 2, 5*time.Second, "test")

	_, err := r.Query(context.Background(), query)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRacingResolver_ZeroServers(t *testing.T) {
	r := newRacingResolver(nil, nil, 2, 5*time.Second, "test")
	_, err := r.Query(context.Background(), buildTestQuery())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRacingResolver_HealthDegradation(t *testing.T) {
	query := buildTestQuery()
	var callLog []string
	var mu sync.Mutex
	fn := func(ctx context.Context, server string, q *dns.Msg) (*dns.Msg, error) {
		mu.Lock()
		callLog = append(callLog, server)
		mu.Unlock()
		if server == "bad:53" {
			return nil, errors.New("fail")
		}
		return buildTestResponse(q), nil
	}
	r := newRacingResolver([]string{"bad:53", "ok1:53", "ok2:53"}, fn, 2, 5*time.Second, "test")

	// Degrade "bad:53" with 3 consecutive failures
	for i := 0; i < 3; i++ {
		r.servers[0].recordFailure()
	}
	// Recently probed so it won't be picked for probe
	r.servers[0].lastProbe.Store(time.Now().UnixNano())

	// Set latencies so pick order is deterministic
	r.servers[1].latencyNs.Store(10_000_000)
	r.servers[2].latencyNs.Store(20_000_000)

	callLog = nil
	_, err := r.Query(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}

	// "bad:53" should NOT have been called
	mu.Lock()
	defer mu.Unlock()
	for _, s := range callLog {
		if s == "bad:53" {
			t.Error("degraded server bad:53 should not be called")
		}
	}
}

func TestServerHealth_ProbeBackoff_Initial(t *testing.T) {
	s := &serverHealth{addr: "a:53"}
	for i := 0; i < 3; i++ {
		s.recordFailure()
	}
	if got := s.probeBackoffDuration(); got != 30*time.Second {
		t.Errorf("initial probe backoff = %v, want 30s", got)
	}
}

func TestServerHealth_ProbeBackoff_Doubles(t *testing.T) {
	s := &serverHealth{addr: "a:53"}
	for i := 0; i < 3; i++ {
		s.recordFailure()
	}
	s.recordProbeAttempt()
	if got := s.probeBackoffDuration(); got != 60*time.Second {
		t.Errorf("after 1 probe = %v, want 60s", got)
	}
	s.recordProbeAttempt()
	if got := s.probeBackoffDuration(); got != 120*time.Second {
		t.Errorf("after 2 probes = %v, want 120s", got)
	}
	s.recordProbeAttempt()
	if got := s.probeBackoffDuration(); got != 240*time.Second {
		t.Errorf("after 3 probes = %v, want 240s", got)
	}
	s.recordProbeAttempt()
	if got := s.probeBackoffDuration(); got != 300*time.Second {
		t.Errorf("after 4 probes = %v, want 300s (capped)", got)
	}
}

func TestServerHealth_ProbeBackoff_ResetsOnSuccess(t *testing.T) {
	s := &serverHealth{addr: "a:53"}
	for i := 0; i < 3; i++ {
		s.recordFailure()
	}
	s.recordProbeAttempt()
	s.recordProbeAttempt()
	s.recordProbeAttempt()
	s.recordSuccess(10_000_000)
	if got := s.probeBackoffDuration(); got != 30*time.Second {
		t.Errorf("after success = %v, want 30s (reset)", got)
	}
}

func TestPick_DegradedProbeRespectsBackoff(t *testing.T) {
	r := newRacingResolver(
		[]string{"a:53", "b:53"},
		nil, 2, 5*time.Second, "test",
	)
	// a degraded, probed once (backoff=60s), probed 35s ago → NOT due
	r.servers[0].consecutiveFail.Store(3)
	r.servers[0].probeAttempts.Store(1) // backoff = 60s
	r.servers[0].lastProbe.Store(time.Now().Add(-35 * time.Second).UnixNano())
	r.servers[1].latencyNs.Store(10_000_000)

	selected := r.pick()
	if len(selected) != 1 {
		t.Fatalf("len = %d, want 1 (a should not be probed yet)", len(selected))
	}
}

func TestPick_DegradedProbeDueWithBackoff(t *testing.T) {
	r := newRacingResolver(
		[]string{"a:53", "b:53", "c:53"},
		nil, 2, 5*time.Second, "test",
	)
	// a degraded, probed once (backoff=60s), probed 65s ago → due
	r.servers[0].consecutiveFail.Store(3)
	r.servers[0].probeAttempts.Store(1) // backoff = 60s
	r.servers[0].lastProbe.Store(time.Now().Add(-65 * time.Second).UnixNano())
	r.servers[1].latencyNs.Store(10_000_000)
	r.servers[2].latencyNs.Store(20_000_000)

	selected := r.pick()
	// 2 healthy (b, c) ≥ raceN → a NOT probed (probe only when healthy < raceN)
	if len(selected) != 2 {
		t.Fatalf("len = %d, want 2", len(selected))
	}
}

func TestRacingResolver_ConcurrentSafety(t *testing.T) {
	var calls atomic.Int32
	fn := func(ctx context.Context, server string, q *dns.Msg) (*dns.Msg, error) {
		calls.Add(1)
		time.Sleep(time.Duration(calls.Load()) * time.Millisecond)
		return buildTestResponse(q), nil
	}
	r := newRacingResolver([]string{"a:53", "b:53", "c:53"}, fn, 2, 5*time.Second, "test")

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = r.Query(context.Background(), buildTestQuery())
		}()
	}
	wg.Wait()
}

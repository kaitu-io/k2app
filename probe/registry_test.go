package probe

import (
	"testing"
	"time"

	"github.com/kaitu-io/k2/wire"
)

func mkStats(rtt float64, loss float64, supported bool) wire.ProbeStats {
	return wire.ProbeStats{
		Reachable:     true,
		EchoSupported: supported,
		AvgRttMs:      rtt,
		LossRate:      loss,
		JitterMs:      10,
		MeasuredAt:    time.Now(),
	}
}

func TestRegistry_RecordAndScore(t *testing.T) {
	r := NewRegistry(15 * time.Minute)
	url := "k2v5://u:t@host:443"
	r.Record(url, mkStats(50, 0, true))
	score, ok := r.Score(url)
	if !ok {
		t.Fatal("Score: ok=false, want true")
	}
	if score <= 0 || score > 1 {
		t.Errorf("score = %v, want (0,1]", score)
	}
}

func TestRegistry_ScoreMissing(t *testing.T) {
	r := NewRegistry(15 * time.Minute)
	if _, ok := r.Score("k2v5://never"); ok {
		t.Error("missing url should return ok=false")
	}
}

func TestRegistry_TTLExpiry(t *testing.T) {
	r := NewRegistry(50 * time.Millisecond)
	url := "k2v5://u:t@host:443"
	r.Record(url, mkStats(50, 0, true))
	time.Sleep(80 * time.Millisecond)
	if _, ok := r.Score(url); ok {
		t.Error("expired record should return ok=false")
	}
}

func TestRegistry_EchoUnsupportedSentinel(t *testing.T) {
	r := NewRegistry(15 * time.Minute)
	url := "k2v5://u:t@host:443"
	r.Record(url, mkStats(0, 0, false))
	score, ok := r.Score(url)
	if !ok {
		t.Fatal("ok=false for unsupported — should still have record")
	}
	if score != -1 {
		t.Errorf("score = %v, want -1 sentinel", score)
	}
}

func TestRegistry_SingleUnreachable_TreatedAsNoData(t *testing.T) {
	// One zero-scoring probe is treated as "not yet confirmed unreachable".
	// Score returns ok=false so Pick falls back to neutral weighting.
	// This prevents a single network blip from black-holing a healthy server.
	r := NewRegistry(15 * time.Minute)
	url := "k2v5://u:t@host:443"
	r.Record(url, wire.ProbeStats{Reachable: false, MeasuredAt: time.Now()})
	if _, ok := r.Score(url); ok {
		t.Error("single zero should return ok=false (treat as no-data)")
	}
}

func TestRegistry_TwoConsecutiveZeros_HardExcluded(t *testing.T) {
	// Two consecutive zero probes confirm unreachability. Score now returns
	// (0, true) so Pick hard-excludes.
	r := NewRegistry(15 * time.Minute)
	url := "k2v5://u:t@host:443"
	r.Record(url, wire.ProbeStats{Reachable: false, MeasuredAt: time.Now()})
	r.Record(url, wire.ProbeStats{Reachable: false, MeasuredAt: time.Now()})
	score, ok := r.Score(url)
	if !ok {
		t.Fatal("two consecutive zeros should be confirmed (ok=true)")
	}
	if score != 0 {
		t.Errorf("score = %v, want 0", score)
	}
}

func TestRegistry_NonZeroResetsConsecutiveCount(t *testing.T) {
	// A healthy probe between zeros resets the counter. The next zero is
	// again treated as a single-flake (ok=false), not hard exclusion.
	r := NewRegistry(15 * time.Minute)
	url := "k2v5://u:t@host:443"
	r.Record(url, wire.ProbeStats{Reachable: false, MeasuredAt: time.Now()})
	r.Record(url, mkStats(40, 0, true))
	r.Record(url, wire.ProbeStats{Reachable: false, MeasuredAt: time.Now()})
	if _, ok := r.Score(url); ok {
		t.Error("counter not reset after healthy probe: got ok=true, want ok=false")
	}
}

func TestRegistry_Snapshot(t *testing.T) {
	r := NewRegistry(15 * time.Minute)
	r.Record("k2v5://a", mkStats(50, 0, true))
	r.Record("k2v5://b", wire.ProbeStats{Reachable: false, MeasuredAt: time.Now()})
	snap := r.Snapshot()
	if len(snap) != 2 {
		t.Errorf("snapshot len = %d, want 2", len(snap))
	}
	if !snap["k2v5://a"].Reachable || snap["k2v5://b"].Reachable {
		t.Errorf("content mismatch: %+v", snap)
	}
}

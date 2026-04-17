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

func TestRegistry_UnreachableReturnsZero(t *testing.T) {
	r := NewRegistry(15 * time.Minute)
	url := "k2v5://u:t@host:443"
	r.Record(url, wire.ProbeStats{Reachable: false, MeasuredAt: time.Now()})
	score, ok := r.Score(url)
	if !ok {
		t.Fatal("ok=false for unreachable — record exists")
	}
	if score != 0 {
		t.Errorf("score = %v, want 0", score)
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

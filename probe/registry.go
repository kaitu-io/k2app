// Package probe stores candidate-node measurements and runs the background
// probe loop. Registry is consulted by Subscription.Pick (via ScoreSource)
// and by the daemon probe HTTP action (via Snapshot).
//
// Registry is NOT persisted: probe data is point-in-time network state and
// has no meaning across restarts.
package probe

import (
	"time"

	"github.com/kaitu-io/k2/wire"
	"github.com/sasha-s/go-deadlock"
)

// Registry stores per-URL probe results with TTL expiry. Safe for concurrent
// use. Score() is O(1) — hot path (Subscription.Pick calls per selection).
//
// Flake tolerance: a single zero-scoring probe is reported as ok=false
// (no-data, neutral weighting) rather than ok=true,score=0 (hard exclude).
// Only after `zeroThreshold` consecutive zero probes does Score confirm the
// hard-exclusion. Rationale: the probe window is short (8 rounds × 50ms),
// so a momentary WiFi hiccup can produce false zeros. Sustained failure
// over two probe cycles (10 minutes at default cadence) is required before
// blacklisting.
type Registry struct {
	mu            deadlock.Mutex
	ttl           time.Duration
	zeroThreshold int
	results       map[string]entry
}

type entry struct {
	stats            wire.ProbeStats
	score            float64
	consecutiveZeros int
}

// defaultZeroThreshold requires two consecutive zero probes before Score
// confirms hard exclusion. Tests may want a tighter threshold.
const defaultZeroThreshold = 2

func NewRegistry(ttl time.Duration) *Registry {
	return &Registry{
		ttl:           ttl,
		zeroThreshold: defaultZeroThreshold,
		results:       make(map[string]entry),
	}
}

// Record stores stats for url and caches computed score. When score == 0
// (unreachable) the per-URL consecutive-zeros counter increments; any
// non-zero score resets it. The counter gates when Score reports a confirmed
// hard exclusion vs. a tolerated single-flake.
func (r *Registry) Record(url string, stats wire.ProbeStats) {
	score := stats.Score()
	r.mu.Lock()
	prev := r.results[url]
	newEntry := entry{stats: stats, score: score}
	if score == 0 {
		newEntry.consecutiveZeros = prev.consecutiveZeros + 1
	}
	r.results[url] = newEntry
	r.mu.Unlock()
}

// Score implements config.ScoreSource (same signature).
// Returns:
//
//	ok=false          no record / expired / unconfirmed zero (single flake)
//	score=-1, ok=true reachable but echo unsupported (sentinel)
//	score=0,  ok=true confirmed unreachable (≥ zeroThreshold consecutive zeros)
//	score=(0,1]       normal quality
//
// Note: first zero-scoring probe returns ok=false, matching the "no data"
// semantics so Pick falls back to neutral weighting. Second consecutive
// zero confirms exclusion.
func (r *Registry) Score(url string) (float64, bool) {
	r.mu.Lock()
	e, ok := r.results[url]
	thr := r.zeroThreshold
	r.mu.Unlock()
	if !ok {
		return 0, false
	}
	if time.Since(e.stats.MeasuredAt) > r.ttl {
		return 0, false
	}
	if e.score == 0 && e.consecutiveZeros < thr {
		return 0, false
	}
	return e.score, true
}

// Snapshot returns a shallow copy of current results. For daemon probe
// action response assembly; do not use on hot paths.
func (r *Registry) Snapshot() map[string]wire.ProbeStats {
	r.mu.Lock()
	out := make(map[string]wire.ProbeStats, len(r.results))
	for k, v := range r.results {
		out[k] = v.stats
	}
	r.mu.Unlock()
	return out
}

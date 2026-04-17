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
type Registry struct {
	mu      deadlock.Mutex
	ttl     time.Duration
	results map[string]entry
}

type entry struct {
	stats wire.ProbeStats
	score float64
}

func NewRegistry(ttl time.Duration) *Registry {
	return &Registry{
		ttl:     ttl,
		results: make(map[string]entry),
	}
}

// Record stores stats for url and caches computed score.
func (r *Registry) Record(url string, stats wire.ProbeStats) {
	e := entry{stats: stats, score: stats.Score()}
	r.mu.Lock()
	r.results[url] = e
	r.mu.Unlock()
}

// Score implements config.ScoreSource (same signature).
// Returns:
//
//	ok=false        no record / expired
//	score=-1, ok=true  reachable but echo unsupported (sentinel)
//	score=0,  ok=true  unreachable
//	score=(0,1]       normal quality
func (r *Registry) Score(url string) (float64, bool) {
	r.mu.Lock()
	e, ok := r.results[url]
	r.mu.Unlock()
	if !ok {
		return 0, false
	}
	if time.Since(e.stats.MeasuredAt) > r.ttl {
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

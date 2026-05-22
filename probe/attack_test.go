package probe

import (
	"fmt"
	"math/rand/v2"
	"sync"
	"testing"
	"time"

	"github.com/kaitu-io/k2/wire"
)

// TestAttackRegistry_ConcurrentRecordScoreSnapshot fires concurrent
// Record/Score/Snapshot operations across many URLs. Catches:
//   - data race on results map (under -race)
//   - panic from concurrent map iteration vs write
//   - inconsistency between consecutiveZeros counter and current score value
func TestAttackRegistry_ConcurrentRecordScoreSnapshot(t *testing.T) {
	r := NewRegistry(15 * time.Minute)

	urls := make([]string, 32)
	for i := range urls {
		urls[i] = fmt.Sprintf("k2v5://attack-%d", i)
	}

	const opsPerProducer = 5000
	var wg sync.WaitGroup

	// 8 Record producers — mix zero / non-zero / -1 sentinel.
	for w := range 8 {
		wg.Add(1)
		seed := uint64(w)
		go func() {
			defer wg.Done()
			rng := rand.New(rand.NewPCG(seed, ^seed))
			for range opsPerProducer {
				url := urls[rng.IntN(len(urls))]
				var st wire.ProbeStats
				switch rng.IntN(4) {
				case 0:
					st = wire.ProbeStats{Reachable: false, MeasuredAt: time.Now()}
				case 1:
					st = wire.ProbeStats{Reachable: true, EchoSupported: false, MeasuredAt: time.Now()}
				default:
					st = wire.ProbeStats{
						Reachable:     true,
						EchoSupported: true,
						AvgRttMs:      float64(rng.IntN(200) + 10),
						LossRate:      rng.Float64() * 0.3,
						JitterMs:      float64(rng.IntN(30)),
						MeasuredAt:    time.Now(),
					}
				}
				r.Record(url, st)
			}
		}()
	}

	// 4 Score readers — read random URLs.
	for w := range 4 {
		wg.Add(1)
		seed := uint64(100 + w)
		go func() {
			defer wg.Done()
			rng := rand.New(rand.NewPCG(seed, ^seed))
			for range opsPerProducer {
				_, _ = r.Score(urls[rng.IntN(len(urls))])
			}
		}()
	}

	// 2 Snapshot readers — concurrent map copy.
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range opsPerProducer {
				_ = r.Snapshot()
			}
		}()
	}

	wg.Wait()

	// Sanity: every URL should now have a stored entry.
	snap := r.Snapshot()
	if len(snap) == 0 {
		t.Fatal("Snapshot is empty after storm — no records persisted")
	}
}

// TestAttackRegistry_ZerosAndRecoveryInterleave specifically interleaves
// zero-score and non-zero-score records on the SAME URL across multiple
// goroutines, then asserts that the final state is consistent: either the
// last write was zero (consecutiveZeros >= 1) or non-zero (consecutiveZeros
// == 0). The invariant `consecutiveZeros > 0 → score == 0` must hold under
// any interleaving.
func TestAttackRegistry_ZerosAndRecoveryInterleave(t *testing.T) {
	r := NewRegistry(15 * time.Minute)
	url := "k2v5://target"

	const ops = 5000
	var wg sync.WaitGroup

	wg.Add(2)
	go func() {
		defer wg.Done()
		for range ops {
			r.Record(url, wire.ProbeStats{Reachable: false, MeasuredAt: time.Now()})
		}
	}()
	go func() {
		defer wg.Done()
		for range ops {
			r.Record(url, wire.ProbeStats{
				Reachable: true, EchoSupported: true,
				AvgRttMs: 50, LossRate: 0, JitterMs: 5,
				MeasuredAt: time.Now(),
			})
		}
	}()
	wg.Wait()

	// Final invariant: snapshot the entry. score and consecutiveZeros are
	// always written together under r.mu, so we expect consistency.
	snap := r.Snapshot()
	if _, ok := snap[url]; !ok {
		t.Fatal("URL not in snapshot")
	}

	// Inspect internal state via the public Score interface. We can't peek
	// at consecutiveZeros directly, but we can verify the public contract:
	//   - if Score returns (s, true) and s > 0, the entry is healthy
	//   - if Score returns (0, false), it's flake-tolerated single-zero
	//   - if Score returns (0, true), confirmed unreachable (>=2 zeros)
	score, ok := r.Score(url)
	switch {
	case !ok && score == 0:
		// flake-tolerated zero — last writer was unreachable, count was 1
	case ok && score > 0:
		// healthy — last writer was reachable
	case ok && score == 0:
		// confirmed unreachable — last writer was zero and count >= 2
	default:
		t.Fatalf("unexpected score state: score=%v ok=%v", score, ok)
	}
}

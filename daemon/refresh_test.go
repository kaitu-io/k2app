package daemon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kaitu-io/k2/rule"
)

// TestRuleRefreshLoop_TicksOnInterval verifies the loop fires the refresh
// action once immediately (cold-start parity with the old one-shot prefetch)
// and then once per interval, and stops cleanly on context cancel.
//
// We assert on the *injected* refreshAction so the test never hits the
// network — rule.EnsureBundles' own behavior is covered in rule/.
func TestRuleRefreshLoop_TicksOnInterval(t *testing.T) {
	var ticks atomic.Int32
	d := &Daemon{
		ruleCacheDir:  t.TempDir(),
		refreshAction: func(ctx context.Context, dir string) { ticks.Add(1) },
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 20ms tick — comfortable to count ≥3 fires (initial + 2 timed) in <100ms.
	go d.runRuleRefreshLoop(ctx, 20*time.Millisecond)

	deadline := time.After(500 * time.Millisecond)
	for ticks.Load() < 3 {
		select {
		case <-deadline:
			t.Fatalf("expected ≥3 ticks within 500ms, got %d", ticks.Load())
		case <-time.After(5 * time.Millisecond):
		}
	}

	cancel()

	// After cancel, ticks should plateau within one interval (allow generous
	// 100ms margin for the scheduler to drain the in-flight select).
	before := ticks.Load()
	time.Sleep(100 * time.Millisecond)
	after := ticks.Load()
	if after-before > 1 {
		t.Errorf("ticks kept growing after cancel: before=%d after=%d (want delta ≤ 1)", before, after)
	}
}

// TestRuleRefreshLoop_FiresImmediately verifies the cold-start invariant —
// the very first action runs synchronously before the first ticker wait, so
// boot-time prefetch behavior is preserved.
func TestRuleRefreshLoop_FiresImmediately(t *testing.T) {
	fired := make(chan struct{}, 1)
	d := &Daemon{
		ruleCacheDir: t.TempDir(),
		refreshAction: func(ctx context.Context, dir string) {
			select {
			case fired <- struct{}{}:
			default:
			}
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Use an interval far longer than the test budget so any tick *must* be
	// the immediate fire, not a periodic tick.
	go d.runRuleRefreshLoop(ctx, 1*time.Hour)

	select {
	case <-fired:
		// good
	case <-time.After(100 * time.Millisecond):
		t.Fatal("refreshAction did not fire within 100ms of loop start")
	}
}

// A cold cache must be seeded from the embedded archive BEFORE the networked
// refresh starts. EnsureBundles holds .ensure.lock for the whole download (up
// to 30s), and the connect path's SeedFromEmbedIfEmpty waits at most 10s for
// that lock — so a connect right after daemon start on a fresh install, with a
// slow rule source, failed with 504 "no bundle on disk after embedded seed"
// (reproduced on testlab Win11, 0.4.11 and 0.4.12). appext.PrefetchRules
// already seeds first; this pins the daemon to the same order. A cancelled ctx
// makes the download fail immediately, so only the seed can leave bundles.
func TestDoRefreshRules_SeedsColdCacheBeforeDownload(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	d := &Daemon{}
	d.doRefreshRules(ctx, dir)

	if n := len(rule.ListRegions(dir)); n == 0 {
		t.Fatal("cold cache still empty after doRefreshRules: embedded seed did not run before the download")
	}
}

// TestDoRefreshRules_ColdStartRefreshDoesNotBlockConnect replays the desktop
// cold-cache 504 deterministically: the rule source accepts the request and
// never answers, so a download that starts on an empty cache holds
// .ensure.lock for its whole 30s budget. The connect path
// (engine.prefetchRulesForStart = SeedFromEmbedIfEmpty + ListRegions) runs as
// soon as the refresh has either seeded the cache or reached the network, and
// must come back within a second with bundles on disk. Before the fix the
// refresh went straight to the download, the connect path's seed waited 10s
// for the lock and gave up, and Start failed with 504.
func TestDoRefreshRules_ColdStartRefreshDoesNotBlockConnect(t *testing.T) {
	dir := t.TempDir()

	hit := make(chan struct{}, 1)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case hit <- struct{}{}:
		default:
		}
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() { close(release); srv.Close() })

	orig := rule.DefaultSources
	rule.DefaultSources = []string{srv.URL + "/"}
	t.Cleanup(func() { rule.DefaultSources = orig })

	ctx, cancel := context.WithCancel(context.Background())
	refreshDone := make(chan struct{})
	go func() {
		defer close(refreshDone)
		(&Daemon{}).doRefreshRules(ctx, dir)
	}()
	t.Cleanup(func() { cancel(); <-refreshDone }) // runs before the server cleanup above

	deadline := time.After(5 * time.Second)
waitRefresh:
	for len(rule.ListRegions(dir)) == 0 {
		select {
		case <-hit:
			break waitRefresh // download in flight, holding .ensure.lock
		case <-deadline:
			t.Fatal("refresh neither seeded the cache nor reached the rule source within 5s")
		case <-time.After(5 * time.Millisecond):
		}
	}

	start := time.Now()
	seedErr := rule.SeedFromEmbedIfEmpty(dir)
	n := len(rule.ListRegions(dir))
	if took := time.Since(start); n == 0 || took > time.Second {
		t.Fatalf("connect path during cold-start refresh: %d regions after %v (seed err: %v); want bundles within 1s", n, took, seedErr)
	}
}

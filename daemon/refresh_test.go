package daemon

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
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

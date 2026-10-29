package safego

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestGo_PanicRecovery(t *testing.T) {
	done := make(chan struct{})
	Go(func() {
		defer close(done)
		panic("test panic")
	})
	select {
	case <-done:
		// Goroutine completed without crashing the process.
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for panicking goroutine")
	}
}

func TestGo_NormalExecution(t *testing.T) {
	var called atomic.Bool
	done := make(chan struct{})
	Go(func() {
		defer close(done)
		called.Store(true)
	})
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for goroutine")
	}
	if !called.Load() {
		t.Fatal("function was not called")
	}
}

func TestLoop_PanicRestart(t *testing.T) {
	var calls atomic.Int32
	done := make(chan struct{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	Loop(ctx, "test-restart", func(ctx context.Context) {
		n := calls.Add(1)
		if n < 3 {
			panic("deliberate panic")
		}
		// Third call: return normally to stop the loop.
		close(done)
	})

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for loop to finish")
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("expected 3 calls, got %d", got)
	}
}

func TestLoop_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	var runs atomic.Int32
	var stopped sync.WaitGroup
	stopped.Add(1)

	// Override Loop's goroutine detection: we track when the outer
	// goroutine exits by wrapping fn to block until cancelled.
	Loop(ctx, "test-cancel", func(ctx context.Context) {
		runs.Add(1)
		defer stopped.Done()
		<-ctx.Done()
	})

	// Give the goroutine time to start.
	time.Sleep(50 * time.Millisecond)
	cancel()

	stopped.Wait()
	if got := runs.Load(); got != 1 {
		t.Fatalf("expected 1 run, got %d", got)
	}
}

func TestLoop_NormalReturnStops(t *testing.T) {
	var calls atomic.Int32

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	Loop(ctx, "test-normal", func(ctx context.Context) {
		calls.Add(1)
		// Return normally — loop must NOT restart.
	})

	// Wait enough time that a restart (1s backoff) would have happened.
	time.Sleep(2 * time.Second)
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected exactly 1 call (no restart), got %d", got)
	}
}

package engine

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// mockProbeDialer implements probeDialer for testing.
type mockProbeDialer struct {
	mu            sync.Mutex
	handshakeFunc func(ctx context.Context) error
	calls         int32 // atomic for race-safe reads
}

func (m *mockProbeDialer) Handshake(ctx context.Context) error {
	atomic.AddInt32(&m.calls, 1)
	m.mu.Lock()
	fn := m.handshakeFunc
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx)
	}
	return nil // Default: success
}

func (m *mockProbeDialer) getCalls() int {
	return int(atomic.LoadInt32(&m.calls))
}

func (m *mockProbeDialer) setHandshakeFunc(fn func(ctx context.Context) error) {
	m.mu.Lock()
	m.handshakeFunc = fn
	m.mu.Unlock()
}

// newTestProbe creates a recovery probe with short timings for testing.
// Returns the probe with probeInitialDelay overridden via a wrapper.
func newConnectedEngine() (*Engine, *mockEventHandler) {
	e := New()
	h := &mockEventHandler{}
	e.SetEventHandler(h)
	e.mu.Lock()
	e.state = StateConnected
	e.connectedAt = time.Now()
	e.mu.Unlock()
	return e, h
}

func TestRecoveryProbe_Success(t *testing.T) {
	e, h := newConnectedEngine()
	dialer := &mockProbeDialer{}

	// Set a wire error that should be cleared on success.
	e.mu.Lock()
	e.lastError = &EngineError{Code: 503, Category: CategoryServer, Message: "refused"}
	e.mu.Unlock()

	probe := newRecoveryProbe(dialer, e)
	probe.trigger()

	// Wait for probe to complete (initial delay + dial).
	time.Sleep(probeInitialDelay + time.Second)

	if dialer.getCalls() != 1 {
		t.Fatalf("expected 1 dial call, got %d", dialer.getCalls())
	}

	// Wire error should be cleared.
	e.mu.Lock()
	hasErr := e.lastError != nil
	e.mu.Unlock()
	if hasErr {
		t.Fatal("expected wire error to be cleared after successful probe")
	}

	// Handler should have received clear status.
	statuses := h.getStatuses()
	if len(statuses) == 0 {
		t.Fatal("expected at least one status update")
	}
	last := statuses[len(statuses)-1]
	if last.Error != nil {
		t.Errorf("expected nil error in last status, got %+v", last.Error)
	}

	probe.stop()
}

func TestRecoveryProbe_FailsThenRetries(t *testing.T) {
	e, _ := newConnectedEngine()
	dialer := &mockProbeDialer{}
	dialer.setHandshakeFunc(func(ctx context.Context) error {
		return fmt.Errorf("wire: TCP dial: connection refused")
	})

	probe := newRecoveryProbe(dialer, e)
	probe.trigger()

	// Wait for initial delay + all 3 attempts with retry intervals.
	// Attempt 1 at +3s, attempt 2 at +13s, attempt 3 at +23s.
	time.Sleep(probeInitialDelay + 2*probeRetryInterval + 3*time.Second)

	calls := dialer.getCalls()
	if calls != probeMaxAttempts {
		t.Fatalf("expected %d dial calls, got %d", probeMaxAttempts, calls)
	}

	// Engine should have a wire error reported.
	e.mu.Lock()
	hasErr := e.lastError != nil
	e.mu.Unlock()
	if !hasErr {
		t.Fatal("expected wire error after failed probes")
	}

	probe.stop()
}

func TestRecoveryProbe_SkipsOnClientError(t *testing.T) {
	e, _ := newConnectedEngine()
	dialer := &mockProbeDialer{}

	// Set a client error (401).
	e.mu.Lock()
	e.lastError = &EngineError{Code: 401, Category: CategoryClient, Message: "token expired"}
	e.mu.Unlock()

	probe := newRecoveryProbe(dialer, e)
	probe.trigger()

	time.Sleep(probeInitialDelay + time.Second)

	if dialer.getCalls() != 0 {
		t.Fatalf("expected 0 dial calls with client error, got %d", dialer.getCalls())
	}

	probe.stop()
}

func TestRecoveryProbe_SkipsWhenPaused(t *testing.T) {
	e, _ := newConnectedEngine()
	dialer := &mockProbeDialer{}

	e.mu.Lock()
	e.paused = true
	e.mu.Unlock()

	probe := newRecoveryProbe(dialer, e)
	probe.trigger()

	time.Sleep(probeInitialDelay + time.Second)

	if dialer.getCalls() != 0 {
		t.Fatalf("expected 0 dial calls when paused, got %d", dialer.getCalls())
	}

	probe.stop()
}

func TestRecoveryProbe_SkipsWhenDisconnected(t *testing.T) {
	e, _ := newConnectedEngine()
	dialer := &mockProbeDialer{}

	e.mu.Lock()
	e.state = StateDisconnected
	e.mu.Unlock()

	probe := newRecoveryProbe(dialer, e)
	probe.trigger()

	time.Sleep(probeInitialDelay + time.Second)

	if dialer.getCalls() != 0 {
		t.Fatalf("expected 0 dial calls when disconnected, got %d", dialer.getCalls())
	}

	probe.stop()
}

func TestRecoveryProbe_NewTriggerCancelsOld(t *testing.T) {
	e, _ := newConnectedEngine()
	var callCount atomic.Int32
	dialer := &mockProbeDialer{}
	dialer.setHandshakeFunc(func(ctx context.Context) error {
		callCount.Add(1)
		return nil
	})

	probe := newRecoveryProbe(dialer, e)

	// Trigger twice rapidly — first should be cancelled.
	probe.trigger()
	time.Sleep(100 * time.Millisecond) // let first goroutine start
	probe.trigger()

	// Wait for the second probe to complete.
	time.Sleep(probeInitialDelay + time.Second)

	// Should see exactly 1 dial (second trigger), not 2.
	calls := int(callCount.Load())
	if calls != 1 {
		t.Fatalf("expected 1 dial call (old cancelled), got %d", calls)
	}

	probe.stop()
}

func TestRecoveryProbe_StopCancelsPending(t *testing.T) {
	e, _ := newConnectedEngine()
	dialer := &mockProbeDialer{}

	probe := newRecoveryProbe(dialer, e)
	probe.trigger()

	// Stop before initial delay expires.
	time.Sleep(500 * time.Millisecond)
	probe.stop()

	// Wait past the initial delay.
	time.Sleep(probeInitialDelay)

	if dialer.getCalls() != 0 {
		t.Fatalf("expected 0 dial calls after stop, got %d", dialer.getCalls())
	}
}

func TestRecoveryProbe_StopsOnClientErrorFromServer(t *testing.T) {
	e, _ := newConnectedEngine()
	dialer := &mockProbeDialer{}
	callNum := atomic.Int32{}
	dialer.setHandshakeFunc(func(ctx context.Context) error {
		n := callNum.Add(1)
		if n == 1 {
			// First attempt: server rejects auth.
			return fmt.Errorf("wire: stream rejected")
		}
		// Should never reach here.
		return nil
	})

	probe := newRecoveryProbe(dialer, e)
	probe.trigger()

	// Wait for initial delay + first attempt + some buffer.
	time.Sleep(probeInitialDelay + 2*time.Second)

	// Should only have 1 call — stopped after client error classification.
	if int(callNum.Load()) != 1 {
		t.Fatalf("expected 1 dial call (stop on auth error), got %d", callNum.Load())
	}

	probe.stop()
}

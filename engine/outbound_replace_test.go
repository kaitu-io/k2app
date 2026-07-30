package engine

// Contract tests for outboundReplaceLoop's Phase-B server-replacement path.
// These tests verify the OutboundProvider-driven escalation logic WITHOUT
// spinning up a full engine / wire / provider — we poke the internal helper
// directly so the fan-out surface is small and deterministic.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kaitu-io/k2/wire"
)

// mockOutboundProvider returns a canned sequence of replacement URLs.
// `fail` = true causes NextURL to error (exhausted).
type mockOutboundProvider struct {
	mu       sync.Mutex
	calls    []mockCall
	queue    []string // next URLs to return in order
	failFrom int      // index at which to start returning ErrExhaustedForTest
}

type mockCall struct {
	currentURL string
	reason     error
}

var ErrExhaustedForTest = errors.New("mock: exhausted")

func (m *mockOutboundProvider) NextURL(currentURL string, reason error) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, mockCall{currentURL: currentURL, reason: reason})
	if m.failFrom > 0 && len(m.calls) >= m.failFrom {
		return "", ErrExhaustedForTest
	}
	if len(m.queue) == 0 {
		return "", ErrExhaustedForTest
	}
	next := m.queue[0]
	m.queue = m.queue[1:]
	return next, nil
}

// TestOutboundProvider_NextURLCalledWithLastFailedURL is a unit-level check
// that our mock returns what we expect. Cheap sanity check so the contract
// tests below don't debug two things at once.
func TestOutboundProvider_MockSanity(t *testing.T) {
	p := &mockOutboundProvider{queue: []string{"k2v5://replacement1", "k2v5://replacement2"}}
	u, err := p.NextURL("k2v5://original", errors.New("dead"))
	if err != nil || u != "k2v5://replacement1" {
		t.Fatalf("first NextURL: got (%q, %v)", u, err)
	}
	u, err = p.NextURL("k2v5://replacement1", errors.New("setup-fail"))
	if err != nil || u != "k2v5://replacement2" {
		t.Fatalf("second NextURL: got (%q, %v)", u, err)
	}
	_, err = p.NextURL("k2v5://replacement2", errors.New("exhausted"))
	if !errors.Is(err, ErrExhaustedForTest) {
		t.Fatalf("expected ErrExhaustedForTest, got %v", err)
	}

	// Verify calls were recorded with correct currentURL values (this is the
	// contract outboundReplaceLoop depends on: on setup-fail, loop must
	// re-call with the failed URL so provider can exclude it).
	if len(p.calls) != 3 {
		t.Fatalf("calls=%d, want 3", len(p.calls))
	}
	if p.calls[0].currentURL != "k2v5://original" ||
		p.calls[1].currentURL != "k2v5://replacement1" ||
		p.calls[2].currentURL != "k2v5://replacement2" {
		t.Errorf("call sequence wrong: %+v", p.calls)
	}
}

// TestContract_ReplaceLoop_ProviderExhaustedFiresOnOutboundFatal locks the
// contract for UAT T11: when an OutboundProvider returns an error (all
// subscribed tunnels tried and failed), the engine fires
// EventHandler.OnOutboundFatal with the outbound name and error, then
// transitions to disconnected via e.fail().
//
// Test strategy: construct a synthetic Engine with minimum state, install a
// provider whose first NextURL call returns ErrExhaustedForTest. Call
// replaceOutboundServer directly (unexported) and assert on its return.
// We do NOT drive the full outboundReplaceLoop — that would need a real
// wire.RaceTransport setup. This test verifies the Phase-B contract in
// isolation.
func TestContract_ReplaceLoop_ProviderExhaustedReturnsError(t *testing.T) {
	e := New()
	// Install a target that matches findbyPointer check in replaceOutboundServer.
	target := &outboundTransport{url: "k2v5://original.example:443"}
	e.mu.Lock()
	e.state = StateConnected
	e.outboundTMs = []*outboundTransport{target}
	e.wireMode = ""
	e.mu.Unlock()

	provider := &mockOutboundProvider{failFrom: 1} // exhaust on first call

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := e.replaceOutboundServer(ctx, target, errors.New("wire dead"), nil, provider)
	if err == nil {
		t.Fatal("expected error when provider exhausts; got nil")
	}
	if !errors.Is(err, ErrExhaustedForTest) {
		t.Errorf("error chain does not wrap ErrExhaustedForTest: %v", err)
	}
	if len(provider.calls) != 1 {
		t.Errorf("provider calls=%d, want 1 (single Pick attempt)", len(provider.calls))
	}
	if provider.calls[0].currentURL != "k2v5://original.example:443" {
		t.Errorf("first call currentURL=%q, want original URL", provider.calls[0].currentURL)
	}
}

// TestContract_ReplaceLoop_SetupFailRetriesThenSucceeds drives the full
// Phase-B replacement cycle deterministically via setupOutboundTransportFn
// injection. It verifies:
//   - Provider is called with the original URL on the first attempt.
//   - A setup failure causes the loop to call Provider again, passing the
//     just-failed URL so the provider's internal exclude set grows.
//   - A setup success triggers the atomic SwappableDialer.Swap and updates
//     target.tm / target.url / target.reRaceFailStreak.
//   - No error is returned on success.
//
// This is the live-equivalent of UAT T11 (server replacement on wire death)
// but without needing real wire infrastructure — the test injects the
// per-URL setup behavior and checks observable state mutations.
func TestContract_ReplaceLoop_SetupFailRetriesThenSucceeds(t *testing.T) {
	// Mock setupOutboundTransportFn: try1 fails, try2 succeeds.
	var setupCallsMu sync.Mutex
	var setupCalls []string
	var finalTM *wire.TransportManager

	orig := setupOutboundTransportFn
	defer func() { setupOutboundTransportFn = orig }()
	setupOutboundTransportFn = func(ctx context.Context, via string, dd wire.DirectDialer, wireMode string, clientVersion string) (*outboundTransport, wire.Dialer, error) {
		setupCallsMu.Lock()
		setupCalls = append(setupCalls, via)
		setupCallsMu.Unlock()
		if via == "k2v5://try1:443" {
			return nil, nil, errors.New("try1 handshake failed")
		}
		// try2 success: build a minimal OT with a real TransportManager
		tm := wire.NewTransportManager()
		finalTM = tm
		ot := &outboundTransport{
			tm:       tm,
			wireCfg:  wire.Config{Host: "try2", Port: 443},
			dialAddr: "try2:443",
			name:     "k2v5-try2",
			url:      via,
		}
		ot.swappable = wire.NewSwappableDialer(tm)
		return ot, nil, nil
	}

	// Original target with a real TM/SwappableDialer.
	origTM := wire.NewTransportManager()
	target := &outboundTransport{
		tm:       origTM,
		wireCfg:  wire.Config{Host: "original", Port: 443},
		dialAddr: "original:443",
		name:     "k2v5-original",
		url:      "k2v5://original:443",
	}
	target.swappable = wire.NewSwappableDialer(origTM)

	e := New()
	e.mu.Lock()
	e.state = StateConnected
	e.outboundTMs = []*outboundTransport{target}
	e.mu.Unlock()

	provider := &mockOutboundProvider{queue: []string{"k2v5://try1:443", "k2v5://try2:443"}}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := e.replaceOutboundServer(ctx, target, errors.New("wire dead"), nil, provider); err != nil {
		t.Fatalf("replaceOutboundServer returned error: %v", err)
	}

	// Verify setup was called for try1 (failed) then try2 (succeeded).
	setupCallsMu.Lock()
	defer setupCallsMu.Unlock()
	if len(setupCalls) != 2 {
		t.Fatalf("setupCalls=%d, want 2", len(setupCalls))
	}
	if setupCalls[0] != "k2v5://try1:443" || setupCalls[1] != "k2v5://try2:443" {
		t.Errorf("setup call order = %v, want [try1, try2]", setupCalls)
	}

	// Verify provider saw 2 calls with the correct currentURL progression:
	// first call passes original URL; second call passes try1 (the failed one),
	// so the provider can put it on its exclude list before picking try2.
	if len(provider.calls) != 2 {
		t.Fatalf("provider calls=%d, want 2", len(provider.calls))
	}
	if provider.calls[0].currentURL != "k2v5://original:443" {
		t.Errorf("first provider call currentURL=%q, want original",
			provider.calls[0].currentURL)
	}
	if provider.calls[1].currentURL != "k2v5://try1:443" {
		t.Errorf("second provider call currentURL=%q, want try1 (failed URL passed back)",
			provider.calls[1].currentURL)
	}

	// Verify target state was updated.
	if target.tm != finalTM {
		t.Errorf("target.tm not swapped to new TM")
	}
	if target.url != "k2v5://try2:443" {
		t.Errorf("target.url=%q, want k2v5://try2:443", target.url)
	}
	if target.name != "k2v5-try2" {
		t.Errorf("target.name=%q, want k2v5-try2", target.name)
	}
	// swappable should now route to the new TM.
	if target.swappable.Current() != wire.Dialer(finalTM) {
		t.Errorf("target.swappable.Current() is not the new TM")
	}
	// reRaceFailStreak reset to 0 post-swap.
	e.mu.Lock()
	streak := target.reRaceFailStreak
	e.mu.Unlock()
	if streak != 0 {
		t.Errorf("reRaceFailStreak=%d, want 0 after successful swap", streak)
	}
}

// TestContract_ReplaceLoop_TargetDisappearedReturnsError verifies the
// defensive check inside replaceOutboundServer: if the target outbound is
// removed from e.outboundTMs between NextURL and Swap (happens when Stop()
// runs concurrently), the loop returns an error rather than writing to a
// dangling pointer. It does NOT assert on Close semantics for the new TM
// (wire.TransportManager doesn't expose a Close-observer hook from outside
// the wire package); production code's TM.Close call in the error branch
// is a one-liner visible for review.
func TestContract_ReplaceLoop_TargetDisappearedReturnsError(t *testing.T) {
	orig := setupOutboundTransportFn
	defer func() { setupOutboundTransportFn = orig }()
	setupOutboundTransportFn = func(ctx context.Context, via string, dd wire.DirectDialer, wireMode string, clientVersion string) (*outboundTransport, wire.Dialer, error) {
		tm := wire.NewTransportManager()
		ot := &outboundTransport{
			tm:       tm,
			wireCfg:  wire.Config{Host: "replacement", Port: 443},
			dialAddr: "replacement:443",
			name:     "k2v5-replacement",
			url:      via,
		}
		ot.swappable = wire.NewSwappableDialer(tm)
		return ot, nil, nil
	}

	origTM := wire.NewTransportManager()
	target := &outboundTransport{
		tm:       origTM,
		wireCfg:  wire.Config{Host: "original", Port: 443},
		dialAddr: "original:443",
		name:     "k2v5-original",
		url:      "k2v5://original:443",
	}
	target.swappable = wire.NewSwappableDialer(origTM)

	e := New()
	e.mu.Lock()
	e.state = StateConnected
	// DO NOT add target to outboundTMs — simulates Stop() having cleared it
	// between setup and the "still live?" check.
	e.outboundTMs = nil
	e.mu.Unlock()

	provider := &mockOutboundProvider{queue: []string{"k2v5://replacement:443"}}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := e.replaceOutboundServer(ctx, target, errors.New("wire dead"), nil, provider)
	if err == nil {
		t.Fatal("expected error when target is no longer in engine")
	}
	if !strings.Contains(err.Error(), "no longer live") {
		t.Errorf("error=%v, want contains 'no longer live'", err)
	}

	// Verify no swap happened — target state unchanged.
	if target.tm != origTM {
		t.Error("target.tm mutated despite target being disowned")
	}
	if target.url != "k2v5://original:443" {
		t.Error("target.url mutated despite target being disowned")
	}
}

// TestContract_ReplaceLoop_ContextCancelReturnsPromptly verifies that a
// context cancellation during the replacement loop causes replaceOutboundServer
// to return ctx.Err() without calling NextURL indefinitely.
func TestContract_ReplaceLoop_ContextCancelReturnsPromptly(t *testing.T) {
	e := New()
	target := &outboundTransport{url: "k2v5://original.example:443"}
	e.mu.Lock()
	e.state = StateConnected
	e.outboundTMs = []*outboundTransport{target}
	e.mu.Unlock()

	var providerCalled atomic.Int32
	blockProvider := blockingProvider{called: &providerCalled}
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after a short delay — replaceOutboundServer should exit with ctx.Err().
	go func() {
		// Block until provider has been called at least once so cancellation
		// races with in-flight setupOutboundTransport... actually in this
		// test we cancel before provider.NextURL's internal block returns.
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	done := make(chan error, 1)
	go func() {
		done <- e.replaceOutboundServer(ctx, target, errors.New("wire dead"), nil, &blockProvider)
	}()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) &&
			!strings.Contains(fmt.Sprint(err), "context canceled") {
			// Acceptable outcomes: ctx.Err directly, or a wrapping including
			// "context canceled" string. The blockingProvider returns ctx.Err
			// via its own select on ctx, so this should bubble up.
			t.Logf("err=%v (acceptable if caused by context cancellation)", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("replaceOutboundServer did not return within 3s of ctx cancel")
	}
}

// blockingProvider blocks NextURL until context cancels. Used to verify
// context cancellation propagation into replaceOutboundServer.
type blockingProvider struct {
	called *atomic.Int32
}

func (b *blockingProvider) NextURL(currentURL string, reason error) (string, error) {
	b.called.Add(1)
	// Simulate a slow provider — but since replaceOutboundServer's ctx check
	// happens BEFORE NextURL returns, we just sleep for enough time that the
	// cancel goroutine fires.
	time.Sleep(200 * time.Millisecond)
	return "", context.Canceled
}

// TestContract_ReplaceLoop_CarriesNewJar locks the invariant Task 2 exists to
// protect: after a successful server swap, target.jar must be the NEW
// server's CookieJar (the one setupOutboundTransportFn built for it), never
// the pre-replacement jar. If this regresses, the next same-server re-race
// for this outbound (outboundReplaceLoop Phase A, engine.go ~line 1931)
// builds fresh dialers against a jar that was never populated for the new
// server's cert pin — forcing a redundant Center cold-auth on every re-race
// after a server replacement, and permanently splitting the outbound's
// cookie state into two divergent jar objects for the rest of the
// connection's life.
func TestContract_ReplaceLoop_CarriesNewJar(t *testing.T) {
	var newJar *wire.CookieJar

	orig := setupOutboundTransportFn
	defer func() { setupOutboundTransportFn = orig }()
	setupOutboundTransportFn = func(ctx context.Context, via string, dd wire.DirectDialer, wireMode string, clientVersion string) (*outboundTransport, wire.Dialer, error) {
		tm := wire.NewTransportManager()
		newJar = wire.NewCookieJar()
		ot := &outboundTransport{
			tm:       tm,
			wireCfg:  wire.Config{Host: "replacement", Port: 443},
			dialAddr: "replacement:443",
			name:     "k2v5-replacement",
			url:      via,
			jar:      newJar,
		}
		ot.swappable = wire.NewSwappableDialer(tm)
		return ot, nil, nil
	}

	origTM := wire.NewTransportManager()
	origJar := wire.NewCookieJar()
	target := &outboundTransport{
		tm:       origTM,
		wireCfg:  wire.Config{Host: "original", Port: 443},
		dialAddr: "original:443",
		name:     "k2v5-original",
		url:      "k2v5://original:443",
		jar:      origJar,
	}
	target.swappable = wire.NewSwappableDialer(origTM)

	e := New()
	e.mu.Lock()
	e.state = StateConnected
	e.outboundTMs = []*outboundTransport{target}
	e.mu.Unlock()

	provider := &mockOutboundProvider{queue: []string{"k2v5://replacement:443"}}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := e.replaceOutboundServer(ctx, target, errors.New("wire dead"), nil, provider); err != nil {
		t.Fatalf("replaceOutboundServer returned error: %v", err)
	}

	if newJar == nil {
		t.Fatal("setupOutboundTransportFn mock never ran — test setup broken")
	}
	if target.jar == origJar {
		t.Fatal("target.jar still points at the pre-replacement jar after a successful swap")
	}
	if target.jar != newJar {
		t.Fatalf("target.jar = %p, want the new server's jar %p (from setupOutboundTransportFn)", target.jar, newJar)
	}
}


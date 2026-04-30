package test

// E2E tests for k2subs:// server-switching scenarios.
//
// 22 subtests across 5 groups (A: Pick layer, B: Probe ↔ switch,
// C: Refresh ↔ switch, D: Lifecycle / concurrency, E: Real-user).
// Spec: docs/superpowers/specs/2026-04-30-k2subs-switch-e2e-design.md
//
// Each test uses the k2subsFixture from k2subs_switch_helpers_test.go and
// runs goleak.VerifyNone at end-of-test to guard against goroutine leaks.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/engine"
)

const (
	tunA = "k2v5://srv-a.example:443"
	tunB = "k2v5://srv-b.example:443"
	tunC = "k2v5://srv-c.example:443"
	tunD = "k2v5://srv-d.example:443"
	tunE = "k2v5://srv-e.example:443"
)

func twoTunnels() []config.Tunnel {
	return []config.Tunnel{
		{URL: tunA, RecommendScore: 0.5},
		{URL: tunB, RecommendScore: 0.5},
	}
}

// ---------------------------------------------------------------------------
// Group A — Pick layer in switch context
// ---------------------------------------------------------------------------

// A2 — single-switch happy path. Provider returns a different URL,
// setupOutboundTransportFn succeeds, swappable swaps, fail streak resets.
func TestK2SubsSwitch_A2_NextURLSwapsDialer(t *testing.T) {
	f := newK2subsFixture(t, twoTunnels())
	current := f.Resolve(context.Background())

	// Confirm Resolve returned a credentialed version of A or B.
	if !strings.Contains(current, "alice:tok@") {
		t.Fatalf("resolved URL missing creds: %q", current)
	}

	if err := f.TriggerSwitch(context.Background(), errors.New("simulated wire fail")); err != nil {
		t.Fatalf("TriggerSwitch: %v", err)
	}

	// The new URL must differ from `current`.
	newURL := f.Target.URL()
	if newURL == current {
		t.Fatalf("swap did not change URL: still %q", newURL)
	}
	if !strings.Contains(newURL, "alice:tok@") {
		t.Fatalf("post-swap URL missing creds: %q", newURL)
	}

	// reRaceFailStreak resets to 0 after a successful swap.
	if streak := engine.ReRaceFailStreak(f.Engine, f.Target); streak != 0 {
		t.Fatalf("reRaceFailStreak = %d, want 0", streak)
	}

	// fakeWireSetup saw exactly one call (for the new URL).
	calls := f.WireSetup.Calls()
	if len(calls) != 1 || calls[0] != newURL {
		t.Fatalf("WireSetup.Calls = %v, want [%q]", calls, newURL)
	}

	// The new TM is what swappable now routes to.
	wantTM := f.WireSetup.BuiltTM(newURL)
	if got := f.Target.Swappable().Current(); got != wantTM {
		t.Errorf("swappable.Current() != fakeWireSetup-built TM for %q", newURL)
	}
}

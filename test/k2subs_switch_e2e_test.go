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
	"time"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/engine"
	"github.com/kaitu-io/k2/wire"
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

// A1 — Resolve's initial Pick distribution roughly tracks recommendScore.
// Runs 400 cold-start fixtures; ~75% of picks should land on URL-A
// (recommendScore 0.75), tolerating 60–90% per existing project conventions.
func TestK2SubsSwitch_A1_WeightedInitialPick(t *testing.T) {
	tunnels := []config.Tunnel{
		{URL: tunA, RecommendScore: 0.75},
		{URL: tunB, RecommendScore: 0.25},
	}

	counts := map[string]int{}
	const N = 400
	for i := 0; i < N; i++ {
		// Each iteration runs in its own subtest scope so cleanups (incl.
		// goleak) settle per-iteration. Without this, all 400 fixtures'
		// httptest servers + manager state would pile up before the parent's
		// goleak runs and the report would be unactionable.
		t.Run("", func(t *testing.T) {
			f := newK2subsFixture(t, tunnels)
			resolvedURL := f.Resolve(context.Background())
			counts[stripCreds(resolvedURL)]++
		})
	}

	aPct := float64(counts[tunA]) / float64(N) * 100
	if aPct < 60 || aPct > 90 {
		t.Errorf("A = %.1f%% (counts=%v), want 60-90%% (recommendScore 0.75)", aPct, counts)
	}
}

// A3 — When 7 candidates have a ScoreSource installed, only the top 5 by
// adjusted weight are eligible. The bottom 2 (scores 0.4, 0.3) are never
// picked.
func TestK2SubsSwitch_A3_TopK5Truncation(t *testing.T) {
	bareTunnels := []string{
		"k2v5://t1:443", "k2v5://t2:443", "k2v5://t3:443",
		"k2v5://t4:443", "k2v5://t5:443", "k2v5://t6:443",
		"k2v5://t7:443",
	}
	tunnels := make([]config.Tunnel, len(bareTunnels))
	for i, u := range bareTunnels {
		tunnels[i] = config.Tunnel{URL: u, RecommendScore: 1}
	}

	f := newK2subsFixture(t, tunnels)
	sessions, _, err := f.Mgr.Resolve(&config.ClientConfig{
		Routes: []config.RouteConfig{{Via: f.SubsURL(), Match: muxAll()}},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	// Install a deterministic ScoreSource keyed by the credentialed URLs
	// (Subscription stores tunnels with creds injected by Manager.Resolve).
	sub := sessions[0].Subscription()
	scores := mapScoreSource{}
	descScores := []float64{0.9, 0.8, 0.7, 0.6, 0.5, 0.4, 0.3}
	for i, tun := range sub.Tunnels() {
		scores[tun.URL] = descScores[i]
	}
	sub.SetScoreSource(scores)

	seen := map[string]bool{}
	for i := 0; i < 3000; i++ {
		tun, err := sub.Pick(nil)
		if err != nil {
			t.Fatalf("Pick: %v", err)
		}
		seen[stripCreds(tun.URL)] = true
	}

	// Bottom 2 (t6, t7) must never appear; top 5 must each appear at least
	// once across 3000 samples.
	for _, banned := range []string{"k2v5://t6:443", "k2v5://t7:443"} {
		if seen[banned] {
			t.Errorf("top-K=5 violated; %s appeared in selections", banned)
		}
	}
	for _, want := range []string{"k2v5://t1:443", "k2v5://t2:443", "k2v5://t3:443", "k2v5://t4:443", "k2v5://t5:443"} {
		if !seen[want] {
			t.Errorf("top-5 candidate %s never selected", want)
		}
	}

	// Silence unused-import warnings until other groups land.
	_ = time.Second
	_ = wire.WireStats{}
}

// ---------------------------------------------------------------------------
// Group B — Probe ↔ switch interaction
// ---------------------------------------------------------------------------

// B1 — Sentinel score (-1) means "echo unsupported"; do not penalize.
// Pick falls back to pure recommendScore distribution.
func TestK2SubsSwitch_B1_SentinelNoPenalty(t *testing.T) {
	tunnels := []config.Tunnel{
		{URL: tunA, RecommendScore: 0.9},
		{URL: tunB, RecommendScore: 0.1},
	}
	f := newK2subsFixture(t, tunnels)
	sessions, _, err := f.Mgr.Resolve(&config.ClientConfig{
		Routes: []config.RouteConfig{{Via: f.SubsURL(), Match: muxAll()}},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	sub := sessions[0].Subscription()

	// Record sentinel for both URLs (Reachable=true, EchoSupported=false → Score=-1).
	for _, tun := range sub.Tunnels() {
		f.Reg.Record(tun.URL, wire.ProbeStats{
			Reachable: true, EchoSupported: false, MeasuredAt: time.Now(),
		})
	}

	counts := pickN(t, sub, 2000)
	if counts[tunA] <= counts[tunB] {
		t.Errorf("sentinel must not penalize; counts=%v", counts)
	}
}

// B2 — A single zero-score record is treated as "no data" (flake tolerance).
// URL is not excluded after just one bad probe.
func TestK2SubsSwitch_B2_SingleZeroFlakeTolerated(t *testing.T) {
	tunnels := []config.Tunnel{
		{URL: tunA, RecommendScore: 0.5},
		{URL: tunB, RecommendScore: 0.5},
	}
	f := newK2subsFixture(t, tunnels)
	sessions, _, err := f.Mgr.Resolve(&config.ClientConfig{
		Routes: []config.RouteConfig{{Via: f.SubsURL(), Match: muxAll()}},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	sub := sessions[0].Subscription()

	credsB := withCreds(tunB, "alice:tok")
	f.Reg.Record(credsB, wire.ProbeStats{Reachable: false, MeasuredAt: time.Now()})

	saw := map[string]bool{}
	for i := 0; i < 200; i++ {
		tun, err := sub.Pick(nil)
		if err != nil {
			t.Fatalf("Pick: %v", err)
		}
		saw[stripCreds(tun.URL)] = true
	}
	if !saw[tunB] {
		t.Errorf("single zero should not exclude; tunB never picked in 200 calls")
	}
}

// B3 — Two consecutive zeros confirm hard exclusion.
func TestK2SubsSwitch_B3_DoubleZeroHardExclude(t *testing.T) {
	tunnels := []config.Tunnel{
		{URL: tunA, RecommendScore: 0.5},
		{URL: tunB, RecommendScore: 0.5},
	}
	f := newK2subsFixture(t, tunnels)
	sessions, _, err := f.Mgr.Resolve(&config.ClientConfig{
		Routes: []config.RouteConfig{{Via: f.SubsURL(), Match: muxAll()}},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	sub := sessions[0].Subscription()

	credsB := withCreds(tunB, "alice:tok")
	f.Reg.Record(credsB, wire.ProbeStats{Reachable: false, MeasuredAt: time.Now()})
	f.Reg.Record(credsB, wire.ProbeStats{Reachable: false, MeasuredAt: time.Now()})

	neverSeenInPick(t, sub, tunB, 50)
}

// B4 — All tunnels probe-excluded → NextURL exhausts via Manager.
func TestK2SubsSwitch_B4_AllExcludedFiresFatal(t *testing.T) {
	tunnels := []config.Tunnel{
		{URL: tunA, RecommendScore: 0.5},
		{URL: tunB, RecommendScore: 0.5},
	}
	f := newK2subsFixture(t, tunnels)
	f.Resolve(context.Background())

	credsA := withCreds(tunA, "alice:tok")
	credsB := withCreds(tunB, "alice:tok")
	for _, c := range []string{credsA, credsB} {
		f.Reg.Record(c, wire.ProbeStats{Reachable: false, MeasuredAt: time.Now()})
		f.Reg.Record(c, wire.ProbeStats{Reachable: false, MeasuredAt: time.Now()})
	}

	err := f.TriggerSwitch(context.Background(), errors.New("wire dead"))
	if err == nil {
		t.Fatal("expected error when all tunnels probe-excluded")
	}
	if !errIs(err, nil, "all subscribed tunnels failed") &&
		!errIs(err, nil, "all tunnels excluded") {
		t.Errorf("error should mention exhaustion: %v", err)
	}
}

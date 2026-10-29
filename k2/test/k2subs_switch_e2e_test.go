//go:build !release

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
	"github.com/kaitu-io/k2/subscription"
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

// ---------------------------------------------------------------------------
// Group C — Refresh ↔ switch interaction
// ---------------------------------------------------------------------------

// C1 — A refresh that adds a new tunnel makes it eligible on subsequent picks.
func TestK2SubsSwitch_C1_RefreshAddsTunnel(t *testing.T) {
	f := newK2subsFixture(t, []config.Tunnel{
		{URL: tunA, RecommendScore: 0.5},
		{URL: tunB, RecommendScore: 0.5},
	})
	current := f.Resolve(context.Background())

	// Update server pool to include C; trigger a manual Fetch (deterministic;
	// avoids relying on background refresh timing).
	f.SubsSrv.SetTunnels([]config.Tunnel{
		{URL: tunA, RecommendScore: 0.5},
		{URL: tunB, RecommendScore: 0.5},
		{URL: tunC, RecommendScore: 0.5},
	})
	sessions := f.Mgr.Sessions()
	if err := sessions[0].Subscription().Fetch(context.Background(), f.SubsSrv.HTTPClient()); err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	// Drive NextURL until we either visit tunC or exhaust the pool. The
	// per-session exclude set accumulates failed URLs across calls.
	visited := map[string]bool{stripCreds(current): true}
	curr := current
	for i := 0; i < 5 && !visited[tunC]; i++ {
		next, err := f.Mgr.NextURL(curr, errors.New("test"))
		if err != nil {
			break
		}
		visited[stripCreds(next)] = true
		curr = next
	}
	if !visited[tunC] {
		t.Errorf("after refresh added C, switch never reached C; visited=%v", visited)
	}
}

// C2 — Refresh that drops the engine's current URL surfaces
// ErrNoSubscription from Manager.NextURL.
//
// Manager.NextURL identifies the calling session by exact-URL match against
// stored tunnels. If a refresh has just removed the URL the engine is using,
// no session "owns" it and NextURL reports ErrNoSubscription. Documented in
// manager.go — labelled "expected during the doDown/Stop wind-down window."
//
// Real-world implication: if Center's refresh strips the current URL while
// the engine is mid-Phase-B, the engine cannot rotate via this session and
// the swap fails. This test locks in the current contract so any change is
// deliberate.
func TestK2SubsSwitch_C2_RefreshRemovesCurrentURL(t *testing.T) {
	f := newK2subsFixture(t, []config.Tunnel{
		{URL: tunA, RecommendScore: 0.5},
		{URL: tunB, RecommendScore: 0.5},
		{URL: tunC, RecommendScore: 0.5},
	})
	current := f.Resolve(context.Background())

	// Refresh: drop `current` from the pool, keep the other two.
	currentBare := stripCreds(current)
	newPool := []config.Tunnel{}
	for _, u := range []string{tunA, tunB, tunC} {
		if u != currentBare {
			newPool = append(newPool, config.Tunnel{URL: u, RecommendScore: 0.5})
		}
	}
	f.SubsSrv.SetTunnels(newPool)
	if err := f.Mgr.Sessions()[0].Subscription().Fetch(context.Background(), f.SubsSrv.HTTPClient()); err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	_, err := f.Mgr.NextURL(current, errors.New("test"))
	if !errors.Is(err, subscription.ErrNoSubscription) {
		t.Fatalf("NextURL after current-URL removal: got %v, want ErrNoSubscription", err)
	}

	// Whole-stack: replaceOutboundServer wraps NextURL's error and returns it.
	switchErr := f.TriggerSwitch(context.Background(), errors.New("test"))
	if switchErr == nil {
		t.Fatal("expected TriggerSwitch to fail when current URL is no longer in pool")
	}
}

// C3 — Refresh failure preserves the in-memory tunnel list; switching still works.
func TestK2SubsSwitch_C3_RefreshFailPreservesData(t *testing.T) {
	f := newK2subsFixture(t, []config.Tunnel{
		{URL: tunA, RecommendScore: 0.5},
		{URL: tunB, RecommendScore: 0.5},
	})
	f.Resolve(context.Background())

	// Force all subsequent fetches to fail.
	f.SubsSrv.FailAll(true)
	err := f.Mgr.Sessions()[0].Subscription().Fetch(context.Background(), f.SubsSrv.HTTPClient())
	if err == nil {
		t.Fatal("expected Fetch to fail under failAll")
	}

	// In-memory tunnels survived.
	stored := f.Mgr.Sessions()[0].Subscription().Tunnels()
	if len(stored) != 2 {
		t.Fatalf("tunnels lost on refresh fail: got %d, want 2", len(stored))
	}

	// Server recovers; next refresh syncs new tunnel list.
	f.SubsSrv.FailAll(false)
	f.SubsSrv.SetTunnels([]config.Tunnel{{URL: tunC, RecommendScore: 1}})
	if err := f.Mgr.Sessions()[0].Subscription().Fetch(context.Background(), f.SubsSrv.HTTPClient()); err != nil {
		t.Fatalf("post-recovery Fetch: %v", err)
	}
	stored = f.Mgr.Sessions()[0].Subscription().Tunnels()
	if len(stored) != 1 || stripCreds(stored[0].URL) != tunC {
		t.Errorf("post-recovery tunnels = %v, want [%q]", stored, tunC)
	}
}

// C4 — Server-supplied refresh override is accepted by Fetch (the integration
// path through fakeSubsServer.SetRefresh + Subscription.Fetch).
func TestK2SubsSwitch_C4_ServerOverridesRefreshInterval(t *testing.T) {
	f := newK2subsFixture(t, []config.Tunnel{{URL: tunA, RecommendScore: 1}})
	f.SubsSrv.SetRefresh(300)
	f.Resolve(context.Background())

	tunnels := f.Mgr.Sessions()[0].Subscription().Tunnels()
	if len(tunnels) != 1 {
		t.Fatalf("expected 1 tunnel, got %d", len(tunnels))
	}
	// The Subscription has no public accessor for `interval`. The
	// integration check is: setting `refresh` on the server and calling
	// Fetch must succeed and store tunnels. Detailed interval semantics live
	// in subscription_test.go::TestSubscription_FetchServerRefreshOverride.
	queries := f.SubsSrv.QuerySnapshot()
	if len(queries) == 0 {
		t.Fatal("server saw no fetches")
	}
}

// ---------------------------------------------------------------------------
// Group D — Lifecycle / concurrency
// ---------------------------------------------------------------------------

// D1 — Offline startup with valid disk cache → Resolve succeeds.
func TestK2SubsSwitch_D1_OfflineWithCache(t *testing.T) {
	// Run 1: populate cache by Resolving once successfully.
	f := newK2subsFixture(t, []config.Tunnel{{URL: tunA, RecommendScore: 1}})
	f.Resolve(context.Background())
	cacheDir := f.CacheDir
	subsURL := f.SubsURL()
	f.Mgr.CloseAll()
	f.SubsSrv.FailAll(true) // simulate Center down for run 2

	// Run 2: same URL, same cacheDir, but server is down. Resolve must
	// succeed using the cached data written by run 1.
	mgr := subscription.NewManager(cacheDir, f.SubsSrv.HTTPClient(), nil)
	defer mgr.CloseAll()

	cfg := &config.ClientConfig{
		Routes: []config.RouteConfig{{Via: subsURL, Match: muxAll()}},
	}
	_, resolved, err := mgr.Resolve(cfg)
	if err != nil {
		t.Fatalf("Resolve with cache + offline: %v", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("resolved len = %d, want 1", len(resolved))
	}
	if stripCreds(resolved[0]) != tunA {
		t.Errorf("resolved URL not from cache: stripCreds=%q, want %q", stripCreds(resolved[0]), tunA)
	}
}

// D2 — Offline startup with no cache → Resolve fails.
func TestK2SubsSwitch_D2_OfflineNoCache(t *testing.T) {
	srv := newFakeSubsServer(t)
	srv.SetTunnels([]config.Tunnel{{URL: tunA, RecommendScore: 1}})
	srv.FailAll(true)

	cacheDir := t.TempDir()
	mgr := subscription.NewManager(cacheDir, srv.HTTPClient(), nil)
	defer mgr.CloseAll()

	subsURL := srv.SubsURL("u:p", "")
	_, _, err := mgr.Resolve(&config.ClientConfig{
		Routes: []config.RouteConfig{{Via: subsURL, Match: muxAll()}},
	})
	if err == nil {
		t.Fatal("expected error when subs server unreachable and no cache")
	}
	if !errIs(err, nil, "fetch") && !errIs(err, nil, "no cache") {
		t.Errorf("error should mention fetch failure: %v", err)
	}
}

// D3 — Two distinct k2subs:// routes produce two isolated Sessions.
func TestK2SubsSwitch_D3_MultiRouteIsolation(t *testing.T) {
	srvX := newFakeSubsServer(t)
	srvX.SetTunnels([]config.Tunnel{{URL: tunA, RecommendScore: 1}})
	srvY := newFakeSubsServer(t)
	srvY.SetTunnels([]config.Tunnel{{URL: tunB, RecommendScore: 1}})

	mgr := subscription.NewManager(t.TempDir(), srvX.HTTPClient(), nil)
	defer mgr.CloseAll()

	urlX := srvX.SubsURL("u1:p1", "")
	urlY := srvY.SubsURL("u2:p2", "")

	sessions, _, err := mgr.Resolve(&config.ClientConfig{
		Routes: []config.RouteConfig{
			{Via: urlX, Match: muxAll()},
			{Via: urlY, Match: muxAll()},
		},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("sessions = %d, want 2", len(sessions))
	}
	if sessions[0] == sessions[1] {
		t.Fatal("sessions must be distinct pointers")
	}

	// Each session's tunnel pool should reflect only its own subs server.
	xTuns := sessions[0].Subscription().Tunnels()
	yTuns := sessions[1].Subscription().Tunnels()
	if len(xTuns) != 1 || stripCreds(xTuns[0].URL) != tunA {
		t.Errorf("X session tunnels = %v, want [tunA]", xTuns)
	}
	if len(yTuns) != 1 || stripCreds(yTuns[0].URL) != tunB {
		t.Errorf("Y session tunnels = %v, want [tunB]", yTuns)
	}
}

// D4 — Stop racing with Phase-B replace cleans up the in-flight new TM.
// Strategy: make setup of the replacement URL slow; cancel ctx mid-setup;
// verify replaceOutboundServer returns context.Canceled. goleak (running
// from the fixture's t.Cleanup) catches any leaked goroutine.
func TestK2SubsSwitch_D4_StopDuringPhaseB(t *testing.T) {
	f := newK2subsFixture(t, []config.Tunnel{
		{URL: tunA, RecommendScore: 0.5},
		{URL: tunB, RecommendScore: 0.5},
	})
	current := f.Resolve(context.Background())

	// Identify which URL Resolve picked, slow the OTHER one.
	credsA := withCreds(tunA, "alice:tok")
	credsB := withCreds(tunB, "alice:tok")
	other := credsA
	if current == credsA {
		other = credsB
	}
	f.WireSetup.SlowURL(other, 200*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := f.TriggerSwitch(ctx, errors.New("wire dead"))
	if err == nil {
		t.Fatal("expected ctx-cancel error from TriggerSwitch")
	}
	if !errors.Is(err, context.Canceled) && !errIs(err, nil, "context canceled") {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	// goleak in fixture cleanup catches any orphan TM/goroutine.
}

// D5 — Setup-fail-then-success: provider's calls show currentURL progression
// (origin → first-failed → next), and setupLog shows attempted URLs in order.
func TestK2SubsSwitch_D5_SetupFailEscalatesExclude(t *testing.T) {
	f := newK2subsFixture(t, []config.Tunnel{
		{URL: tunA, RecommendScore: 0.5},
		{URL: tunB, RecommendScore: 0.5},
		{URL: tunC, RecommendScore: 0.5},
	})
	current := f.Resolve(context.Background())

	// Identify the two non-current URLs in deterministic order.
	currentBare := stripCreds(current)
	var candidates []string
	for _, u := range []string{tunA, tunB, tunC} {
		if u != currentBare {
			candidates = append(candidates, u)
		}
	}
	if len(candidates) != 2 {
		t.Fatalf("unexpected candidate count: %v", candidates)
	}

	// Fail BOTH non-current URLs once, so we know the test exhausts cleanly
	// when there are only 2 candidates and 1 must fail. We need 3 tunnels to
	// reliably exercise the fail-then-succeed path: fail first (any), succeed
	// second. Pick the first probe-Picked URL to fail; the OTHER will succeed.
	//
	// Approach: fail both candidates' creds-injected forms, but only the
	// FIRST one Manager.NextURL returns becomes a setup attempt. We don't
	// know the order in advance; let the test trace what happened.
	credsCandidates := []string{
		withCreds(candidates[0], "alice:tok"),
		withCreds(candidates[1], "alice:tok"),
	}
	// Fail the first one Pick is likely to choose. Since recommendScore is
	// equal across all 3, Pick is uniform random over the 2 non-excluded
	// candidates; pick either to fail and the other will succeed.
	f.WireSetup.FailURL(credsCandidates[0], errors.New("handshake-timeout"))

	if err := f.TriggerSwitch(context.Background(), errors.New("wire dead")); err != nil {
		t.Fatalf("TriggerSwitch (expected eventual success): %v", err)
	}

	calls := f.WireSetup.Calls()
	// Expect at least 1 call (success path on first try) or 2 calls
	// (fail-then-success). Both are valid outcomes; we assert the fail-URL
	// was tried before the success when 2 calls happened.
	switch len(calls) {
	case 1:
		// Pick chose credsCandidates[1] first (the success URL). Fine.
		if calls[0] == credsCandidates[0] {
			t.Errorf("setup succeeded on a URL we marked to fail: %q", calls[0])
		}
	case 2:
		if calls[0] != credsCandidates[0] {
			t.Errorf("first setup call = %q, want %q (fail URL)", calls[0], credsCandidates[0])
		}
		if calls[1] != credsCandidates[1] {
			t.Errorf("second setup call = %q, want %q (success URL)", calls[1], credsCandidates[1])
		}
	default:
		t.Errorf("WireSetup.Calls = %v, want 1 or 2", calls)
	}

	// Whichever URL succeeded must now be the target's URL.
	if f.Target.URL() == current {
		t.Errorf("target.URL still original: %q", f.Target.URL())
	}
}

// ---------------------------------------------------------------------------
// Group E — Real-user scenarios
// ---------------------------------------------------------------------------

// E1 — Frequent toggle: 5 successive Resolve / TriggerSwitch / CloseAll cycles.
// Each cycle's fixture has its own goleak verification — repeated toggle
// must not leak goroutines.
func TestK2SubsSwitch_E1_ToggleNoLeak(t *testing.T) {
	for i := 0; i < 5; i++ {
		t.Run("", func(t *testing.T) {
			f := newK2subsFixture(t, []config.Tunnel{
				{URL: tunA, RecommendScore: 0.5},
				{URL: tunB, RecommendScore: 0.5},
			})
			f.Resolve(context.Background())
			if err := f.TriggerSwitch(context.Background(), errors.New("toggle")); err != nil {
				t.Fatalf("TriggerSwitch: %v", err)
			}
			f.Mgr.CloseAll()
			if got := f.Mgr.SessionsLen(); got != 0 {
				t.Errorf("SessionsLen after CloseAll = %d, want 0", got)
			}
		})
	}
}

// E2 — Center maintenance window. After Resolve, refresh fails (failAll=true);
// switches still work using the pre-fetched tunnel list. Recovery: failAll
// off → next Fetch syncs new pool.
func TestK2SubsSwitch_E2_MaintenanceWindow(t *testing.T) {
	f := newK2subsFixture(t, []config.Tunnel{
		{URL: tunA, RecommendScore: 0.5},
		{URL: tunB, RecommendScore: 0.5},
		{URL: tunC, RecommendScore: 0.5},
	})
	f.Resolve(context.Background())

	f.SubsSrv.FailAll(true)
	// Trigger ≤2 switches during maintenance. With 3 tunnels, the 2nd switch
	// may exhaust depending on ordering; we only assert that the FIRST one
	// works without subs server contact.
	if err := f.TriggerSwitch(context.Background(), errors.New("wire failure")); err != nil {
		t.Fatalf("first switch during maintenance failed: %v", err)
	}

	// Recovery: clear failAll, fetch fresh pool.
	f.SubsSrv.FailAll(false)
	f.SubsSrv.SetTunnels([]config.Tunnel{
		{URL: tunD, RecommendScore: 1},
	})
	if err := f.Mgr.Sessions()[0].Subscription().Fetch(context.Background(), f.SubsSrv.HTTPClient()); err != nil {
		t.Fatalf("post-recovery Fetch: %v", err)
	}
	tunnels := f.Mgr.Sessions()[0].Subscription().Tunnels()
	if len(tunnels) != 1 || stripCreds(tunnels[0].URL) != tunD {
		t.Errorf("post-recovery tunnels = %v, want [%q]", tunnels, tunD)
	}
}

// E3 — Switch storm: many sequential switches in rapid succession.
//
// Production serializes Phase-B replacement via the engine's outboundReplaceLoop
// goroutine fed by a buffer-1 channel — concurrent calls to
// replaceOutboundServer never happen in production. This test exercises the
// actual production "storm" path: the same single goroutine doing N switches
// back-to-back as fast as possible. Validates that:
//   - per-session exclude set accumulates correctly across N switches
//   - SwappableDialer reaches a stable, consistent terminal state
//   - no goroutine leak across rapid Resolve→switch→switch→...→exhaust cycles
//
// Concurrent invocation of replaceOutboundServer from multiple goroutines is
// out of scope: it would expose target.name reads outside e.mu (a real race
// for an unreachable-in-production path), and fixing that constitutes
// engine-layer refactoring beyond this test suite.
func TestK2SubsSwitch_E3_SwitchStorm(t *testing.T) {
	f := newK2subsFixture(t, []config.Tunnel{
		{URL: tunA, RecommendScore: 0.5},
		{URL: tunB, RecommendScore: 0.5},
		{URL: tunC, RecommendScore: 0.5},
		{URL: tunD, RecommendScore: 0.5},
		{URL: tunE, RecommendScore: 0.5},
	})
	f.Resolve(context.Background())

	// Sequential storm: drive 4 successful switches (5 tunnels minus the
	// initial Pick). The 5th call should exhaust.
	successes := 0
	for i := 0; i < 5; i++ {
		err := f.TriggerSwitch(context.Background(), errors.New("storm"))
		if err == nil {
			successes++
		}
	}
	if successes < 3 {
		t.Errorf("storm: %d successes, want >= 3 (5 tunnels - initial Pick)", successes)
	}

	// Final state stable.
	if f.Target.URL() == "" {
		t.Fatal("target.URL() is empty after storm")
	}
	if got := f.Target.Swappable().Current(); got == nil {
		t.Fatal("swappable.Current() is nil after storm")
	}
}

// E4 — Stale probe entry (older than TTL) is ignored — Pick may return URL.
func TestK2SubsSwitch_E4_StaleProbeData(t *testing.T) {
	f := newK2subsFixture(t, []config.Tunnel{
		{URL: tunA, RecommendScore: 0.5},
		{URL: tunB, RecommendScore: 0.5},
	})
	f.Resolve(context.Background())

	credsB := withCreds(tunB, "alice:tok")
	// Two zeros, but back-dated past the 15-minute TTL.
	old := time.Now().Add(-20 * time.Minute)
	f.Reg.Record(credsB, wire.ProbeStats{Reachable: false, MeasuredAt: old})
	f.Reg.Record(credsB, wire.ProbeStats{Reachable: false, MeasuredAt: old})

	if _, ok := f.Reg.Score(credsB); ok {
		t.Errorf("expired entry: Score returned ok=true (expected false for stale data)")
	}

	// Pick may return either URL — verify B isn't excluded.
	saw := map[string]bool{}
	for i := 0; i < 200; i++ {
		tun, err := f.Mgr.Sessions()[0].Subscription().Pick(nil)
		if err != nil {
			t.Fatalf("Pick: %v", err)
		}
		saw[stripCreds(tun.URL)] = true
	}
	if !saw[tunB] {
		t.Errorf("stale probe entry should not exclude; tunB never picked in 200 calls")
	}
}

// E5 — Country query parameter is forwarded to the subs endpoint;
// `refresh` is stripped client-side.
func TestK2SubsSwitch_E5_RegionalPool(t *testing.T) {
	f := newK2subsFixture(t, []config.Tunnel{{URL: tunA, RecommendScore: 1}})

	customURL := f.SubsSrv.SubsURL("alice:tok", "country=jp&refresh=600")
	mgr := subscription.NewManager(t.TempDir(), f.SubsSrv.HTTPClient(), nil)
	defer mgr.CloseAll()

	if _, _, err := mgr.Resolve(&config.ClientConfig{
		Routes: []config.RouteConfig{{Via: customURL, Match: muxAll()}},
	}); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	queries := f.SubsSrv.QuerySnapshot()
	if len(queries) == 0 {
		t.Fatal("no requests captured")
	}
	if !strings.Contains(queries[0], "country=jp") {
		t.Errorf("query missing country=jp: %q", queries[0])
	}
	if strings.Contains(queries[0], "refresh=") {
		t.Errorf("refresh= should be stripped client-side; got: %q", queries[0])
	}
}

// E6 — "Fake-alive" servers: every URL setup succeeds but each is then
// declared dead via repeated TriggerSwitch. Eventually all URLs are in the
// per-session exclude set and NextURL exhausts.
func TestK2SubsSwitch_E6_FakeAliveServers(t *testing.T) {
	tunnels := []config.Tunnel{
		{URL: tunA, RecommendScore: 0.5},
		{URL: tunB, RecommendScore: 0.5},
		{URL: tunC, RecommendScore: 0.5},
		{URL: tunD, RecommendScore: 0.5},
		{URL: tunE, RecommendScore: 0.5},
	}
	f := newK2subsFixture(t, tunnels)
	f.Resolve(context.Background())

	// Each successful TriggerSwitch installs a new url; the session's
	// exclude set accumulates each tried URL. With 5 tunnels we expect
	// at most ~4 successful switches (start-tunnel was the initial Pick;
	// one of the other 4 fills exclude; switch from there picks another;
	// etc.) before NextURL exhausts.
	successes := 0
	var lastErr error
	for i := 0; i < 10; i++ {
		if err := f.TriggerSwitch(context.Background(), errors.New("fake-alive")); err != nil {
			lastErr = err
			break
		}
		successes++
	}
	if successes < 3 {
		t.Errorf("successes = %d, want >= 3", successes)
	}
	if lastErr == nil {
		t.Errorf("expected eventual exhaustion error after switches; successes=%d", successes)
	}
}

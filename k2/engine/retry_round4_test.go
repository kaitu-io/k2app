package engine

// Dead-node fix round 4 (re-review 3: I-1, I-2, M-1, M-2, M-3). The retry
// schedule during a Center outage must never fail the engine — also for the
// k2subs shell provider — and an outage's exclusions must not outlive it.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/rule"
	"github.com/kaitu-io/k2/subscription"
	"github.com/kaitu-io/k2/wire"
)

// k2subsManager resolves a real subscription.Manager against a local k2subs
// endpoint listing hosts (each "k2v5://<host>:443"). Returns the manager and
// the tunnel URLs in the order served.
func k2subsManager(t *testing.T, hosts ...string) (*subscription.Manager, []string) {
	t.Helper()
	var body strings.Builder
	body.WriteString(`{"tunnels":[`)
	for i, h := range hosts {
		if i > 0 {
			body.WriteString(",")
		}
		fmt.Fprintf(&body, `{"url":"k2v5://%s:443","recommendScore":0.5}`, h)
	}
	body.WriteString(`]}`)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, body.String())
	}))
	t.Cleanup(srv.Close)
	mgr := subscription.NewManager(t.TempDir(), srv.Client(), nil)
	cfg := &config.ClientConfig{Routes: []config.RouteConfig{{
		Via:   "k2subs://u:t@" + strings.TrimPrefix(srv.URL, "https://"),
		Match: rule.MatchConfig{All: true},
	}}}
	if _, _, err := mgr.Resolve(cfg); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	t.Cleanup(mgr.CloseAll)
	var urls []string
	for _, tu := range mgr.Tunnels() {
		urls = append(urls, tu.URL)
	}
	if len(urls) != len(hosts) {
		t.Fatalf("tunnels = %v, want %d", urls, len(hosts))
	}
	return mgr, urls
}

func (hs *foHarness) setupCount(host string) int {
	hs.servers.mu.Lock()
	defer hs.servers.mu.Unlock()
	return hs.servers.setups[host]
}

// assertStillScheduled: connected, reconnecting 503 shown, never
// disconnected / 401, no OnOutboundFatal.
func (hs *foHarness) assertStillScheduled(when string) {
	hs.t.Helper()
	if s, le := hs.engineState(); s != StateConnected || le != nil {
		hs.t.Fatalf("%s: engine = %s / %v, want connected / nil", when, s, le)
	}
	if hs.h.sawState(StateDisconnected) || hs.h.sawCode(ErrCodeAuthRejected) {
		hs.t.Fatalf("%s: status showed disconnected or 401", when)
	}
	if n := hs.h.fatalCount(); n != 0 {
		hs.t.Fatalf("%s: OnOutboundFatal = %d, want 0", when, n)
	}
	if s, _ := hs.h.last(); !isReconnectingCode(ErrCodeServerUnreachable)(s) {
		hs.t.Fatalf("%s: last status = %+v, want reconnecting/503", when, s)
	}
}

// I-1, real subscription.Manager: k2subs session, Center unreachable from
// every node, the current node dead. The escalate round excludes every
// shell tunnel; without the shell Reset the first retry tick found nothing
// to try and failed the engine 503 (k2r: reconnectAfterFatal mid-outage).
// Every tick must stay scheduled, re-try the candidates, and fail over once
// Center answers again.
func TestRetry_K2subsCenterOutageNeverFailsThenFailsOver(t *testing.T) {
	mgr, urls := k2subsManager(t, "a.example", "b.example", "c.example")
	hs := newFoHarness(t, foOpts{shell: mgr, targetURL: urls[0], net: []checkResult{checkMatched}})
	// a: dead (harness default, not alive). b, c: code-less (Center down).
	hs.servers.mu.Lock()
	hs.servers.setupErrs["b.example"] = codeLessReject()
	hs.servers.setupErrs["c.example"] = codeLessReject()
	hs.servers.mu.Unlock()

	hs.escalate()
	hs.waitStatus("reconnecting 503", isReconnectingCode(ErrCodeServerUnreachable))
	tm := hs.nextTimer()
	hs.assertStillScheduled("after escalate")

	for tick := 1; tick <= 3; tick++ {
		beforeB, beforeC := hs.setupCount("b.example"), hs.setupCount("c.example")
		tm.fire()
		tm = hs.nextTimer() // re-armed: still on the schedule
		hs.assertStillScheduled(fmt.Sprintf("tick %d (Center down)", tick))
		if hs.setupCount("b.example") != beforeB+1 || hs.setupCount("c.example") != beforeC+1 {
			t.Fatalf("tick %d: setups b %d→%d c %d→%d, want each re-tried once (shell exclusions Reset)",
				tick, beforeB, hs.setupCount("b.example"), beforeC, hs.setupCount("c.example"))
		}
	}

	// Center back: the candidates admit; a stays dead.
	hs.servers.mu.Lock()
	delete(hs.servers.setupErrs, "b.example")
	delete(hs.servers.setupErrs, "c.example")
	hs.servers.mu.Unlock()
	hs.servers.setAlive("b.example", true)
	hs.servers.setAlive("c.example", true)
	tm.fire()
	eventually(t, "failed over off the dead node", func() bool {
		u := hs.targetURL()
		return u == urls[1] || u == urls[2]
	})
	hs.waitStatus("connected, no hold", func(s Status) bool { return s.State == StateConnected && s.Error == nil })
	if s, le := hs.engineState(); s != StateConnected || le != nil || hs.h.fatalCount() != 0 {
		t.Fatalf("engine = %s / %v, fatal = %d after the failover", s, le, hs.h.fatalCount())
	}
}

// exclShell is a shell provider with its own exclusion list and NO Reset —
// e.g. a third-party provider. Its exclusions survive every engine Reset.
type exclShell struct {
	urls     []string
	mu       sync.Mutex
	excluded map[string]bool
}

func (s *exclShell) OwnsURL(u string) bool {
	for _, x := range s.urls {
		if x == u {
			return true
		}
	}
	return false
}

func (s *exclShell) NextURL(cur string, _ error) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.excluded[cur] = true
	for _, u := range s.urls {
		if !s.excluded[u] {
			return u, nil
		}
	}
	return "", errors.New("shell: all tunnels excluded")
}

// I-1 (independent guard): a retry-tick Phase B that tries zero candidates
// (everything still excluded) stays on the retry schedule — it never
// reaches failExhausted / fail(503) / OnOutboundFatal.
func TestRetry_TickTriedZeroStaysScheduled(t *testing.T) {
	shell := &exclShell{urls: []string{foURLA, foURLB}, excluded: map[string]bool{}}
	hs := newFoHarness(t, foOpts{shell: shell, net: []checkResult{checkMatched}})
	hs.servers.mu.Lock()
	hs.servers.setupErrs["b.example"] = codeLessReject() // Center down; a dead
	hs.servers.mu.Unlock()

	hs.escalate()
	hs.waitStatus("reconnecting 503", isReconnectingCode(ErrCodeServerUnreachable))
	tm := hs.nextTimer()
	for tick := 1; tick <= 3; tick++ {
		before := hs.provider.count()
		tm.fire()
		tm = hs.nextTimer()
		if hs.provider.count() == before {
			t.Fatalf("tick %d: no Phase B ran (precondition: a is dead, not code-less)", tick)
		}
		hs.assertStillScheduled(fmt.Sprintf("tick %d, zero candidates tried", tick))
	}
	if n := hs.setupCount("b.example"); n != 1 {
		t.Fatalf("setups on b = %d, want 1 (the shell never offers it again)", n)
	}
}

// M-3: while the current node answers code-less (alive, Center down) a retry
// tick does Phase A only — no candidate setups for the length of the outage.
func TestRetry_CenterOutageLiveNodeTickNoPhaseB(t *testing.T) {
	hs := newFoHarness(t, foOpts{
		fallbacks: map[string][]string{foURLA: {foURLB, foURLC}},
		net:       []checkResult{checkMatched},
	})
	raceTransportFn = func(ctx context.Context, cfg wire.Config, _ string, _ wire.DirectDialer, _ *wire.CookieJar) (*wire.RaceResult, error) {
		hs.servers.mu.Lock()
		hs.servers.races[cfg.Host]++
		hs.servers.mu.Unlock()
		return nil, fmt.Errorf("transport race: %w", codeLessReject())
	}
	hs.servers.mu.Lock()
	hs.servers.setupErrs["b.example"] = codeLessReject()
	hs.servers.setupErrs["c.example"] = codeLessReject()
	hs.servers.mu.Unlock()

	hs.escalate()
	hs.waitStatus("reconnecting 503", isReconnectingCode(ErrCodeServerUnreachable))
	tm := hs.nextTimer()
	setupsB, setupsC := hs.setupCount("b.example"), hs.setupCount("c.example")
	calls := hs.provider.count()
	for tick := 1; tick <= 3; tick++ {
		races := hs.servers.raceCount("a.example")
		tm.fire()
		tm = hs.nextTimer()
		if n := hs.servers.raceCount("a.example"); n != races+1 {
			t.Fatalf("tick %d: Phase-A races on a = %d, want %d", tick, n, races+1)
		}
		hs.assertStillScheduled(fmt.Sprintf("tick %d", tick))
	}
	if hs.setupCount("b.example") != setupsB || hs.setupCount("c.example") != setupsC || hs.provider.count() != calls {
		t.Fatalf("ticks ran Phase B on a live code-less node: setups b %d→%d c %d→%d, NextURL %d→%d",
			setupsB, hs.setupCount("b.example"), setupsC, hs.setupCount("c.example"), calls, hs.provider.count())
	}
}

// I-2: a Center outage excludes a, b and c; Center returns and a answers on
// a retry tick; a then dies within candidateExcludeTTL. The outage's
// exclusions must be gone, so it fails over to b — never 503 with live
// fallbacks.
func TestRetry_OutageEndsThenNodeDiesFailsOverNo503(t *testing.T) {
	hs := newFoHarness(t, foOpts{
		fallbacks: map[string][]string{foURLA: {foURLB, foURLC}},
		net:       []checkResult{checkMatched},
	})
	var aState atomic.Int32 // 0 code-less (Center down), 1 answers, 2 dead
	raceTransportFn = func(ctx context.Context, cfg wire.Config, _ string, _ wire.DirectDialer, _ *wire.CookieJar) (*wire.RaceResult, error) {
		hs.servers.mu.Lock()
		hs.servers.races[cfg.Host]++
		hs.servers.mu.Unlock()
		switch aState.Load() {
		case 0:
			return nil, fmt.Errorf("transport race: %w", codeLessReject())
		case 1:
			return &wire.RaceResult{Dialer: &stubDialer{}, TransportType: "quic-443"}, nil
		}
		return nil, errors.New("transport race: dial a.example: connection refused")
	}
	hs.servers.mu.Lock()
	hs.servers.setupErrs["b.example"] = codeLessReject()
	hs.servers.setupErrs["c.example"] = codeLessReject()
	hs.servers.mu.Unlock()

	hs.escalate()
	hs.waitStatus("reconnecting 503", isReconnectingCode(ErrCodeServerUnreachable))
	tm := hs.nextTimer()
	// The code-less exhaustion itself forgets the outage's exclusions.
	hs.static.mu.Lock()
	left := len(hs.static.excluded)
	hs.static.mu.Unlock()
	if left != 0 {
		t.Fatalf("static exclusions after a code-less exhaustion = %d, want 0 (Reset)", left)
	}
	tm.fire() // Center still down
	tm = hs.nextTimer()

	// Center back: a answers on the next tick and leaves the schedule.
	aState.Store(1)
	hs.servers.mu.Lock()
	delete(hs.servers.setupErrs, "b.example")
	delete(hs.servers.setupErrs, "c.example")
	hs.servers.mu.Unlock()
	hs.servers.setAlive("b.example", true)
	hs.servers.setAlive("c.example", true)
	resets := hs.provider.resetCount()
	tm.fire()
	hs.waitStatus("connected on a", func(s Status) bool { return s.State == StateConnected && s.Error == nil })
	if hs.targetURL() != foURLA {
		t.Fatalf("target = %s, want a (its own node answered)", hs.targetURL())
	}
	eventually(t, "provider Reset when a leaves the schedule", func() bool { return hs.provider.resetCount() > resets })

	// Minutes later (well inside candidateExcludeTTL) a dies.
	aState.Store(2)
	hs.escalate()
	eventually(t, "failed over to b", func() bool { return hs.targetURL() == foURLB })
	if s, le := hs.engineState(); s != StateConnected || le != nil {
		t.Fatalf("engine = %s / %v after a died, want connected on b", s, le)
	}
	if hs.h.fatalCount() != 0 || hs.h.sawState(StateDisconnected) {
		t.Fatalf("OnOutboundFatal = %d / disconnected = %v, want neither", hs.h.fatalCount(), hs.h.sawState(StateDisconnected))
	}
}

// M-2: a Phase-B swap starts the new node at zero strikes — strikes earned on
// the dead old TransportManager never carry onto the new one.
func TestEchoProbe_SwapZeroesOldStrikes(t *testing.T) {
	ot, _ := newDeadNodeTransport("t-swap-strikes")
	hm, ch := newHMForEcho(t, []*outboundTransport{ot})
	clk := useFakeClock(hm, ot)
	st := &echoLoopState{}
	for i := 0; i < echoReraceThreshold-1; i++ {
		userDial(ot)
		clk.advance(probeStep)
		hm.probeOnce(ot, st, time.Now())
	}
	if st.strikes != echoReraceThreshold-1 {
		t.Fatalf("precondition: strikes = %d", st.strikes)
	}
	nt, _ := newDeadNodeTransport("t-swap-strikes-new")
	nt.tm.SetConnFailClock(clk.now)
	ot.tm = nt.tm
	userDial(ot)
	clk.advance(probeStep)
	hm.probeOnce(ot, st, time.Now())
	if st.strikes != 1 || reraced(ch) {
		t.Fatalf("after the swap: strikes = %d, rerace = %v; want 1, false (old strikes dropped)", st.strikes, reraced(ch))
	}
}

// cachedConnDialer: a cached connection whose echo times out (F1 blackhole
// shape), while user dials through it fail or answer on demand.
type cachedConnDialer struct {
	deadNodeDialer
}

func (d *cachedConnDialer) RunEchoProbe(context.Context) error { return context.DeadlineExceeded }

// M-1: with a cached connection the echo result is the evidence. A user-dial
// failure run ending on an "answered" dial (which over a cached connection
// may be a no-round-trip UDP open) must neither zero the echo strikes nor
// count as the node's recovery.
func TestEchoProbe_CachedConnRunEndKeepsEchoStrikes(t *testing.T) {
	d := &cachedConnDialer{}
	d.fail.Store(true)
	tm := wire.NewTransportManager()
	tm.SetPrimary(d, "quic-443")
	ot := &outboundTransport{tm: tm, name: "t-cached"}
	hm, ch := newHMForEcho(t, []*outboundTransport{ot})
	e := New()
	e.wireHealthRecovered(hm)
	clk := useFakeClock(hm, ot)
	st := &echoLoopState{}

	hm.probeOnce(ot, st, time.Now()) // echo timeout: strike 1
	userDial(ot)                     // a failure run opens
	clk.advance(time.Second)
	hm.probeOnce(ot, st, time.Now()) // strike 2, run seen
	if st.strikes != 2 {
		t.Fatalf("precondition: strikes = %d", st.strikes)
	}
	e.mu.Lock()
	ot.reRaceFailStreak = 1
	e.mu.Unlock()
	d.fail.Store(false)
	userDial(ot) // "answered": the run ends
	clk.advance(time.Second)
	hm.probeOnce(ot, st, time.Now()) // strike 3 → re-race
	if !reraced(ch) {
		t.Fatalf("no re-race after 3 echo timeouts (strikes = %d): the run end zeroed echo strikes", st.strikes)
	}
	e.mu.Lock()
	streak := ot.reRaceFailStreak
	e.mu.Unlock()
	if streak != 1 {
		t.Fatalf("streak = %d, want 1 kept (a cached-path run end is not proof of life)", streak)
	}
}

// metadataTimeout is a candidate that completed the transport handshake and
// whose metadata reply never came (wire.ErrMetadataTimeout): Center hanging.
func metadataTimeout() error {
	return &wire.ProxyLayerError{Err: fmt.Errorf("%w: read response: %w", wire.ErrMetadataTimeout, context.DeadlineExceeded)}
}

// S15 ruling: Center hangs (no refusal), the current node is dead, the
// fallbacks connect at transport and their metadata reply times out, the
// network is online. No fail(503): the engine stays on the retry schedule
// (reconnecting 503), and once metadata answers it fails over.
func TestRetry_MetadataTimeoutCandidatesStayScheduledThenFailOver(t *testing.T) {
	hs := newFoHarness(t, foOpts{
		fallbacks: map[string][]string{foURLA: {foURLB, foURLC}},
		net:       []checkResult{checkMatched},
	})
	// a: dead at transport (harness default). b, c: metadata hangs.
	hs.servers.mu.Lock()
	hs.servers.setupErrs["b.example"] = metadataTimeout()
	hs.servers.setupErrs["c.example"] = metadataTimeout()
	hs.servers.mu.Unlock()

	hs.escalate()
	hs.waitStatus("reconnecting 503", isReconnectingCode(ErrCodeServerUnreachable))
	tm := hs.nextTimer()
	hs.assertStillScheduled("after escalate")
	for tick := 1; tick <= 2; tick++ {
		tm.fire()
		tm = hs.nextTimer()
		hs.assertStillScheduled(fmt.Sprintf("tick %d (Center hanging)", tick))
	}

	// Metadata answers again.
	hs.servers.mu.Lock()
	delete(hs.servers.setupErrs, "b.example")
	delete(hs.servers.setupErrs, "c.example")
	hs.servers.mu.Unlock()
	hs.servers.setAlive("b.example", true)
	hs.servers.setAlive("c.example", true)
	tm.fire()
	eventually(t, "failed over to b", func() bool { return hs.targetURL() == foURLB })
	hs.waitStatus("connected, no hold", func(s Status) bool { return s.State == StateConnected && s.Error == nil })
	if hs.h.fatalCount() != 0 || hs.h.sawState(StateDisconnected) {
		t.Fatalf("OnOutboundFatal = %d / disconnected = %v", hs.h.fatalCount(), hs.h.sawState(StateDisconnected))
	}
}

// S15: a metadata timeout is a code-less reject in the replace loop (never
// user action, never node death) and a retryable server error at initial
// connect — never the 401 the code-less reject maps to there.
func TestMetadataTimeoutClassification(t *testing.T) {
	err := fmt.Errorf("transport race: %w", errors.Join(metadataTimeout(), errors.New("tcpws: connection refused")))
	if !isCodeLessReject(err) {
		t.Fatal("isCodeLessReject(metadata timeout) = false")
	}
	if ce := replaceLoopUserAction(err); ce != nil {
		t.Fatalf("replaceLoopUserAction = %v, want nil", ce)
	}
	if ce := ClassifyError(err); ce.Code != ErrCodeServerUnreachable || ce.RequiresUserAction() {
		t.Fatalf("ClassifyError = %v, want 503 (server side, retryable)", ce)
	}
	// A transport failure stays node death.
	if isCodeLessReject(errors.New("transport race: dial b.example: connection refused")) {
		t.Fatal("a transport failure classified as code-less")
	}
}

// A coded verdict elsewhere in the same race error is not masked by a
// metadata timeout on the other candidate.
func TestMetadataTimeoutDoesNotMaskCodedReject(t *testing.T) {
	err := fmt.Errorf("transport race: %w", errors.Join(metadataTimeout(), codedReject(ErrCodePaymentRequired)))
	if isCodeLessReject(err) {
		t.Fatal("timeout + coded 402 classified code-less")
	}
	if ce := replaceLoopUserAction(err); ce == nil || ce.Code != ErrCodePaymentRequired {
		t.Fatalf("replaceLoopUserAction = %v, want 402", ce)
	}
}

// Review 4 concern 4: a k2subs session whose earlier failovers excluded
// every other node (Manager exclusions never expire). The current node dies
// while the others are alive: escalate's first round tries nothing, so it
// Resets and runs one more round — a failover, never fail(503).
func TestEscalate_K2subsAllExcludedByEarlierFailoversFailsOver(t *testing.T) {
	mgr, urls := k2subsManager(t, "a.example", "b.example", "c.example")
	// Earlier failovers in this session: b → c → a.
	_, _ = mgr.NextURL(urls[1], errors.New("b died"))
	_, _ = mgr.NextURL(urls[2], errors.New("c died"))
	hs := newFoHarness(t, foOpts{shell: mgr, targetURL: urls[0], net: []checkResult{checkMatched}})
	hs.servers.setAlive("b.example", true) // back for hours
	hs.servers.setAlive("c.example", true)

	hs.escalate() // a dies
	eventually(t, "failed over off a", func() bool {
		u := hs.targetURL()
		return u == urls[1] || u == urls[2]
	})
	if s, le := hs.engineState(); s != StateConnected || le != nil {
		t.Fatalf("engine = %s / %v, want connected", s, le)
	}
	if hs.h.fatalCount() != 0 || hs.h.sawState(StateDisconnected) {
		t.Fatalf("OnOutboundFatal = %d / disconnected = %v, want neither", hs.h.fatalCount(), hs.h.sawState(StateDisconnected))
	}
}

// Review 4 concern 4: the retry is exactly one round — a second
// tried-nothing is an exhaustion like any other (a provider that cannot be
// Reset keeps everything excluded): since R1 it goes on the retry schedule.
func TestEscalate_TriedNothingTwiceIsExhausted(t *testing.T) {
	shell := &exclShell{urls: []string{foURLA, foURLB}, excluded: map[string]bool{foURLB: true}}
	hs := newFoHarness(t, foOpts{shell: shell, net: []checkResult{checkMatched}})
	hs.escalate()
	hs.waitStatus("reconnecting 503", isReconnectingCode(ErrCodeServerUnreachable))
	hs.assertStillScheduled("after two tried-nothing rounds")
	if n := hs.provider.count(); n != 2 {
		t.Fatalf("NextURL calls = %d, want 2 (one round, one retry round)", n)
	}
}

// Review 4, I-2 residual: a target that leaves the schedule through a tick's
// Phase-B swap forgets the round's exclusions too (b answered code-less
// there and must not stay excluded for 10 min / the whole k2subs session).
func TestRetry_SwapExitResetsProvider(t *testing.T) {
	hs := newFoHarness(t, foOpts{
		fallbacks: map[string][]string{foURLA: {foURLB, foURLC}},
		net:       []checkResult{checkMatched},
	})
	hs.servers.mu.Lock()
	hs.servers.setupErrs["b.example"] = codeLessReject()
	hs.servers.setupErrs["c.example"] = codeLessReject()
	hs.servers.mu.Unlock()
	hs.escalate() // a dead, b and c code-less → scheduled
	hs.waitStatus("reconnecting 503", isReconnectingCode(ErrCodeServerUnreachable))
	tm := hs.nextTimer()

	// Center back for c only; b still answers code-less.
	hs.servers.mu.Lock()
	delete(hs.servers.setupErrs, "c.example")
	hs.servers.mu.Unlock()
	hs.servers.setAlive("c.example", true)
	tm.fire()
	eventually(t, "swapped to c on the tick", func() bool { return hs.targetURL() == foURLC })
	hs.waitStatus("connected", func(s Status) bool { return s.State == StateConnected && s.Error == nil })
	eventually(t, "exclusions forgotten on the swap exit", func() bool {
		hs.static.mu.Lock()
		defer hs.static.mu.Unlock()
		return len(hs.static.excluded) == 0
	})
}

package engine

// Flow-simulator findings (sim-fix-brief.md), on the harness's fake clock:
//
//   - FB: a current node that answers code-less (alive, but its own path to
//     Center is broken) used to pin its target to Phase A only, so a live
//     candidate was never tried. A code-less tick now runs Phase B at most
//     once per codeLessPhaseBEvery per target.
//   - PI: a Pause landing while a round is in flight aborts the round — no
//     further candidate setup, no swap, no pre-exhaust netcheck, no echo
//     escalate — and the target keeps (or takes) its place on the schedule;
//     Wake's immediate tick resumes it. PE: an echo signal while paused is
//     dropped.
//   - D2 / RW: a recovered wake (targetRecovered) also wakes a network hold;
//     on both paths it is muted while the latest Phase A was code-less and
//     after a recovered tick that failed.
//   - FA: a round with any code-less answer never counts toward the cap.

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kaitu-io/k2/wire"
)

func TestCodeLessPhaseBEveryIsTwoMinutes(t *testing.T) {
	if codeLessPhaseBEvery != 2*time.Minute {
		t.Fatalf("codeLessPhaseBEvery = %v, want 2m (sim-fix-brief ruling)", codeLessPhaseBEvery)
	}
}

// codeLessRaces makes every Phase-A race answer the code-less reject (the
// current node is alive, Center unreachable from it), counting races.
func (hs *foHarness) codeLessRaces() {
	raceTransportFn = func(ctx context.Context, cfg wire.Config, _ string, _ wire.DirectDialer, _ *wire.CookieJar) (*wire.RaceResult, error) {
		hs.servers.mu.Lock()
		hs.servers.races[cfg.Host]++
		hs.servers.mu.Unlock()
		return nil, fmt.Errorf("transport race: %w", codeLessReject())
	}
}

// setupTimes records the fake-clock time of every setup on host.
func (hs *foHarness) setupTimes(host string) func() []time.Time {
	var mu sync.Mutex
	var at []time.Time
	hs.servers.mu.Lock()
	hs.servers.onSetup = func(h string) {
		if h == host {
			mu.Lock()
			at = append(at, hs.clock.Now())
			mu.Unlock()
		}
	}
	hs.servers.mu.Unlock()
	return func() []time.Time {
		mu.Lock()
		defer mu.Unlock()
		return append([]time.Time(nil), at...)
	}
}

// fireUntil fires retry ticks until cond holds once a tick has been handled
// (a target that left the schedule arms no timer). Returns the elapsed time
// of the tick that satisfied cond.
func (r *tickRun) fireUntil(what string, cond func() bool, horizon time.Duration) time.Duration {
	hs := r.hs
	hs.t.Helper()
	for {
		at := r.fire()
		deadline := time.Now().Add(5 * time.Second)
		for {
			if cond() {
				return at
			}
			select {
			case tm := <-hs.timers:
				r.next = &tm
			case <-hs.done:
				hs.t.Fatalf("loop exited at %v waiting for %s", at, what)
			case <-time.After(time.Millisecond):
				if time.Now().After(deadline) {
					hs.t.Fatalf("tick at %v: neither re-armed nor %s", at, what)
				}
				continue
			}
			break
		}
		if cond() {
			return at
		}
		if at > horizon {
			hs.t.Fatalf("no %s by %v", what, at)
		}
	}
}

// FB (a): a global Center outage puts the target on the schedule (every
// node code-less); then Center becomes reachable from b again while the
// current node a keeps answering code-less (its own Center path is broken).
// The target must fail over to b within codeLessPhaseBEvery plus one tick
// spacing, and no code-less Phase B may run in its first 2 min on the
// schedule.
func TestCodeLess_NodeLocalCurrentFailsOverToLiveCandidate(t *testing.T) {
	diag := recordDiag(t)
	hs := newFoHarness(t, foOpts{
		fallbacks: map[string][]string{foURLA: {foURLB, foURLC}},
		net:       []checkResult{checkMatched},
	})
	hs.codeLessRaces()
	hs.servers.mu.Lock()
	hs.servers.setupErrs["b.example"] = codeLessReject()
	hs.servers.setupErrs["c.example"] = codeLessReject()
	hs.servers.mu.Unlock()
	bTimes := hs.setupTimes("b.example")
	hs.escalate()
	hs.waitStatus("reconnecting 503", isReconnectingCode(ErrCodeServerUnreachable))
	r := hs.startTicks()
	entryRounds := len(bTimes())
	if entryRounds != 1 {
		t.Fatalf("escalate rounds = %d, want 1", entryRounds)
	}
	for r.peek().at.Sub(r.start) < codeLessPhaseBEvery {
		if _, exited := r.tick(); exited {
			t.Fatal("engine failed during a Center outage")
		}
	}
	if n := len(bTimes()); n != entryRounds {
		t.Fatalf("code-less Phase-B rounds in the first 2 min on the schedule = %d, want 0", n-entryRounds)
	}
	// The outage goes on to ~5 min (code-less rounds stay scheduled).
	for r.peek().at.Sub(r.start) < 5*time.Minute {
		if _, exited := r.tick(); exited {
			t.Fatal("engine failed during a Center outage")
		}
		hs.assertExhaustedScheduled(fmt.Sprintf("tick at %v", r.elapsed()))
	}
	if n := len(bTimes()); n < 2 {
		t.Fatalf("Phase-B rounds by 5 min = %d, want ≥ 2 (code-less ticks run Phase B every 2 min)", n)
	}

	// Center reachable from b again; a still code-less.
	backAt := r.elapsed()
	hs.servers.mu.Lock()
	delete(hs.servers.setupErrs, "b.example")
	hs.servers.mu.Unlock()
	hs.servers.setAlive("b.example", true)
	at := r.fireUntil("failover to b", func() bool { return hs.targetURL() == foURLB }, 30*time.Minute)
	if lag := at - backAt; lag > codeLessPhaseBEvery+holdBackoff[len(holdBackoff)-1] {
		t.Fatalf("failed over %v after b came back, want ≤ %v (2 min + one tick spacing)",
			lag, codeLessPhaseBEvery+holdBackoff[len(holdBackoff)-1])
	}
	hs.waitStatus("connected", func(s Status) bool { return s.State == StateConnected && s.Error == nil })
	if hs.h.fatalCount() != 0 || hs.h.sawState(StateDisconnected) {
		t.Fatalf("OnOutboundFatal %d / disconnected %v, want neither", hs.h.fatalCount(), hs.h.sawState(StateDisconnected))
	}
	if n := len(diag.find("DIAG: outbound-failover", "")); n != 1 {
		t.Fatalf("DIAG: outbound-failover = %d, want 1", n)
	}
	rec := diag.find("DIAG: outbound-exhausted-recovered", "")
	if len(rec) != 1 || rec[0].attrs["via"] != "failover" {
		t.Fatalf("DIAG: outbound-exhausted-recovered = %+v, want one via=failover", rec)
	}
}

// FB (b): a global Center outage (current node and every candidate
// code-less) for 30 min: Phase-B rounds ≤ 30min/codeLessPhaseBEvery + the
// escalate round, at least codeLessPhaseBEvery apart, and the engine never
// fails.
func TestCodeLess_GlobalOutagePhaseBThrottled(t *testing.T) {
	hs := newFoHarness(t, foOpts{
		fallbacks: map[string][]string{foURLA: {foURLB, foURLC}},
		net:       []checkResult{checkMatched},
	})
	hs.codeLessRaces()
	hs.servers.mu.Lock()
	hs.servers.setupErrs["b.example"] = codeLessReject()
	hs.servers.setupErrs["c.example"] = codeLessReject()
	hs.servers.mu.Unlock()
	bTimes := hs.setupTimes("b.example")
	hs.escalate()
	hs.waitStatus("reconnecting 503", isReconnectingCode(ErrCodeServerUnreachable))
	r := hs.startTicks()
	for r.elapsed() < 30*time.Minute {
		if at, exited := r.tick(); exited {
			t.Fatalf("engine failed at %v during a global Center outage", at)
		}
	}
	hs.assertExhaustedScheduled("30 min global Center outage")
	ts := bTimes()
	if len(ts) > 15+1 {
		t.Fatalf("Phase-B rounds in 30 min = %d, want ≤ 16 (one per 2 min + the escalate round)", len(ts))
	}
	if len(ts) < 10 {
		t.Fatalf("Phase-B rounds in 30 min = %d, want ≥ 10 (code-less ticks still run Phase B)", len(ts))
	}
	for i := 1; i < len(ts); i++ {
		if gap := ts[i].Sub(ts[i-1]); gap < codeLessPhaseBEvery {
			t.Fatalf("Phase-B rounds %d and %d only %v apart, want ≥ %v", i-1, i, gap, codeLessPhaseBEvery)
		}
	}
}

// pauseOnSetup pauses the engine when host's candidate setup starts (before
// its verdict).
func (hs *foHarness) pauseOnSetup(host string) {
	hs.servers.mu.Lock()
	hs.servers.onSetup = func(h string) {
		if h == host {
			hs.e.Pause()
		}
	}
	hs.servers.mu.Unlock()
}

// wakeAndExpectFailover wakes the engine and expects the immediate wake tick
// (no timer fired) to fail over to want.
func (hs *foHarness) wakeAndExpectFailover(want string) {
	hs.t.Helper()
	hs.servers.mu.Lock()
	hs.servers.onSetup = nil
	hs.servers.mu.Unlock()
	hs.e.Wake()
	eventually(hs.t, "wake tick failed over to "+want, func() bool { return hs.targetURL() == want })
	hs.waitStatus("connected", func(s Status) bool { return s.State == StateConnected && s.Error == nil })
}

// PI (a), (c): Pause lands during a tick's Phase B. (a) after b's setup
// started: c is never set up. (c) during the last candidate's setup: the
// provider runs dry but no pre-exhaust netcheck is taken. Either way the
// target stays on the schedule on a, nothing is excluded that was not
// tried, and Wake's tick fails over.
func TestPause_MidTickPhaseBAborts(t *testing.T) {
	for _, tc := range []struct {
		name, pauseOn string
		live          string // the candidate alive at Wake
	}{
		{"(a) before the next candidate setup", "b.example", foURLC},
		{"(c) before the pre-exhaust netcheck", "c.example", foURLB},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hs := newFoHarness(t, foOpts{
				fallbacks: map[string][]string{foURLA: {foURLB, foURLC}},
				net:       []checkResult{checkMatched},
			})
			hs.escalate() // a, b, c dead → exhausted, scheduled
			hs.waitStatus("reconnecting 503", isReconnectingCode(ErrCodeServerUnreachable))
			r := hs.startTicks()
			setupsC := hs.setupCount("c.example")
			hs.pauseOnSetup(tc.pauseOn)
			var checksAtPause int
			hs.servers.mu.Lock()
			hook := hs.servers.onSetup
			hs.servers.onSetup = func(h string) {
				hook(h)
				if h == tc.pauseOn {
					checksAtPause = hs.net.callCount()
				}
			}
			hs.servers.mu.Unlock()
			if _, exited := r.tick(); exited {
				t.Fatal("loop exited")
			}
			if p, _ := hs.e.pauseState(); !p {
				t.Fatal("precondition: the tick's Phase B never reached the pause hook")
			}
			if tc.pauseOn == "b.example" {
				if n := hs.setupCount("c.example"); n != setupsC {
					t.Fatalf("c set up %d time(s) after Pause landed, want 0", n-setupsC)
				}
			}
			if n := hs.net.callCount(); n != checksAtPause {
				t.Fatalf("netchecks after Pause landed = %d, want 0", n-checksAtPause)
			}
			if u := hs.targetURL(); u != foURLA {
				t.Fatalf("target = %s while paused, want a", u)
			}
			if st, le := hs.engineState(); st != StateConnected || le != nil || hs.h.fatalCount() != 0 {
				t.Fatalf("engine = %s / %v, fatal %d", st, le, hs.h.fatalCount())
			}
			hs.servers.setAlive(hostOf(tc.live), true)
			hs.wakeAndExpectFailover(tc.live)
		})
	}
}

// PI (b): Pause lands while a candidate's setup is in flight and the setup
// then succeeds: the fresh TransportManager is closed, not swapped in. Wake's
// tick fails over.
func TestPause_SetupSucceedsWhilePausedNoSwap(t *testing.T) {
	hs := newFoHarness(t, foOpts{
		fallbacks: map[string][]string{foURLA: {foURLB}},
		net:       []checkResult{checkMatched},
	})
	var mu sync.Mutex
	var built []*wire.TransportManager
	inner := setupOutboundTransportFn
	setupOutboundTransportFn = func(ctx context.Context, via string, dd wire.DirectDialer, mode, ver string) (*outboundTransport, wire.Dialer, error) {
		ot, d, err := inner(ctx, via, dd, mode, ver)
		if err == nil {
			ot.tm.SetPrimary(&stubDialer{}, "quic-443")
			mu.Lock()
			built = append(built, ot.tm)
			mu.Unlock()
		}
		return ot, d, err
	}
	hs.escalate() // a, b dead → scheduled
	hs.waitStatus("reconnecting 503", isReconnectingCode(ErrCodeServerUnreachable))
	r := hs.startTicks()
	hs.servers.setAlive("b.example", true)
	hs.pauseOnSetup("b.example")
	if _, exited := r.tick(); exited {
		t.Fatal("loop exited")
	}
	if u := hs.targetURL(); u != foURLA {
		t.Fatalf("a setup that finished while paused was swapped in (target %s)", u)
	}
	mu.Lock()
	if len(built) != 1 || built[0].Primary() != nil {
		t.Fatalf("built TMs = %d; want 1, closed (primary released)", len(built))
	}
	mu.Unlock()
	hs.wakeAndExpectFailover(foURLB)
}

// PI (d): Pause lands during the Phase-A race that reaches the Phase-B
// threshold: escalate must not start (no pre-phaseB netcheck, no candidate
// setup). The target goes on the schedule; Wake's immediate tick fails over.
func TestPause_EchoEscalateEntryGated(t *testing.T) {
	hs := newFoHarness(t, foOpts{
		fallbacks: map[string][]string{foURLA: {foURLB}},
		net:       []checkResult{checkMatched},
	})
	hs.servers.setAlive("b.example", true)
	races := 0
	hs.servers.mu.Lock()
	hs.servers.onRace = func(context.Context, string) {
		races++ // loop goroutine only
		if races == reRaceFailThresholdForReplace {
			hs.e.Pause()
		}
	}
	hs.servers.mu.Unlock()
	hs.escalate()
	tm := hs.nextTimer() // the target joined the schedule
	if n := hs.net.callCount(); n != 0 {
		t.Fatalf("netchecks after Pause landed = %d, want 0 (escalate gated)", n)
	}
	if n := hs.setupsTotal(); n != 0 {
		t.Fatalf("candidate setups after Pause landed = %d, want 0", n)
	}
	if u := hs.targetURL(); u != foURLA {
		t.Fatalf("target = %s while paused, want a", u)
	}
	_ = tm // never fired: Wake ticks at once
	hs.servers.mu.Lock()
	hs.servers.onRace = nil
	hs.servers.mu.Unlock()
	hs.wakeAndExpectFailover(foURLB)
}

// Device D2 (716848aef): after a 106 hold the current node carried traffic
// 2s after the network came back, but the hold ended only at the next
// backoff tick ~29s later. targetRecovered for the held target wakes the
// hold at once (same throttle as the schedule: one tick per holdBackoff[0]
// since the last attempt); the tick re-judges and runs Phase A as a normal
// hold tick.
func TestHold_TargetRecoveredEndsHoldAtOnce(t *testing.T) {
	for _, tc := range []struct {
		name string
		net  checkResult // the verdict at the recovered tick
	}{
		{"netcheck online", checkMatched},
		{"netcheck still reads no-internet", checkNoResponse},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hs := newFoHarness(t, foOpts{
				fallbacks: map[string][]string{foURLA: {foURLB}},
				net:       []checkResult{checkNoResponse},
			})
			hm := &healthMonitor{}
			hs.e.wireHealthRecovered(hm)
			hs.escalate()
			hs.waitStatus("reconnecting 106", isReconnectingCode(ErrCodeNoInternet))
			tm := hs.nextTimer() // the 5s step; then on to the 30s step
			tm.fire()
			tm = hs.nextTimer()
			tm.fire()
			tm = hs.nextTimer()
			if tm.d != holdBackoff[len(holdBackoff)-1] {
				t.Fatalf("hold step = %v, want the 30s step", tm.d)
			}
			// The network comes back; 2s later a user dial through a answers.
			hs.net.set(tc.net)
			hs.servers.setAlive("a.example", true)
			hs.clock.advanceTo(hs.clock.Now().Add(holdBackoff[0]))
			hm.recovered(hs.target)
			hs.waitStatus("connected, hold ended", func(s Status) bool { return s.State == StateConnected && s.Error == nil })
			if hs.targetURL() != foURLA {
				t.Fatalf("target = %s, want a (its own node answered)", hs.targetURL())
			}
			if n := hs.setupsTotal(); n != 0 {
				t.Fatalf("candidate setups = %d, want 0", n)
			}
		})
	}
}

// The recovered hold wake is a tick, not a re-race signal: a burst within
// holdBackoff[0] of the last attempt does nothing, and a recovered signal
// for an outbound that is not held does nothing either.
func TestHold_TargetRecoveredIsRateLimited(t *testing.T) {
	hs := newFoHarness(t, foOpts{
		fallbacks: map[string][]string{foURLA: {foURLB}},
		net:       []checkResult{checkNoResponse},
	})
	hm := &healthMonitor{}
	hs.e.wireHealthRecovered(hm)
	hs.escalate()
	hs.waitStatus("reconnecting 106", isReconnectingCode(ErrCodeNoInternet))
	tm := hs.nextTimer()
	tm.fire()
	tm = hs.nextTimer()
	races, checks := hs.racesTotal(), hs.net.callCount()
	for i := 0; i < 20; i++ {
		hm.recovered(hs.target) // fake clock does not move: inside the gap
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond)
	if n, c := hs.racesTotal(), hs.net.callCount(); n != races || c != checks {
		t.Fatalf("recovered burst within %v of the last attempt: %d races, %d netchecks; want 0, 0",
			holdBackoff[0], n-races, c-checks)
	}
	// One past the gap: exactly one tick (Phase A fails, still 106).
	hs.clock.advanceTo(hs.clock.Now().Add(holdBackoff[0]))
	hm.recovered(hs.target)
	eventually(t, "one recovered tick", func() bool { return hs.racesTotal() == races+1 })
	_ = tm
	time.Sleep(20 * time.Millisecond)
	if n := hs.racesTotal(); n != races+1 {
		t.Fatalf("races after one recovered signal = %d, want 1", n-races)
	}
	hs.waitStatus("still 106", isReconnectingCode(ErrCodeNoInternet))
}

// PE: an echo re-race signal received while paused is dropped — no race, no
// escalate.
func TestPause_EchoSignalWhilePausedDropped(t *testing.T) {
	hs := newFoHarness(t, foOpts{
		fallbacks: map[string][]string{foURLA: {foURLB}},
		net:       []checkResult{checkMatched},
	})
	hs.servers.setAlive("b.example", true)
	hs.e.Pause()
	for i := 0; i < 2*reRaceFailThresholdForReplace; i++ {
		hs.signal()
	}
	time.Sleep(20 * time.Millisecond)
	if n, c, s := hs.racesTotal(), hs.net.callCount(), hs.setupsTotal(); n != 0 || c != 0 || s != 0 {
		t.Fatalf("while paused: %d races, %d netchecks, %d setups; want none", n, c, s)
	}
	if u := hs.targetURL(); u != foURLA {
		t.Fatalf("target = %s, want a", u)
	}
}

// FA: Center hangs, the current node is dead (transport), one candidate is
// dead and one answers code-less (metadata timeout). Every round mixes a
// code-less answer with transport failures: none may count toward the cap.
// 30 min: no fail; Center back → failover.
func TestExhaust_MixedCodeLessRoundsNeverCap(t *testing.T) {
	hs := newFoHarness(t, foOpts{
		fallbacks: map[string][]string{foURLA: {foURLB, foURLC}},
		net:       []checkResult{checkMatched},
	})
	hs.servers.mu.Lock()
	hs.servers.setupErrs["c.example"] = metadataTimeout() // b: dead (default)
	hs.servers.mu.Unlock()
	hs.escalate()
	hs.waitStatus("reconnecting 503", isReconnectingCode(ErrCodeServerUnreachable))
	r := hs.startTicks()
	for r.elapsed() < 30*time.Minute {
		if at, exited := r.tick(); exited {
			t.Fatalf("engine failed at %v: mixed code-less + dead rounds must not count", at)
		}
	}
	hs.assertExhaustedScheduled("30 min mixed rounds")
	hs.servers.mu.Lock()
	delete(hs.servers.setupErrs, "c.example")
	hs.servers.mu.Unlock()
	hs.servers.setAlive("c.example", true) // Center back
	r.fireUntil("failover to c", func() bool { return hs.targetURL() == foURLC }, 32*time.Minute)
	hs.waitStatus("connected", func(s Status) bool { return s.State == StateConnected && s.Error == nil })
	if hs.h.fatalCount() != 0 {
		t.Fatalf("OnOutboundFatal = %d", hs.h.fatalCount())
	}
}

// rwDriver runs the retry schedule for horizon on the fake clock and calls
// hm.recovered every `every` of fake time between ticks (an echo OK), waiting
// for the loop to handle each event. Returns timer-fired ticks and races.
func rwDriver(t *testing.T, hs *foHarness, hm *healthMonitor, idle *atomic.Int64, every, horizon time.Duration) (fires, races int) {
	t.Helper()
	settle := func(before int64) {
		eventually(t, "loop idle", func() bool { return idle.Load() > before })
	}
	cur := hs.nextTimer()
	drain := func() {
		for {
			select {
			case tm := <-hs.timers:
				cur = tm
			default:
				return
			}
		}
	}
	start := hs.clock.Now()
	races0 := hs.racesTotal()
	for hs.clock.Now().Sub(start) < horizon {
		// Wakes only up to the tick that was due when the interval began: a
		// wake tick re-arms (cur moves on), which must not stretch the window.
		due := cur.at
		for at := hs.clock.Now().Add(every); at.Before(due); at = hs.clock.Now().Add(every) {
			hs.clock.advanceTo(at)
			before := idle.Load()
			hm.recovered(hs.target)
			if len(hs.e.recoveredWake) == 0 && !hs.target.scheduled.Load() {
				t.Fatal("target left the schedule")
			}
			settle(before)
			drain()
		}
		before := idle.Load()
		cur.fire()
		fires++
		settle(before)
		drain()
	}
	return fires, hs.racesTotal() - races0
}

func newRWHarness(t *testing.T, fallbacks map[string][]string) (*foHarness, *healthMonitor, *atomic.Int64) {
	idle := &atomic.Int64{}
	hs := newFoHarness(t, foOpts{
		fallbacks: fallbacks,
		net:       []checkResult{checkMatched},
		idle:      func() { idle.Add(1) },
	})
	hm := &healthMonitor{}
	hs.e.wireHealthRecovered(hm)
	hs.drainOnce.Do(hs.drainSetups)
	return hs, hm, idle
}

// RW (a): Center hanging, node alive (every Phase A a metadata timeout),
// candidates code-less too, an echo OK every 16s for 30 min. The echo only
// re-proves the node is alive: no recovered wake may tick. Every race is a
// timer tick, and the tick count stays within the M4 cadence.
func TestRetrySchedule_RecoveredIgnoredWhileCodeLess(t *testing.T) {
	hs, hm, idle := newRWHarness(t, map[string][]string{foURLA: {foURLB, foURLC}})
	raceTransportFn = func(ctx context.Context, cfg wire.Config, _ string, _ wire.DirectDialer, _ *wire.CookieJar) (*wire.RaceResult, error) {
		hs.servers.mu.Lock()
		hs.servers.races[cfg.Host]++
		hs.servers.mu.Unlock()
		return nil, fmt.Errorf("transport race: %w", metadataTimeout())
	}
	hs.servers.mu.Lock()
	hs.servers.setupErrs["b.example"] = metadataTimeout()
	hs.servers.setupErrs["c.example"] = metadataTimeout()
	hs.servers.mu.Unlock()
	hs.escalate()
	hs.waitStatus("reconnecting 503", isReconnectingCode(ErrCodeServerUnreachable))
	setups0 := hs.setupsTotal()
	fires, races := rwDriver(t, hs, hm, idle, 16*time.Second, 30*time.Minute)
	if races != fires {
		t.Fatalf("races = %d over %d timer ticks: %d recovered wakes ticked a code-less target", races, fires, races-fires)
	}
	// M4 budget: 5,10,30,30… to 10 min (≤ 22 ticks), then 60s (20 ticks).
	if fires > 45 {
		t.Fatalf("timer ticks in 30 min = %d, want ≤ 45 (M4 cadence)", fires)
	}
	if s := hs.setupsTotal() - setups0; s > 2*15 {
		t.Fatalf("candidate setups = %d, want ≤ 30 (one 2-candidate round per 2 min)", s)
	}
	hs.assertExhaustedScheduled("30 min Center hang")
}

// RW (b): the node is dead at transport but its echo keeps answering (OK
// every 16s): a recovered wake ticks at most once per cadence interval — a
// failed recovered tick mutes further wakes until the next timer tick.
func TestRetrySchedule_FailedRecoveredTickMutesUntilNextTick(t *testing.T) {
	hs, hm, idle := newRWHarness(t, nil) // manual mode: a pure retry schedule
	hs.escalate()
	hs.waitStatus("reconnecting 503", isReconnectingCode(ErrCodeServerUnreachable))
	fires, races := rwDriver(t, hs, hm, idle, 16*time.Second, 30*time.Minute)
	extra := races - fires
	if extra < 1 {
		t.Fatalf("no recovered wake ticked (races %d, timer ticks %d): precondition", races, fires)
	}
	if extra > fires {
		t.Fatalf("recovered wakes ticked %d times over %d timer ticks, want ≤ one per interval", extra, fires)
	}
}

// RW on the hold path: (a) a held target whose Phase A was code-less
// ignores recovered wakes; (b) a failed recovered hold tick mutes further
// wakes until the next backoff tick.
func TestHold_RecoveredWakeMuting(t *testing.T) {
	t.Run("(b) failed recovered tick mutes", func(t *testing.T) {
		hs := newFoHarness(t, foOpts{
			fallbacks: map[string][]string{foURLA: {foURLB}},
			net:       []checkResult{checkNoResponse},
		})
		hm := &healthMonitor{}
		hs.e.wireHealthRecovered(hm)
		hs.escalate()
		hs.waitStatus("reconnecting 106", isReconnectingCode(ErrCodeNoInternet))
		cur := hs.nextTimer()
		races0 := hs.racesTotal()
		fires := 0
		for i := 0; i < 6; i++ {
			// Wakes every 6s (> the 5s throttle) inside the interval.
			due := cur.at // a wake tick re-arms; the window stays this interval
			for at := hs.clock.Now().Add(6 * time.Second); at.Before(due); at = hs.clock.Now().Add(6 * time.Second) {
				hs.clock.advanceTo(at)
				hm.recovered(hs.target)
				select {
				case tm := <-hs.timers: // the wake ticked and re-armed
					cur = tm
				case <-time.After(30 * time.Millisecond):
				}
			}
			cur.fire()
			fires++
			cur = hs.nextTimer()
		}
		extra := hs.racesTotal() - races0 - fires
		if extra < 1 || extra > fires {
			t.Fatalf("recovered hold ticks = %d over %d backoff ticks, want 1..%d", extra, fires, fires)
		}
	})
	t.Run("(a) code-less Phase A ignores wakes", func(t *testing.T) {
		hs := newFoHarness(t, foOpts{
			fallbacks: map[string][]string{foURLA: {foURLB}},
			net:       []checkResult{checkNoResponse},
		})
		hm := &healthMonitor{}
		hs.e.wireHealthRecovered(hm)
		hs.codeLessRaces()
		hs.escalate()
		hs.waitStatus("reconnecting 106", isReconnectingCode(ErrCodeNoInternet))
		cur := hs.nextTimer()
		races0 := hs.racesTotal()
		hs.clock.advanceTo(hs.clock.Now().Add(holdBackoff[0]))
		hm.recovered(hs.target)
		select {
		case <-hs.timers:
			t.Fatal("a recovered wake ticked a code-less held target")
		case <-time.After(30 * time.Millisecond):
		}
		if n := hs.racesTotal() - races0; n != 0 {
			t.Fatalf("races = %d, want 0", n)
		}
		_ = cur
	})
}

// PI: Pause lands during a tick's netcheck — the hold's and the schedule's:
// the tick stops there, no Phase-A race.
func TestPause_DuringTickNetcheckNoPhaseA(t *testing.T) {
	for _, tc := range []struct {
		name  string
		net   checkResult
		want  int
		after checkResult // what the paused netcheck reads
	}{
		{"network hold", checkNoResponse, ErrCodeNoInternet, checkNoResponse},
		{"network hold, network back", checkNoResponse, ErrCodeNoInternet, checkMatched},
		{"retry schedule", checkMatched, ErrCodeServerUnreachable, checkMatched},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hs := newFoHarness(t, foOpts{
				fallbacks: map[string][]string{foURLA: {foURLB}},
				net:       []checkResult{tc.net},
			})
			hs.escalate()
			hs.waitStatus("reconnecting", isReconnectingCode(tc.want))
			tm := hs.nextTimer()
			var pauseNext atomic.Bool
			hs.e.mu.Lock()
			inner := hs.e.netcheck
			hs.e.netcheck = func(ctx context.Context) ([]checkResult, []time.Duration) {
				if pauseNext.Load() {
					hs.e.Pause()
				}
				return inner(ctx)
			}
			hs.e.mu.Unlock()
			hs.net.set(tc.after)
			pauseNext.Store(true)
			races := hs.racesTotal()
			tm.fire()
			hs.nextTimer() // the tick ended and re-armed
			if p, _ := hs.e.pauseState(); !p {
				t.Fatal("precondition: the tick's netcheck never ran")
			}
			if n := hs.racesTotal(); n != races {
				t.Fatalf("Phase-A races after Pause landed in the netcheck = %d, want 0", n-races)
			}
		})
	}
}

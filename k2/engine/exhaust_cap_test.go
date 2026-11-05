package engine

// Task R1: a candidate exhaustion on a healthy network no longer disconnects
// at once. It goes on the retry schedule (reconnecting 503) and only fails
// after exhaustFailAfter of sustained online exhaustion; an account verdict
// (coded 401/402/403, sticky 401/402) still fails at once. Everything runs on
// the harness's fake clock (foClock moves only when a retry timer fires).

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kaitu-io/k2/wire"
)

// diagRecorder captures DIAG records for the exhaustion schedule.
type diagRecorder struct {
	mu   sync.Mutex
	recs []diagRec
}

type diagRec struct {
	msg   string
	level slog.Level
	attrs map[string]any
}

func (d *diagRecorder) Enabled(context.Context, slog.Level) bool { return true }
func (d *diagRecorder) Handle(_ context.Context, r slog.Record) error {
	if !strings.HasPrefix(r.Message, "DIAG: outbound-exhausted") && r.Message != "DIAG: transport-rerace" {
		return nil
	}
	rec := diagRec{msg: r.Message, level: r.Level, attrs: map[string]any{}}
	r.Attrs(func(a slog.Attr) bool {
		rec.attrs[a.Key] = a.Value.Any()
		return true
	})
	d.mu.Lock()
	d.recs = append(d.recs, rec)
	d.mu.Unlock()
	return nil
}
func (d *diagRecorder) WithAttrs([]slog.Attr) slog.Handler { return d }
func (d *diagRecorder) WithGroup(string) slog.Handler      { return d }

func recordDiag(t *testing.T) *diagRecorder {
	t.Helper()
	d := &diagRecorder{}
	prev := slog.Default()
	slog.SetDefault(slog.New(d))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return d
}

// find returns the records with message msg (and, for transport-rerace,
// reason == reason when reason != "").
func (d *diagRecorder) find(msg, reason string) []diagRec {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []diagRec
	for _, r := range d.recs {
		if r.msg != msg {
			continue
		}
		if reason != "" && r.attrs["reason"] != reason {
			continue
		}
		out = append(out, r)
	}
	return out
}

// tickRun drives the retry schedule on the fake clock and records the
// intervals of the ticks it fired.
type tickRun struct {
	hs        *foHarness
	start     time.Time
	intervals []time.Duration
	next      *foTimer // armed, not yet fired
}

func (hs *foHarness) startTicks() *tickRun {
	hs.drainOnce.Do(hs.drainSetups)
	return &tickRun{hs: hs, start: hs.clock.Now()}
}

// drainSetups discards setupDone notifications for the test's lifetime: a
// long run sets up far more candidates than the channel holds (16).
func (hs *foHarness) drainSetups() {
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-hs.servers.setupDone:
			case <-stop:
				return
			}
		}
	}()
	hs.t.Cleanup(func() {
		hs.cancel() // the loop stops setting up before the drain stops
		select {
		case <-hs.done:
		case <-time.After(5 * time.Second):
		}
		close(stop)
		<-done
	})
}

func (r *tickRun) elapsed() time.Duration { return r.hs.clock.Now().Sub(r.start) }

// peek returns the next armed retry timer without firing it.
func (r *tickRun) peek() foTimer {
	r.hs.t.Helper()
	if r.next == nil {
		tm := r.hs.nextTimer()
		r.next = &tm
	}
	return *r.next
}

// fire fires the next retry timer without waiting for the tick to finish;
// returns the tick's elapsed fake time.
func (r *tickRun) fire() time.Duration {
	r.hs.t.Helper()
	tm := r.peek()
	r.next = nil
	r.intervals = append(r.intervals, tm.d)
	tm.fire()
	return tm.at.Sub(r.start)
}

// tick fires the next retry timer and waits until the loop has handled it:
// the following timer is armed (exited=false) or the loop exited.
func (r *tickRun) tick() (at time.Duration, exited bool) {
	r.hs.t.Helper()
	at = r.fire()
	select {
	case tm := <-r.hs.timers:
		r.next = &tm
		return at, false
	case <-r.hs.done:
		return at, true
	case <-time.After(5 * time.Second):
		r.hs.t.Fatalf("tick at %v: neither re-armed nor exited", at)
		return at, false
	}
}

func (hs *foHarness) assertExhaustedScheduled(when string) {
	hs.t.Helper()
	if s, le := hs.engineState(); s != StateConnected || le != nil {
		hs.t.Fatalf("%s: engine = %s / %v, want connected / nil", when, s, le)
	}
	if hs.h.sawState(StateDisconnected) {
		hs.t.Fatalf("%s: status went disconnected", when)
	}
	if n := hs.h.fatalCount(); n != 0 {
		hs.t.Fatalf("%s: OnOutboundFatal = %d, want 0", when, n)
	}
	if s, _ := hs.h.last(); !isReconnectingCode(ErrCodeServerUnreachable)(s) {
		hs.t.Fatalf("%s: last status = %+v, want reconnecting/503", when, s)
	}
}

func (hs *foHarness) waitExit() {
	hs.t.Helper()
	select {
	case <-hs.done:
	case <-time.After(5 * time.Second):
		hs.t.Fatal("replace loop did not exit")
	}
}

func (hs *foHarness) assertFailed503Once(when string) {
	hs.t.Helper()
	st, le := hs.engineState()
	if st != StateDisconnected || le == nil || le.Code != ErrCodeServerUnreachable {
		hs.t.Fatalf("%s: engine = %s / %v, want disconnected / 503", when, st, le)
	}
	if n := hs.h.fatalCount(); n != 1 {
		hs.t.Fatalf("%s: OnOutboundFatal = %d, want exactly 1", when, n)
	}
}

// runUntilCapFails ticks an exhausted target until the engine fails, asserting
// it stays connected + reconnecting/503 at every tick before capFrom +
// exhaustFailAfter and fails on the first tick at or after it. Returns the
// elapsed time of the failing tick.
func (r *tickRun) runUntilCapFails(capFrom time.Duration, horizon time.Duration) time.Duration {
	hs := r.hs
	hs.t.Helper()
	for {
		at, exited := r.tick()
		if exited {
			if at-capFrom < exhaustFailAfter {
				hs.t.Fatalf("engine failed at %v, %v into the cap: before exhaustFailAfter", at, at-capFrom)
			}
			return at
		}
		if at-capFrom >= exhaustFailAfter {
			hs.t.Fatalf("tick at %v (%v into the cap) did not fail", at, at-capFrom)
		}
		hs.assertExhaustedScheduled(fmt.Sprintf("tick at %v", at))
		if at > horizon {
			hs.t.Fatalf("no fail by %v", at)
		}
	}
}

func TestExhaustFailAfterIsTenMinutes(t *testing.T) {
	if exhaustFailAfter != 10*time.Minute {
		t.Fatalf("exhaustFailAfter = %v, want 10m (R1 brief)", exhaustFailAfter)
	}
}

// All candidates dead on an online network: no fail for 10 min, connected +
// reconnecting/503 throughout, ticks 5/10/30s; at ≥10 min OnOutboundFatal +
// fail(503) exactly once.
func TestExhaust_AllDeadOnlineFailsOnlyAtCap(t *testing.T) {
	diag := recordDiag(t)
	hs := newFoHarness(t, foOpts{
		fallbacks: map[string][]string{foURLA: {foURLB, foURLC}},
		net:       []checkResult{checkMatched},
	})
	hs.escalate()
	hs.waitStatus("reconnecting 503", isReconnectingCode(ErrCodeServerUnreachable))
	ex := diag.find("DIAG: outbound-exhausted", "")
	if len(ex) != 1 || ex[0].level != slog.LevelWarn || ex[0].attrs["outbound"] != "k2v5-a.example" ||
		fmt.Sprint(ex[0].attrs["tried"]) != "2" || ex[0].attrs["err"] == nil {
		t.Fatalf("DIAG: outbound-exhausted = %+v, want one Warn {outbound k2v5-a.example, tried 2, err}", ex)
	}
	hs.assertExhaustedScheduled("after escalate")

	r := hs.startTicks()
	failedAt := r.runUntilCapFails(0, 20*time.Minute)
	hs.assertFailed503Once("at the cap")
	if failedAt < exhaustFailAfter || failedAt > exhaustFailAfter+30*time.Second {
		t.Fatalf("failed at %v, want in [10m, 10m30s]", failedAt)
	}
	want := []time.Duration{5 * time.Second, 10 * time.Second, 30 * time.Second}
	for i, d := range r.intervals {
		w := want[min(i, len(want)-1)]
		if d != w {
			t.Fatalf("tick %d interval = %v, want %v (5→10→30s)", i, d, w)
		}
	}
	fatal := diag.find("DIAG: outbound-exhausted-fatal", "")
	if len(fatal) != 1 || fatal[0].level != slog.LevelError || fatal[0].attrs["outbound"] != "k2v5-a.example" {
		t.Fatalf("DIAG: outbound-exhausted-fatal = %+v, want one Error for k2v5-a.example", fatal)
	}
	if s, ok := fatal[0].attrs["elapsedS"].(int64); !ok || s < 600 {
		t.Fatalf("fatal elapsedS = %v, want ≥ 600", fatal[0].attrs["elapsedS"])
	}
	if n := len(diag.find("DIAG: transport-rerace", "exhausted-retry")); n != len(r.intervals) {
		t.Fatalf("transport-rerace reason=exhausted-retry = %d, want one per tick (%d)", n, len(r.intervals))
	}
	if n := len(diag.find("DIAG: transport-rerace", "wait-tick")); n != 0 {
		t.Fatalf("transport-rerace reason=wait-tick = %d on an exhausted target, want 0", n)
	}
	if n := len(diag.find("DIAG: outbound-exhausted-recovered", "")); n != 0 {
		t.Fatalf("recovered logged %d times without a recovery", n)
	}
}

// One candidate comes back at minute 4: fail over, leave the schedule
// (recovered via=failover), and a later death restarts the cap from zero.
func TestExhaust_CandidateBackAtMinute4FailsOverAndCapRestarts(t *testing.T) {
	diag := recordDiag(t)
	hs := newFoHarness(t, foOpts{
		fallbacks: map[string][]string{foURLA: {foURLB, foURLC}},
		net:       []checkResult{checkMatched},
	})
	hs.escalate()
	hs.waitStatus("reconnecting 503", isReconnectingCode(ErrCodeServerUnreachable))
	r := hs.startTicks()
	for r.peek().at.Sub(r.start) < 4*time.Minute {
		r.tick()
		hs.assertExhaustedScheduled("before minute 4")
	}
	hs.servers.setAlive("b.example", true)
	r.fire()
	eventually(t, "failed over to b", func() bool { return hs.targetURL() == foURLB })
	hs.waitStatus("connected, no hold", func(s Status) bool { return s.State == StateConnected && s.Error == nil })
	rec := diag.find("DIAG: outbound-exhausted-recovered", "")
	if len(rec) != 1 || rec[0].level != slog.LevelInfo || rec[0].attrs["via"] != "failover" ||
		rec[0].attrs["outbound"] == nil {
		t.Fatalf("DIAG: outbound-exhausted-recovered = %+v, want one Info via=failover", rec)
	}
	if s, ok := rec[0].attrs["elapsedS"].(int64); !ok || s < 240 || s > 270 {
		t.Fatalf("recovered elapsedS = %v, want ≈ 240", rec[0].attrs["elapsedS"])
	}
	if hs.h.fatalCount() != 0 || hs.h.sawState(StateDisconnected) {
		t.Fatal("disconnected / fatal during the run")
	}

	// b dies 5 min later (a and c still dead): a new exhaustion run, its cap
	// counted from the new entry, not from the first one.
	hs.clock.advanceTo(hs.clock.Now().Add(5 * time.Minute))
	hs.servers.setAlive("b.example", false)
	hs.escalate()
	hs.waitStatus("reconnecting 503 again", isReconnectingCode(ErrCodeServerUnreachable))
	if n := len(diag.find("DIAG: outbound-exhausted", "")); n != 2 {
		t.Fatalf("DIAG: outbound-exhausted = %d, want 2 (one per run)", n)
	}
	r2 := hs.startTicks()
	failedAt := r2.runUntilCapFails(0, 20*time.Minute)
	if failedAt < exhaustFailAfter || failedAt > exhaustFailAfter+30*time.Second {
		t.Fatalf("second run failed %v after its entry, want in [10m, 10m30s]: the cap did not restart", failedAt)
	}
	hs.assertFailed503Once("second run's cap")
}

// Offline (verdict 106) for 6 min in the middle: offline time never counts —
// the cap is measured from the first online tick after the outage.
func TestExhaust_OfflineTimeDoesNotCount(t *testing.T) {
	hs := newFoHarness(t, foOpts{
		fallbacks: map[string][]string{foURLA: {foURLB, foURLC}},
		net:       []checkResult{checkMatched},
	})
	hs.escalate()
	hs.waitStatus("reconnecting 503", isReconnectingCode(ErrCodeServerUnreachable))
	r := hs.startTicks()
	// Online until 2 min, offline from then for 6 min.
	var backOnline time.Duration
	for backOnline == 0 {
		at := r.peek().at.Sub(r.start)
		if at >= 2*time.Minute && at < 8*time.Minute {
			hs.net.set(checkNoResponse)
		} else {
			hs.net.set(checkMatched)
			if at >= 8*time.Minute {
				backOnline = at
			}
		}
		if _, exited := r.tick(); exited {
			t.Fatalf("tick at %v: engine failed", at)
		}
		if s, le := hs.engineState(); s != StateConnected || le != nil {
			t.Fatalf("tick at %v: engine = %s / %v, want connected", at, s, le)
		}
	}
	if total := backOnline; total < 8*time.Minute {
		t.Fatalf("precondition: back online at %v", total)
	}
	if hs.h.sawCode(ErrCodeNoInternet) == false {
		t.Fatal("precondition: the offline ticks never showed 106")
	}
	hs.waitStatus("back to reconnecting 503", isReconnectingCode(ErrCodeServerUnreachable))
	failedAt := r.runUntilCapFails(backOnline, 30*time.Minute)
	hs.assertFailed503Once("cap after the offline stretch")
	if failedAt-backOnline < exhaustFailAfter || failedAt-backOnline > exhaustFailAfter+30*time.Second {
		t.Fatalf("failed %v after back-online (%v), want in [10m, 10m30s]", failedAt-backOnline, backOnline)
	}
}

// Account verdicts still fail at once, without the schedule.
func TestExhaust_AccountVerdictFailsAtOnce(t *testing.T) {
	t.Run("every candidate coded 401", func(t *testing.T) {
		hs := newFoHarness(t, foOpts{
			fallbacks: map[string][]string{foURLA: {foURLB, foURLC}},
			net:       []checkResult{checkMatched},
		})
		hs.servers.mu.Lock()
		hs.servers.setupErrs["b.example"] = codedReject(ErrCodeAuthRejected)
		hs.servers.setupErrs["c.example"] = codedReject(ErrCodeAuthRejected)
		hs.servers.mu.Unlock()
		hs.escalate()
		hs.waitExit()
		if _, le := hs.engineState(); le == nil || le.Code != ErrCodeAuthRejected {
			t.Fatalf("lastError = %v, want 401", le)
		}
		if n := hs.h.fatalCount(); n != 1 {
			t.Fatalf("OnOutboundFatal = %d, want 1", n)
		}
		select {
		case tm := <-hs.timers:
			t.Fatalf("a retry timer (%v) was armed: an account verdict must not be scheduled", tm.d)
		default:
		}
	})
	t.Run("sticky 402", func(t *testing.T) {
		hs := newFoHarness(t, foOpts{
			fallbacks: map[string][]string{foURLA: {foURLB, foURLC}},
			net:       []checkResult{checkMatched},
		})
		hs.e.mu.Lock()
		hs.e.lastError = newEngineError(ErrCodePaymentRequired, "membership expired")
		hs.e.mu.Unlock()
		hs.escalate()
		hs.waitExit()
		if _, le := hs.engineState(); le == nil || le.Code != ErrCodePaymentRequired {
			t.Fatalf("lastError = %v, want the sticky 402", le)
		}
		if n := hs.h.fatalCount(); n != 1 {
			t.Fatalf("OnOutboundFatal = %d, want 1", n)
		}
	})
	t.Run("one candidate 402, one dead: scheduled, cap reports the 402", func(t *testing.T) {
		hs := newFoHarness(t, foOpts{
			fallbacks: map[string][]string{foURLA: {foURLB, foURLC}},
			net:       []checkResult{checkMatched},
		})
		hs.servers.mu.Lock()
		hs.servers.setupErrs["b.example"] = codedReject(ErrCodePaymentRequired)
		hs.servers.mu.Unlock()
		hs.escalate()
		hs.waitStatus("reconnecting 503", isReconnectingCode(ErrCodeServerUnreachable))
		hs.startTicks().runUntilCapFails(0, 20*time.Minute)
		if _, le := hs.engineState(); le == nil || le.Code != ErrCodePaymentRequired {
			t.Fatalf("lastError at the cap = %v, want the candidate's 402 over 503", le)
		}
	})
}

// Manual mode on a dead node: retries forever, no cap (30 min fake).
func TestExhaust_ManualModeNeverFails(t *testing.T) {
	diag := recordDiag(t)
	hs := newFoHarness(t, foOpts{net: []checkResult{checkMatched}})
	hs.escalate()
	hs.waitStatus("reconnecting 503", isReconnectingCode(ErrCodeServerUnreachable))
	r := hs.startTicks()
	for r.elapsed() < 30*time.Minute {
		if _, exited := r.tick(); exited {
			t.Fatalf("engine failed at %v in manual mode", r.elapsed())
		}
	}
	hs.assertExhaustedScheduled("30 min, manual mode")
	if n := len(diag.find("DIAG: outbound-exhausted", "")); n != 0 {
		t.Fatalf("manual mode logged outbound-exhausted %d times", n)
	}
	if n := len(diag.find("DIAG: transport-rerace", "wait-tick")); n != len(r.intervals) {
		t.Fatalf("transport-rerace wait-tick = %d, want %d (manual keeps wait-tick)", n, len(r.intervals))
	}
}

// S15 shape: the current node is dead at transport, the candidates complete
// their handshake and the metadata verdict times out (Center hanging). R1
// review M-2 ruling: a round whose candidates are all code-less does not
// count — a Center outage of any length never disconnects, even with the
// current node dead. Once Center answers, the next tick fails over.
func TestExhaust_S15CenterOutageDeadNodeNeverFailsThenFailsOver(t *testing.T) {
	hs := newFoHarness(t, foOpts{
		fallbacks: map[string][]string{foURLA: {foURLB, foURLC}},
		net:       []checkResult{checkMatched},
	})
	hs.servers.mu.Lock()
	hs.servers.setupErrs["b.example"] = metadataTimeout()
	hs.servers.setupErrs["c.example"] = metadataTimeout()
	hs.servers.mu.Unlock()
	hs.escalate()
	hs.waitStatus("reconnecting 503", isReconnectingCode(ErrCodeServerUnreachable))
	r := hs.startTicks()
	for r.elapsed() < 30*time.Minute {
		if at, exited := r.tick(); exited {
			t.Fatalf("engine failed at %v during a Center outage (current node dead)", at)
		}
		hs.assertExhaustedScheduled(fmt.Sprintf("tick at %v", r.elapsed()))
	}
	// Center answers again: b admits.
	hs.servers.mu.Lock()
	delete(hs.servers.setupErrs, "b.example")
	hs.servers.mu.Unlock()
	hs.servers.setAlive("b.example", true)
	r.fire()
	eventually(t, "failed over to b", func() bool { return hs.targetURL() == foURLB })
	hs.waitStatus("connected", func(s Status) bool { return s.State == StateConnected && s.Error == nil })
	if hs.h.fatalCount() != 0 {
		t.Fatalf("OnOutboundFatal = %d, want 0", hs.h.fatalCount())
	}
}

// Every candidate code-less, the current node dead at transport (the
// k2subs/static Center-outage shape with a refusal instead of a hang): same
// ruling, never caps.
func TestExhaust_AllCandidatesCodeLessDeadNodeNeverCaps(t *testing.T) {
	hs := newFoHarness(t, foOpts{
		fallbacks: map[string][]string{foURLA: {foURLB, foURLC}},
		net:       []checkResult{checkMatched},
	})
	hs.servers.mu.Lock()
	hs.servers.setupErrs["b.example"] = codeLessReject()
	hs.servers.setupErrs["c.example"] = codeLessReject()
	hs.servers.mu.Unlock()
	hs.escalate()
	hs.waitStatus("reconnecting 503", isReconnectingCode(ErrCodeServerUnreachable))
	r := hs.startTicks()
	for r.elapsed() < 30*time.Minute {
		if at, exited := r.tick(); exited {
			t.Fatalf("engine failed at %v: all-code-less rounds must not count", at)
		}
	}
	hs.assertExhaustedScheduled("30 min")
}

// R1 review I-1: the tick's first verdict is online, then the network dies
// during its Phase B (pre-exhaust verdict offline) — on the tick that would
// cross the cap. No fail; the hold shows that verdict's 106; the cap clock
// is cleared, so the cap restarts from the next online tick.
func TestExhaust_OfflineMidTickNeverCapsAndClearsClock(t *testing.T) {
	hs := newFoHarness(t, foOpts{
		fallbacks: map[string][]string{foURLA: {foURLB, foURLC}},
		net:       []checkResult{checkMatched},
	})
	hs.escalate()
	hs.waitStatus("reconnecting 503", isReconnectingCode(ErrCodeServerUnreachable))
	r := hs.startTicks()
	for r.peek().at.Sub(r.start) < exhaustFailAfter {
		if _, exited := r.tick(); exited {
			t.Fatal("failed before the cap")
		}
	}
	// The cap tick: the network dies while it tries the candidates.
	hs.servers.mu.Lock()
	hs.servers.onSetup = func(host string) {
		if host == "c.example" {
			hs.net.set(checkNoResponse)
		}
	}
	hs.servers.mu.Unlock()
	at, exited := r.tick()
	if exited {
		t.Fatalf("tick at %v failed the engine although its pre-exhaust verdict was offline", at)
	}
	if s, le := hs.engineState(); s != StateConnected || le != nil {
		t.Fatalf("engine = %s / %v, want connected", s, le)
	}
	hs.waitStatus("reconnecting 106 from the mid-tick verdict", isReconnectingCode(ErrCodeNoInternet))
	if hs.h.fatalCount() != 0 {
		t.Fatalf("OnOutboundFatal = %d", hs.h.fatalCount())
	}
	// Back online: the clock restarts at the next tick, so 10 more minutes.
	hs.servers.mu.Lock()
	hs.servers.onSetup = nil
	hs.servers.mu.Unlock()
	hs.net.set(checkMatched)
	restart := r.peek().at.Sub(r.start)
	failedAt := r.runUntilCapFails(restart, 30*time.Minute)
	if failedAt-restart != exhaustFailAfter {
		t.Fatalf("failed %v after the restart tick, want exactly %v: the clock was not cleared", failedAt-restart, exhaustFailAfter)
	}
	hs.assertFailed503Once("cap after the mid-tick outage")
}

// lockedShell is a k2subs-shaped shell provider whose owned URL set a test
// can change (a subscription refresh dropping a URL).
type lockedShell struct {
	mu   sync.Mutex
	owns map[string]bool
}

func (s *lockedShell) OwnsURL(u string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.owns[u]
}
func (s *lockedShell) NextURL(cur string, _ error) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for u := range s.owns {
		if u != cur {
			return u, nil
		}
	}
	return "", errors.New("shell: exhausted")
}
func (s *lockedShell) drop(u string) {
	s.mu.Lock()
	delete(s.owns, u)
	s.mu.Unlock()
}

func (hs *foHarness) fromShell() bool {
	hs.e.mu.Lock()
	defer hs.e.mu.Unlock()
	return hs.target.fromShell
}

// R1 review M-4: a target whose URL the shell handed out, after a
// subscription refresh dropped that URL, answers ErrNoCandidates. That is an
// exhaustion — capped, then OnOutboundFatal (k2r rebuilds from a fresh
// subscription; the desktop daemon only logs) — not manual mode. A static-only target with the same
// answer keeps retrying forever (TestExhaust_ManualModeNeverFails).
func TestExhaust_ShellDroppedURLIsCapped(t *testing.T) {
	diag := recordDiag(t)
	shell := &lockedShell{owns: map[string]bool{foURLA: true}}
	hs := newFoHarness(t, foOpts{shell: shell, net: []checkResult{checkMatched}})
	hs.e.mu.Lock()
	hs.target.fromShell = true // as Start marks it (shellOrigins)
	hs.e.mu.Unlock()
	shell.drop(foURLA) // refresh: the tunnel's URL is gone
	hs.escalate()
	hs.waitStatus("reconnecting 503", isReconnectingCode(ErrCodeServerUnreachable))
	if ex := diag.find("DIAG: outbound-exhausted", ""); len(ex) != 1 || fmt.Sprint(ex[0].attrs["tried"]) != "0" {
		t.Fatalf("DIAG: outbound-exhausted = %+v, want one with tried 0", ex)
	}
	hs.startTicks().runUntilCapFails(0, 20*time.Minute)
	hs.assertFailed503Once("cap for a dropped shell URL")
}

// The origin follows the swap: a shell-handed URL marks the target
// fromShell, a static fallback clears it.
func TestFromShellFollowsSwap(t *testing.T) {
	shell := &lockedShell{owns: map[string]bool{foURLA: true, foURLB: true}}
	hs := newFoHarness(t, foOpts{shell: shell, net: []checkResult{checkMatched}})
	hs.servers.setAlive("b.example", true)
	hs.escalate()
	eventually(t, "failed over to b (shell)", func() bool { return hs.targetURL() == foURLB })
	if !hs.fromShell() {
		t.Fatal("fromShell = false after a swap to a shell URL")
	}

	hs2 := newFoHarness(t, foOpts{
		fallbacks: map[string][]string{foURLA: {foURLB}},
		shell:     &lockedShell{owns: map[string]bool{foURLC: true}},
		net:       []checkResult{checkMatched},
	})
	hs2.e.mu.Lock()
	hs2.target.fromShell = true
	hs2.e.mu.Unlock()
	hs2.servers.setAlive("b.example", true)
	hs2.escalate()
	eventually(t, "failed over to b (static)", func() bool { return hs2.targetURL() == foURLB })
	if hs2.fromShell() {
		t.Fatal("fromShell = true after a swap to a static fallback")
	}
}

func TestShellOrigins(t *testing.T) {
	shell := &lockedShell{owns: map[string]bool{foURLB: true, foURLC: true}}
	c := newCompositeProvider(newStaticProvider(map[string][]string{foURLA: {foURLC}}, time.Now), shell)
	a, b, cc := &outboundTransport{url: foURLA}, &outboundTransport{url: foURLB}, &outboundTransport{url: foURLC}
	got := shellOrigins(c, []*outboundTransport{a, b, cc})
	if got[a] || !got[b] || got[cc] {
		t.Fatalf("shellOrigins = a:%v b:%v c:%v, want false/true/false (static wins for c)", got[a], got[b], got[cc])
	}
	if shellOrigins(newCompositeProvider(newStaticProvider(nil, time.Now), nil), []*outboundTransport{b})[b] {
		t.Fatal("no shell: fromShell must be false")
	}
}

// Concern-4 shape: a shell whose exclusions cannot be Reset, everything
// excluded — every round tries nothing. Never fails before the cap.
func TestExhaust_AllExcludedShellNeverFailsBeforeCap(t *testing.T) {
	shell := &exclShell{urls: []string{foURLA, foURLB}, excluded: map[string]bool{foURLB: true}}
	hs := newFoHarness(t, foOpts{shell: shell, net: []checkResult{checkMatched}})
	hs.escalate()
	hs.waitStatus("reconnecting 503", isReconnectingCode(ErrCodeServerUnreachable))
	failedAt := hs.startTicks().runUntilCapFails(0, 20*time.Minute)
	if failedAt < exhaustFailAfter {
		t.Fatalf("failed at %v, before the cap", failedAt)
	}
}

// A Center outage on a live node (Phase A code-less, M-3: no Phase B): the
// node answers, so it is not dead — those ticks never count toward the cap,
// and a 30 min outage never disconnects (review C1).
func TestExhaust_CenterOutageLiveNodeNeverCaps(t *testing.T) {
	hs := newFoHarness(t, foOpts{
		fallbacks: map[string][]string{foURLA: {foURLB, foURLC}},
		net:       []checkResult{checkMatched},
	})
	raceTransportFn = func(ctx context.Context, cfg wire.Config, _ string, _ wire.DirectDialer, _ *wire.CookieJar) (*wire.RaceResult, error) {
		return nil, fmt.Errorf("transport race: %w", codeLessReject())
	}
	hs.servers.mu.Lock()
	hs.servers.setupErrs["b.example"] = codeLessReject()
	hs.servers.setupErrs["c.example"] = codeLessReject()
	hs.servers.mu.Unlock()
	hs.escalate()
	hs.waitStatus("reconnecting 503", isReconnectingCode(ErrCodeServerUnreachable))
	r := hs.startTicks()
	for r.elapsed() < 30*time.Minute {
		if _, exited := r.tick(); exited {
			t.Fatalf("engine failed at %v during a Center outage on a live node", r.elapsed())
		}
	}
	hs.assertExhaustedScheduled("30 min Center outage, live node")
}

// Recovery through Phase A on the current node logs via=phaseA.
func TestExhaust_RecoveredViaPhaseA(t *testing.T) {
	diag := recordDiag(t)
	hs := newFoHarness(t, foOpts{
		fallbacks: map[string][]string{foURLA: {foURLB, foURLC}},
		net:       []checkResult{checkMatched},
	})
	hs.escalate()
	hs.waitStatus("reconnecting 503", isReconnectingCode(ErrCodeServerUnreachable))
	r := hs.startTicks()
	r.tick()
	r.tick()
	hs.servers.setAlive("a.example", true)
	r.fire()
	hs.waitStatus("connected", func(s Status) bool { return s.State == StateConnected && s.Error == nil })
	rec := diag.find("DIAG: outbound-exhausted-recovered", "")
	if len(rec) != 1 || rec[0].attrs["via"] != "phaseA" {
		t.Fatalf("DIAG: outbound-exhausted-recovered = %+v, want one via=phaseA", rec)
	}
	if s, ok := rec[0].attrs["elapsedS"].(int64); !ok || s != 45 {
		t.Fatalf("recovered elapsedS = %v, want 45 (5+10+30)", rec[0].attrs["elapsedS"])
	}
	if hs.targetURL() != foURLA {
		t.Fatalf("target = %s, want a", hs.targetURL())
	}
}

package engine

import (
	"context"
	"errors"
	"log/slog"
	"net/netip"
	"sync"
	"time"

	mdns "github.com/miekg/dns"

	"github.com/kaitu-io/k2/wire"
)

// Hold — waiting for the user's network inside outboundReplaceLoop.
//
// When a node looks dead, the replace loop first asks judgeNet whether the
// user's own network works. If it does not (interface down / no internet /
// captive portal) the engine holds: the tunnel and TUN stay up, status reads
// reconnecting + the verdict's reason (holdErr, see buildStatusLocked), and no
// replacement candidate is spent. The hold ends when judgeNet says online.
//
// Everything here runs on the replace loop's single goroutine. e.mu sections
// only read or write fields; netcheck I/O, netCoordinator calls and status
// pushes happen outside it.

// holdBackoff is the hold's re-judge cadence without network events: 5s, 10s,
// 30s, then 30s forever (the last entry repeats). No upper bound on the hold —
// the user can always disconnect.
var holdBackoff = []time.Duration{5 * time.Second, 10 * time.Second, 30 * time.Second}

// exhaustFailAfter is how long a target may stay continuously exhausted on an
// online network before the engine gives up on it: OnOutboundFatal +
// fail(503) (failExhausted). Before that an exhaustion only puts the target
// on the retry schedule (R1, spec §4.2 amendment 2026-11-05).
const exhaustFailAfter = 10 * time.Minute

// realHoldTimer is the production holdTimerFn.
func realHoldTimer(d time.Duration) (<-chan time.Time, func()) {
	t := time.NewTimer(d)
	return t.C, func() { t.Stop() }
}

// judgeNet decides whether the user's network reaches the internet, from the
// cheapest evidence first (netCoordinator interface state, passive direct
// dials within passiveFreshness — skipped for pre-exhaust — then the active
// netcheck), and logs one DIAG: netcheck line. trigger: pre-phaseB,
// pre-exhaust, wait-tick, net-event, echo-signal.
//
// If ctx is cancelled mid-check the active probes all report no response and
// the verdict reads no-internet — callers must check ctx.Err() first.
func (e *Engine) judgeNet(ctx context.Context, trigger string) netVerdict {
	e.mu.Lock()
	nc := e.netCoord
	dt := e.directTrack
	run := e.netcheck
	targets := e.netcheckTargets
	e.mu.Unlock()

	// pre-exhaust always probes: passive evidence may predate the outage.
	ev := netEvidence{Now: time.Now(), SkipPassive: trigger == "pre-exhaust"}
	if nc != nil {
		ev.IfaceDown = !nc.isNetworkUp()
	}
	if dt != nil {
		ev.LastDirectOK = dt.LastOK()
	}
	var rs []checkResult
	var ds []time.Duration
	start := time.Now()
	v, evidence := decideNet(ev, func() []checkResult {
		if run != nil {
			rs, ds = run(ctx)
		}
		return rs
	})
	attrs := []any{
		"trigger", trigger,
		"result", int(v),
		"verdict", v.String(),
		"evidence", evidence,
		"checks", formatCheckResults(targets, rs, ds),
		"ms", time.Since(start).Milliseconds(),
	}
	if v == netOnline {
		slog.Info("DIAG: netcheck", attrs...)
	} else {
		slog.Warn("DIAG: netcheck", attrs...)
	}
	return v
}

// setHold sets (non-nil) or clears (nil) the hold reason and pushes the
// resulting status. A no-op once the session is over (ctx cancelled by
// Stop/fail, or state no longer connected): Stop and fail clear holdErr
// themselves, and a late write must never land on a following session.
// Skips the push when the reason is unchanged.
func (e *Engine) setHold(ctx context.Context, ee *EngineError) {
	e.mu.Lock()
	if ctx.Err() != nil || e.state != StateConnected {
		e.mu.Unlock()
		return
	}
	if sameHold(e.holdErr, ee) {
		e.mu.Unlock()
		return
	}
	e.setHoldLocked(ee)
	handler := e.handler
	status := e.buildStatusLocked()
	e.mu.Unlock()
	if handler != nil {
		handler.OnStatus(status)
	}
}

func sameHold(a, b *EngineError) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Code == b.Code
}

// holdOutcome is how a network hold ended.
type holdOutcome int

const (
	holdOnline    holdOutcome = iota // judgeNet said online; caller re-races the current node
	holdRecovered                    // a wait-tick Phase-A reconnected the current node; done
	holdExit                         // ctx cancelled or the engine failed; the loop must exit
)

// noCandidatesError is the wait reason when the network is fine but an
// outbound has no replacement: the node itself is unreachable (503), and the
// engine keeps retrying it rather than disconnecting.
func noCandidatesError(name string) *EngineError {
	return newEngineError(ErrCodeServerUnreachable,
		"outbound "+name+" unreachable and has no replacement candidates; retrying")
}

// holdUntilOnline holds on verdict v (v.blocksFailover()): the user's network is
// broken, which affects every outbound alike, so the replace loop blocks here.
// It re-checks on every wake — backoff timer (holdBackoff) or a network
// event via holdWake. Each wake first re-judges the network:
//   - online / unknown (e.g. interface back up, no netcheck targets) → show
//     restore (the hold the loop had before: nil, or the no-candidates 503
//     while retries are pending), return holdOnline (the caller re-races).
//   - otherwise → one Phase-A on the current node (reason=wait-tick),
//     whatever the verdict says: the verdict can be wrong (netcheck targets
//     blocked on this network while the node still answers) and a node that
//     answers proves the session fine → show restore, holdRecovered. A 401/402 fails the engine → holdExit. Any other
//     failure keeps holding with the re-judged reason (pushed on change).
//
// ctx cancelled → holdExit; Stop/fail own clearing the hold then. The
// netcheck and the Phase-A run under ctx, so Stop interrupts them too.
//
// A network event restarts the backoff at its first step: a fresh network
// often needs a few seconds (DHCP, portal redirect) before it answers.
func (e *Engine) holdUntilOnline(ctx context.Context, target *outboundTransport, dd wire.DirectDialer, v netVerdict, restore *EngineError) holdOutcome {
	e.mu.Lock()
	name := target.name
	timerFn := e.holdTimerFn
	e.mu.Unlock()
	if timerFn == nil {
		timerFn = realHoldTimer
	}

	slog.Warn("engine: holding for the network", "outbound", name, "verdict", v.String())
	e.setHold(ctx, v.holdError())
	step := 0
	for {
		d := holdBackoff[min(step, len(holdBackoff)-1)]
		step++
		ch, stop := timerFn(d)
		trigger := "wait-tick"
		select {
		case <-ctx.Done():
			stop()
			return holdExit
		case <-ch:
		case <-e.holdWake:
			stop()
			trigger = "net-event"
			step = 0
		}
		v = e.judgeNet(ctx, trigger)
		if ctx.Err() != nil {
			return holdExit
		}
		if !v.blocksFailover() {
			slog.Info("engine: network back, leaving hold", "outbound", name, "verdict", v.String())
			e.setHold(ctx, restore)
			return holdOnline
		}
		raceErr, quit := e.phaseA(ctx, target, dd, "wait-tick")
		if quit || ctx.Err() != nil {
			return holdExit
		}
		if raceErr == nil {
			slog.Info("engine: current outbound answered during hold, leaving hold",
				"outbound", name, "verdict", v.String())
			e.setHold(ctx, restore)
			return holdRecovered
		}
		if e.failIfUserAction(target, raceErr) {
			return holdExit
		}
		e.setHold(ctx, v.holdError())
	}
}

// exhaustedHoldError is the shown hold while a target is on the schedule
// because every replacement candidate failed on a working network (R1).
func exhaustedHoldError(name string) *EngineError {
	return newEngineError(ErrCodeServerUnreachable,
		"outbound "+name+" unreachable; all replacement candidates failed; retrying")
}

// retryState schedules the next retry of a scheduled outbound (see retrySet).
type retryState struct {
	nextAt time.Time
	step   int // index into holdBackoff of the delay that produced nextAt

	// Exhaustion (R1). exhausted: on the schedule because every candidate
	// failed on a working network (else: no candidates — manual mode, no
	// cap). enteredAt: when this exhaustion run began (DIAG elapsedS).
	// since: start of the current stretch of online exhaustion — the cap
	// clock; zero while not counting (a tick offline, or the current node
	// answering code-less: alive, Center down). lastErr: the latest
	// exhaustion, for failExhausted's verdict precedence.
	exhausted bool
	enteredAt time.Time
	since     time.Time
	lastErr   error
}

// retrySet is the replace loop's retry schedule: outbounds whose node is
// down with nothing to replace it (manual mode / no fallbacks — retried
// forever) or whose every candidate failed on a working network (exhausted —
// retried until exhaustFailAfter of online exhaustion, R1). Instead of blocking the loop on one of them (which would
// starve every other outbound's Phase A / Phase B), each gets a timed Phase-A
// on the holdBackoff cadence, driven from the loop's own select. Owned by the
// loop goroutine — no lock; it dies with the loop (Stop/fail).
type retrySet struct {
	pending map[*outboundTransport]*retryState
	reason  *EngineError // hold shown while pending is non-empty

	timerFn func(time.Duration) (<-chan time.Time, func())
	now     func() time.Time

	// The one armed timer, for the earliest nextAt (nil ch = none; a nil
	// channel never fires in select).
	ch    <-chan time.Time
	stop  func()
	armAt time.Time
}

func newRetrySet(timerFn func(time.Duration) (<-chan time.Time, func()), now func() time.Time) *retrySet {
	if timerFn == nil {
		timerFn = realHoldTimer
	}
	if now == nil {
		now = time.Now
	}
	return &retrySet{pending: map[*outboundTransport]*retryState{}, timerFn: timerFn, now: now}
}

// hold is the hold the loop should show for the retry set: nil when nothing
// is pending.
func (r *retrySet) hold() *EngineError {
	if len(r.pending) == 0 {
		return nil
	}
	return r.reason
}

// schedule (re)arms target's retry at step (clamped to the last backoff),
// keeping its exhaustion state.
func (r *retrySet) schedule(target *outboundTransport, step int) {
	step = min(step, len(holdBackoff)-1)
	st, ok := r.pending[target]
	if !ok {
		st = &retryState{}
		r.pending[target] = st
	}
	st.nextAt, st.step = r.now().Add(holdBackoff[step]), step
}

// enterExhausted marks a scheduled target exhausted with err (only lastErr
// and the shown reason change when it already is): the run starts now, and
// so does its cap clock when counts (see exhaustCounts). Logs DIAG:
// outbound-exhausted once per run.
func (r *retrySet) enterExhausted(target *outboundTransport, name string, err error, counts bool) {
	st := r.pending[target]
	if st == nil {
		return
	}
	st.lastErr = err
	r.reason = exhaustedHoldError(name)
	if st.exhausted {
		return
	}
	now := r.now()
	st.exhausted, st.enteredAt, st.since = true, now, time.Time{}
	if counts {
		st.since = now
	}
	slog.Warn("DIAG: outbound-exhausted",
		"outbound", name, "tried", exhaustedTried(err), "err", err)
}

// leave removes a recovered target (via: phaseA / failover). An exhausted
// target logs DIAG: outbound-exhausted-recovered.
func (r *retrySet) leave(target *outboundTransport, name, via string) {
	if st := r.pending[target]; st != nil && st.exhausted {
		slog.Info("DIAG: outbound-exhausted-recovered",
			"outbound", name, "elapsedS", int64(r.now().Sub(st.enteredAt)/time.Second), "via", via)
	}
	delete(r.pending, target)
}

// arm keeps exactly one timer armed for the earliest pending retry. Re-arms
// only when the earliest time changed, so an idle loop arms nothing new.
func (r *retrySet) arm() {
	if len(r.pending) == 0 {
		r.disarm()
		return
	}
	var earliest time.Time
	for _, st := range r.pending {
		if earliest.IsZero() || st.nextAt.Before(earliest) {
			earliest = st.nextAt
		}
	}
	if r.ch != nil && earliest.Equal(r.armAt) {
		return
	}
	r.disarm()
	d := earliest.Sub(r.now())
	if d < 0 {
		d = 0
	}
	r.ch, r.stop = r.timerFn(d)
	r.armAt = earliest
}

func (r *retrySet) disarm() {
	if r.stop != nil {
		r.stop()
	}
	r.ch, r.stop, r.armAt = nil, nil, time.Time{}
}

// fired is called when r.ch delivered: the timer is spent.
func (r *retrySet) fired() {
	r.ch, r.stop, r.armAt = nil, nil, time.Time{}
}

// due returns the pending targets whose retry time has come.
func (r *retrySet) due() []*outboundTransport {
	now := r.now()
	var out []*outboundTransport
	for t, st := range r.pending {
		if !st.nextAt.After(now) {
			out = append(out, t)
		}
	}
	return out
}

// all returns every pending target.
func (r *retrySet) all() []*outboundTransport {
	out := make([]*outboundTransport, 0, len(r.pending))
	for t := range r.pending {
		out = append(out, t)
	}
	return out
}

// retryOnce is one scheduled attempt for a scheduled target (no candidates,
// or exhausted): re-judge the network (so the hold shows the network's
// reason when it is the network that is down), then one Phase-A and — when
// that fails on a working network with an error that is not a code-less
// reject (the node itself is dead or refusing) — one Phase B with a Reset
// provider (retryPhaseB). Success removes the target (streak reset by
// phaseA / the swap; a queued echo signal for it is dropped; the provider is
// Reset); failure advances its backoff (5s→10s→30s→30s…). A 401/402/403
// fails the engine. An exhausted target whose online exhaustion has lasted
// exhaustFailAfter fails it too (failAtExhaustCap). Echo signals never
// trigger it (outboundReplaceLoop drops them for pending targets). Returns
// false when the loop must exit. The caller syncs the shown hold afterwards.
func (e *Engine) retryOnce(ctx context.Context, r *retrySet, target *outboundTransport, trigger, reason string) bool {
	e.mu.Lock()
	connected := e.state == StateConnected
	dd := e.directDialer
	name := target.name
	provider := e.outboundProvider
	e.mu.Unlock()
	if !connected {
		// engine.state leaves connected only via Stop/fail, which end the
		// loop. Returning true here would leave target due and re-arm a 0s
		// timer: a busy spin until Stop cancels ctx.
		return false
	}
	st := r.pending[target]
	if st != nil && st.exhausted {
		reason = "exhausted-retry" // counted apart from no-candidates wait-ticks
	}
	v := e.judgeNet(ctx, trigger)
	if ctx.Err() != nil {
		return false
	}
	raceErr, quit := e.phaseA(ctx, target, dd, reason)
	if quit || ctx.Err() != nil {
		return false
	}
	if raceErr == nil {
		slog.Info("engine: scheduled outbound answered again", "outbound", name)
		r.leave(target, name, "phaseA")
		e.dropQueuedSignal(target)
		// Exclusions gathered while it was scheduled (a Center outage's)
		// must not outlive the schedule (review 3 I-2).
		if rs, ok := provider.(interface{ Reset() }); ok {
			rs.Reset()
		}
		return true
	}
	if e.failIfUserAction(target, raceErr) {
		return false
	}
	phaseB := escHeld // no Phase B ran: nothing new learned about the candidates
	var exhaustErr error
	verdict := v // the tick's latest verdict: Phase B's pre-exhaust one when that blocked
	// Phase B only when the current node itself is dead or refusing. A
	// code-less reject means the node is alive and Center is not: a
	// candidate would answer the same, so a tick does Phase A only for the
	// length of the outage (review 3 M-3). Once the node dies its error
	// stops being code-less and Phase B runs again (review B).
	if !v.blocksFailover() && provider != nil && !isCodeLessReject(raceErr) {
		// The current node still fails on a working network: try the
		// candidates again (review B ruling) — a target that entered the
		// schedule for a Center outage must not sit on a node that died
		// since, and a manual-mode target gets ErrNoCandidates at once. The
		// provider is Reset first: earlier exclusions may have been the
		// outage's, not the nodes'. Offline never gets here (hold, no
		// candidates burnt).
		if rs, ok := provider.(interface{ Reset() }); ok {
			rs.Reset()
		}
		phaseB, exhaustErr, verdict = e.retryPhaseB(ctx, target, raceErr, dd, provider)
		switch phaseB {
		case escExit:
			return false
		case escDone:
			r.leave(target, name, "failover")
			e.dropQueuedSignal(target)
			// Leaving the schedule through a swap: the round's code-less
			// candidates stay excluded otherwise (review 4, I-2 residual).
			if rs, ok := provider.(interface{ Reset() }); ok {
				rs.Reset()
			}
			return true
		}
	}
	next := 0
	if st != nil {
		next = st.step + 1
	}
	if trigger == "net-event" {
		next = 0 // a fresh network: start the cadence over
	}
	r.schedule(target, next)
	st = r.pending[target]
	counts := exhaustCounts(verdict, raceErr, exhaustErr)
	switch phaseB {
	case escExhausted:
		r.enterExhausted(target, name, exhaustErr, counts)
	case escNoCandidates:
		st.exhausted, st.since = false, time.Time{} // nothing to switch to: no cap
	}
	r.reason = verdict.holdError()
	if r.reason == nil {
		if st.exhausted {
			r.reason = exhaustedHoldError(name)
		} else {
			r.reason = noCandidatesError(name)
		}
	}
	if !st.exhausted {
		return true
	}
	if !counts {
		st.since = time.Time{}
		return true
	}
	if st.since.IsZero() {
		st.since = r.now()
	}
	return !e.failAtExhaustCap(r, target, name)
}

// exhaustCounts reports whether a tick (or escalate's exhaustion) counts
// toward the cap: the network works by the round's LATEST verdict (a
// pre-exhaust verdict that blocked overrides the tick's first one — R1
// review I-1), the current node is not answering code-less, and the round's
// candidates were not all code-less. Code-less = node alive, Center down: a
// Center outage of any length never disconnects, even with the current node
// dead (R1 review M-2 ruling). A dead fleet (transport failures) counts.
func exhaustCounts(verdict netVerdict, raceErr, exhaustErr error) bool {
	if verdict.blocksFailover() || isCodeLessReject(raceErr) {
		return false
	}
	var x *exhaustedError
	return !(errors.As(exhaustErr, &x) && x.allCodeLess)
}

// failAtExhaustCap fails the engine (failExhausted) once target has been
// exhausted on an online network for exhaustFailAfter, and reports whether
// it did. The only caller of failExhausted (TestReplaceLoopFailSitesAllowList).
func (e *Engine) failAtExhaustCap(r *retrySet, target *outboundTransport, name string) bool {
	st := r.pending[target]
	elapsed := r.now().Sub(st.since)
	if elapsed < exhaustFailAfter {
		return false
	}
	slog.Error("DIAG: outbound-exhausted-fatal",
		"outbound", name, "elapsedS", int64(elapsed/time.Second))
	e.failExhausted(target, st.lastErr)
	return true
}

// retryPhaseB is retryOnce's Phase B: escDone after a swap; escNoCandidates
// when there is nothing to switch to (manual mode — no cap); escExhausted
// with the exhaustion error when every candidate failed (or none could be
// tried) on a working network; escHeld when the network died meanwhile
// (provider Reset, nothing learned); escExit when ctx ended or an account
// verdict failed the engine. The verdict returned is the pre-exhaust one
// when it was taken (escHeld / escExhausted), else online: the caller shows
// and counts by it (R1 review I-1).
func (e *Engine) retryPhaseB(ctx context.Context, target *outboundTransport, raceErr error, dd wire.DirectDialer, provider OutboundProvider) (escResult, error, netVerdict) {
	err := e.replaceOutboundServer(ctx, target, raceErr, dd, provider)
	switch {
	case err == nil:
		return escDone, nil, netOnline
	case ctx.Err() != nil:
		return escExit, nil, netOnline
	case e.noCandidates(target, err):
		return escNoCandidates, nil, netOnline
	}
	v := e.judgeNet(ctx, "pre-exhaust")
	if ctx.Err() != nil {
		return escExit, nil, v
	}
	if v.blocksFailover() {
		if rs, ok := provider.(interface{ Reset() }); ok {
			rs.Reset()
		}
		return escHeld, nil, v
	}
	if e.failIfExhaustedByAccount(target, err) {
		return escExit, nil, v
	}
	return escExhausted, err, v
}

// drainHoldWake drops a pending holdWake token. A network event that
// happened BEFORE a wait's first verdict is already reflected in it; left in
// the cap-1 channel it would wake the wait at once and skip the 5s step.
func (e *Engine) drainHoldWake() {
	select {
	case <-e.holdWake:
	default:
	}
}

// dropQueuedSignal discards an echo re-race signal for target queued while
// the loop was waiting: the node was just re-raced successfully, and a stale
// signal would re-race (and ReplacePrimary over) the fresh dialer. A queued
// signal for another outbound is put back (best effort: if a newer signal
// filled the cap-1 channel meanwhile, that one stands).
func (e *Engine) dropQueuedSignal(target *outboundTransport) {
	select {
	case ot := <-e.reRaceChan:
		if ot != target {
			select {
			case e.reRaceChan <- ot:
			default:
			}
		}
	default:
	}
}

// wakeHold pokes a running hold to re-judge now. Non-blocking; a pending
// wake is enough (cap 1).
func (e *Engine) wakeHold() {
	select {
	case e.holdWake <- struct{}{}:
	default:
	}
}

// directResolve adapts a DNS query func (DirectDNSClient.Query) into the
// netcheck resolver. A and AAAA are queried in parallel; IPv4 addresses are
// returned first because netcheck dials addresses in order inside a single
// 3s budget — on a network that blackholes IPv6, AAAA first would spend the
// whole budget there and report a false no-internet.
func directResolve(query func(context.Context, *mdns.Msg) (*mdns.Msg, error)) func(context.Context, string) ([]netip.Addr, error) {
	return func(ctx context.Context, host string) ([]netip.Addr, error) {
		var v4, v6 []netip.Addr
		var err4, err6 error
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); v4, err4 = lookupType(ctx, query, host, mdns.TypeA) }()
		go func() { defer wg.Done(); v6, err6 = lookupType(ctx, query, host, mdns.TypeAAAA) }()
		wg.Wait()
		addrs := append(v4, v6...)
		if len(addrs) > 0 {
			return addrs, nil
		}
		if err4 != nil {
			return nil, err4
		}
		if err6 != nil {
			return nil, err6
		}
		return nil, errors.New("netcheck: no addresses for " + host)
	}
}

func lookupType(ctx context.Context, query func(context.Context, *mdns.Msg) (*mdns.Msg, error), host string, qtype uint16) ([]netip.Addr, error) {
	m := new(mdns.Msg)
	m.SetQuestion(mdns.Fqdn(host), qtype)
	m.RecursionDesired = true
	resp, err := query(ctx, m)
	if err != nil {
		return nil, err
	}
	var out []netip.Addr
	for _, rr := range resp.Answer {
		var ip []byte
		switch r := rr.(type) {
		case *mdns.A:
			ip = r.A
		case *mdns.AAAA:
			ip = r.AAAA
		default:
			continue
		}
		if a, ok := netip.AddrFromSlice(ip); ok {
			out = append(out, a.Unmap())
		}
	}
	return out, nil
}

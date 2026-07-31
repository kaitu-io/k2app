package wire

import (
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kaitu-io/k2/safego"
	"github.com/sasha-s/go-deadlock"
)

// Long-connection periodic revalidation (spec §6.5). Without it the
// "lose access within ~2×cookieTTL" bound (wire/cookie.go's header — the
// cache_ttl/cookieTTL stacking) is void: a 6-hour QUIC connection would
// outlive an unsubscribe by 6 hours. The registry tracks live authenticated
// connections on a listener; a sweeper re-checks each one against its own
// clocks and, only when the endorsement window has lapsed, against the
// validator chain (whose CachingValidator dedups with the cold-auth path, so
// Center sees at most one request per device per node per cookieTTL — but,
// same as the cold-auth path, may also return a pre-revocation cached OK
// instead of a fresh check).

const (
	// cookieGraceMax caps the self-governed "Center unreachable" state: a
	// session accumulating undecidable verdicts for longer is dropped, so a
	// permanently unreachable Center cannot grant indefinite autonomy.
	cookieGraceMax = 6 * time.Hour

	// authSweepInterval is the sweeper cadence.
	authSweepInterval = 60 * time.Second
)

// authSession is one live transport connection's auth state.
//
// Publication protocol: the connection registers at accept time, BEFORE any
// credentials exist. markAuthed later writes the plain identity fields and
// then atomically stores authAt — readers (the sweeper) load authAt first and
// touch the plain fields only when it is non-zero. sync/atomic's ordering
// makes this a valid release/acquire publish; go test -race stays clean.
type authSession struct {
	auth *connAuth
	kill func() // severs the connection; called with NO lock held (Rule #1/#4)

	once  sync.Once // one-time identity publish
	udid  string
	token string // cold-auth credential, reused for revalidation
	mode  string

	authAt     atomic.Int64 // unix sec of last Center endorsement; 0 = never authed
	svcExp     atomic.Int64 // Center ServiceExpiredAt; 0 = no subscription clock
	graceStart atomic.Int64 // unix sec of first consecutive undecidable; 0 = none
}

// markAuthed publishes the session identity exactly once (first metadata
// verdict wins; a connection re-sending metadata does not re-publish — the
// sweeper re-validates on its own schedule anyway).
func (s *authSession) markAuthed(udid, token, mode string, authAt, svcExp int64) {
	s.once.Do(func() {
		s.udid = udid
		s.token = token
		s.mode = mode
		s.svcExp.Store(svcExp)
		s.authAt.Store(authAt) // publish LAST
	})
}

// authSessions is a per-listener registry + sweeper.
//
// Concurrency (constitutional): mu is a standalone STATE lock protecting only
// the set map — registered in k2/CLAUDE.md's Lock Ordering Graph as
// `wire.authSessions.mu`, no nesting with any engine/server lock. The sweeper
// snapshots under mu, releases, then performs validator calls and kill()
// with no lock held.
type authSessions struct {
	validate Validator
	interval time.Duration
	now      func() time.Time // injectable clock; tests drive sweepOnce directly

	done     chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup

	mu  deadlock.Mutex
	set map[*authSession]struct{}
}

// newAuthSessions builds a registry and starts its sweeper goroutine
// (safego.Go; exits on Close).
//
// v MUST have its own bounded call latency (the NewRemoteValidator convention:
// remoteAuthClient's http.Client sets Timeout: 5*time.Second — see
// wire/auth_remote.go). The sweeper enforces no local timeout of its own: a
// validator call that never returns wedges the worker running it forever,
// which starves sweepOnce's wg.Wait (fewer live workers each pass), which in
// the worst case wedges Close()'s wg.Wait — and therefore
// QUICServer.Close/TCPWSServer.Close/whole-process SIGTERM. This is not a new
// obligation introduced here: CachingValidator (wire/auth_cache.go) calls the
// same inner Validator inside singleflight.Group.Do with no timeout of its
// own either, so the bounded-latency contract already has to hold for every
// Validator in the chain today. Do not swap in a custom Validator without
// verifying it upholds this.
func newAuthSessions(v Validator, sweep time.Duration) *authSessions {
	a := &authSessions{
		validate: v,
		interval: sweep,
		now:      time.Now,
		done:     make(chan struct{}),
		set:      make(map[*authSession]struct{}),
	}
	a.wg.Add(1)
	safego.Go(func() {
		defer a.wg.Done()
		ticker := time.NewTicker(a.interval)
		defer ticker.Stop()
		for {
			select {
			case <-a.done:
				return
			case <-ticker.C:
				a.sweepOnce()
			}
		}
	})
	return a
}

// register adds s and returns an idempotent unregister func for the
// connection's teardown defer.
func (a *authSessions) register(s *authSession) (unregister func()) {
	a.mu.Lock()
	a.set[s] = struct{}{}
	a.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			a.mu.Lock()
			delete(a.set, s)
			a.mu.Unlock()
		})
	}
}

// Close stops the sweeper and waits for it to exit. Idempotent. Never called
// with a lock held (wg.Wait — Rule #3).
func (a *authSessions) Close() error {
	a.stopOnce.Do(func() { close(a.done) })
	a.wg.Wait()
	return nil
}

// sweepMaxWorkers bounds the concurrency of ONE revalidation pass. A serial
// pass is a liveness hazard: remoteAuthClient's timeout is 5s and Code==-1 is
// never cached (singleflight only merges CONCURRENT calls; a serial loop gets
// no dedup), so during a Center outage a node with tens of thousands of live
// connections would take an hour-scale wall time to finish one pass. That would
// (a) make authSessions.Close()'s wg.Wait — hence QUICServer/TCPWSServer.Close
// and whole-process SIGTERM — hang for that same hour, and (b) delay SvcExp
// expiry disconnects for every session late in the snapshot. A small pool caps
// pass wall time at ~(N/8)×5s worst case, and the per-session done check gives
// Close a bounded exit (one in-flight validator call, not a whole pass).
const sweepMaxWorkers = 8

// sweepOnce runs one revalidation pass: snapshot (state lock) → release → act
// on the snapshot with a bounded worker pool, no lock held. The feed loop stops
// on Close (a.done), so Close never waits a full pass — only the in-flight
// reviewSession calls drain (bounded by one validator timeout).
func (a *authSessions) sweepOnce() {
	a.mu.Lock()
	snapshot := make([]*authSession, 0, len(a.set))
	for s := range a.set {
		snapshot = append(snapshot, s)
	}
	a.mu.Unlock()
	if len(snapshot) == 0 {
		return
	}

	work := make(chan *authSession)
	var wg sync.WaitGroup
	workers := sweepMaxWorkers
	if len(snapshot) < workers {
		workers = len(snapshot)
	}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		safego.Go(func() {
			defer wg.Done()
			for s := range work {
				select {
				case <-a.done: // Close requested — stop before processing
					return
				default:
				}
				a.reviewSession(s)
			}
		})
	}
	for _, s := range snapshot {
		select {
		case <-a.done: // Close requested — stop feeding, let workers drain
			close(work)
			wg.Wait()
			return
		case work <- s:
		}
	}
	close(work)
	wg.Wait()
}

// reviewSession applies the four dispositions to ONE session. now is read fresh
// per session (a pass may itself span many minutes under load, and grace math
// against a pass-start clock would drift). Validator call and kill() happen
// with NO lock held (constitutional Rule #1/#2).
//
// Single-reviewer assumption: reviewSession updates authAt/svcExp/graceStart
// via three separate atomic Store calls, not one atomic group update — safe
// only because at most one goroutine reviews a given *authSession at a time.
// That holds today because authSessions runs exactly one sweeper goroutine
// per instance (the ticker loop started in newAuthSessions), and sweepOnce's
// worker pool fans a single pass's snapshot out across workers without ever
// assigning the same session to two workers concurrently. Calling sweepOnce()
// concurrently from multiple goroutines — as TestDeadlock_AuthSessionsStandalone
// deliberately does, to stress lock-ordering rather than session-state
// correctness — CAN interleave the three Store calls on an overlapping session
// inconsistently. Do not add a second production caller of sweepOnce() without
// also adding per-session serialization.
func (a *authSessions) reviewSession(s *authSession) {
	authAt := s.authAt.Load()
	if authAt == 0 {
		return // never authenticated (permissive legacy) — nothing to recheck
	}
	now := a.now()
	// 1. Subscription clock expired → drop. Zero Center round trips.
	if svc := s.svcExp.Load(); svc != 0 && now.Unix() >= svc {
		slog.Info("DIAG: auth-session-expired", "udid", s.udid, "svcExp", svc)
		s.kill()
		return
	}
	// 2. Endorsement still inside the window → nothing to do.
	if now.Unix()-authAt <= int64(cookieTTL.Seconds()) {
		return
	}
	// 3. Stale → revalidate. Validator call OUTSIDE any lock.
	v := a.validate(s.token, s.udid, s.mode)
	// Explicit dispatch on Code — AuthVerdict is FOUR-state (wire/auth.go).
	// `!v.OK` alone must NEVER mean kill: that would turn one Center outage into
	// a fleet-wide disconnect 15 minutes later (spec §6.4: fail-closed only for
	// "Center said no", never "Center didn't answer").
	switch {
	case v.OK:
		s.authAt.Store(now.Unix())
		if v.SvcExp != 0 {
			s.svcExp.Store(v.SvcExp)
		}
		s.graceStart.Store(0)
	case v.Code == 401 || v.Code == 402 || v.Code == 403:
		slog.Warn("DIAG: auth-session-revoked", "udid", s.udid, "code", v.Code)
		s.kill()
	default:
		// Code == -1 (undecidable) and anything unclassified: do NOT disconnect.
		// Advance the REGISTRY's authAt by one window — the client's cookie still
		// expires on its original schedule, so the self-governed state cannot
		// leak beyond this connection's life.
		gs := s.graceStart.Load()
		if gs == 0 {
			gs = now.Unix()
			s.graceStart.Store(gs)
		}
		if now.Unix()-gs > int64(cookieGraceMax.Seconds()) {
			slog.Warn("DIAG: auth-grace-exhausted", "udid", s.udid, "graceS", now.Unix()-gs)
			s.kill()
			return
		}
		s.authAt.Store(authAt + int64(cookieTTL.Seconds()))
		slog.Warn("DIAG: auth-center-unreachable", "udid", s.udid, "code", v.Code, "graceS", now.Unix()-gs)
	}
}

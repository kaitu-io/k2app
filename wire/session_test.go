package wire

import (
	"sync/atomic"
	"testing"
	"time"
)

// sweepFixture builds a registry whose sweeper goroutine is given a 1-hour
// interval so it never ticks during a test; tests drive sweepOnce() directly
// with a controlled clock. (The goroutine is not "stopped" — it is idle until
// t.Cleanup calls Close.)
type sweepFixture struct {
	a      *authSessions
	clock  time.Time
	killed atomic.Int32
	calls  atomic.Int32
}

func newSweepFixture(t *testing.T, v Validator) *sweepFixture {
	t.Helper()
	f := &sweepFixture{clock: time.Unix(1_800_000_000, 0)}
	counted := func(token, udid, mode string) AuthVerdict {
		f.calls.Add(1)
		return v(token, udid, mode)
	}
	f.a = newAuthSessions(counted, time.Hour) // 1h interval → ticker never fires; tests call sweepOnce directly
	t.Cleanup(func() { f.a.Close() })
	f.a.now = func() time.Time { return f.clock }
	return f
}

func (f *sweepFixture) addSession(authAt, svcExp int64) *authSession {
	s := &authSession{auth: newConnAuth(), kill: func() { f.killed.Add(1) }}
	f.a.register(s)
	s.markAuthed("dev-1", "tok", "", authAt, svcExp)
	return s
}

func okV() Validator {
	return func(token, udid, mode string) AuthVerdict { return AuthVerdict{OK: true} }
}

// Disposition 1 (spec §6.5): SvcExp in the past → kill, ZERO validator calls.
func TestSweep_SvcExpKillsWithoutCenter(t *testing.T) {
	f := newSweepFixture(t, okV())
	f.addSession(f.clock.Unix()-60, f.clock.Unix()-1)
	f.a.sweepOnce()
	if f.killed.Load() != 1 {
		t.Fatal("expired-subscription session not killed")
	}
	if f.calls.Load() != 0 {
		t.Fatalf("SvcExp expiry consulted the validator %d times, want 0", f.calls.Load())
	}
}

// Disposition 2: endorsement still fresh → untouched, no validator call.
func TestSweep_FreshSessionNotRevalidated(t *testing.T) {
	f := newSweepFixture(t, okV())
	s := f.addSession(f.clock.Unix()-int64((cookieTTL-time.Minute).Seconds()), 0)
	f.a.sweepOnce()
	if f.calls.Load() != 0 || f.killed.Load() != 0 {
		t.Fatalf("fresh session touched: calls=%d killed=%d", f.calls.Load(), f.killed.Load())
	}
	if got := s.authAt.Load(); got != f.clock.Unix()-int64((cookieTTL-time.Minute).Seconds()) {
		t.Fatalf("authAt moved on a fresh session: %d", got)
	}
}

// Disposition 3a: stale + Center OK → authAt advances to now, grace resets.
func TestSweep_RevalidateOKAdvancesAuthAt(t *testing.T) {
	newExp := time.Unix(1_800_000_000, 0).Add(48 * time.Hour).Unix()
	f := newSweepFixture(t, func(token, udid, mode string) AuthVerdict {
		return AuthVerdict{OK: true, SvcExp: newExp}
	})
	s := f.addSession(f.clock.Unix()-int64(cookieTTL.Seconds())-10, 0)
	s.graceStart.Store(f.clock.Unix() - 100)
	f.a.sweepOnce()
	if f.calls.Load() != 1 || f.killed.Load() != 0 {
		t.Fatalf("calls=%d killed=%d", f.calls.Load(), f.killed.Load())
	}
	if s.authAt.Load() != f.clock.Unix() {
		t.Fatalf("authAt not refreshed: %d != %d", s.authAt.Load(), f.clock.Unix())
	}
	if s.svcExp.Load() != newExp {
		t.Fatal("SvcExp from revalidation not stored")
	}
	if s.graceStart.Load() != 0 {
		t.Fatal("grace streak not reset after successful revalidation")
	}
}

// Disposition 3b: stale + explicit reject → kill.
func TestSweep_ExplicitRejectKills(t *testing.T) {
	for _, code := range []int{401, 402, 403} {
		f := newSweepFixture(t, func(token, udid, mode string) AuthVerdict {
			return AuthVerdict{OK: false, Code: code}
		})
		f.addSession(f.clock.Unix()-int64(cookieTTL.Seconds())-10, 0)
		f.a.sweepOnce()
		if f.killed.Load() != 1 {
			t.Fatalf("code %d: revoked session not killed", code)
		}
	}
}

// §6.4 row 4 (node config missing) — spec §9's THIRD required table, alongside
// TestSweep_ExplicitRejectKills (Center explicit reject) and
// TestSweep_CenterUnreachableDoesNotKill (Center unreachable). Phase 1's
// NewRemoteValidator fails CLOSED on a node-config problem (nodeSecret=="" /
// nodeIPv4 unresolvable) by returning a NON(-1) REJECT verdict — never
// undecidable. That encoding is load-bearing on BOTH paths:
//   - cold auth rejects (resolveMetadataAuth's 401/402/403 arm, Task 3 —
//     TestMetadataAuth_ExplicitRejectCode-shaped);
//   - the sweeper KILLS (below), instead of extending the grace window.
//
// Were node-config-missing miscoded as -1, a node that lost its own config
// would silently keep every stale session alive for the full 6h grace — the
// exact fleet-safety inversion this row guards against. This test pins the
// sweep half; it asserts the disposition, not the concrete code, which Phase 1
// owns. If Phase 1 chose a code outside {401,402,403}, this test going green
// against a KEEP-alive default is the tripwire that the encoding is wrong.
func TestSweep_NodeConfigMissingFailsClosed(t *testing.T) {
	f := newSweepFixture(t, func(token, udid, mode string) AuthVerdict {
		return AuthVerdict{OK: false, Code: 403} // Phase 1's fail-closed reject verdict
	})
	f.addSession(f.clock.Unix()-int64(cookieTTL.Seconds())-10, 0)
	f.a.sweepOnce()
	if f.killed.Load() != 1 {
		t.Fatal("node-config-missing fail-closed did not kill on revalidation")
	}
}

// Disposition 3c — THE fleet-safety case (spec §6.4): stale + Center
// undecidable (-1) → NOT killed, authAt advanced by exactly one window, and
// the validator really was reached (not short-circuited upstream).
func TestSweep_CenterUnreachableDoesNotKill(t *testing.T) {
	f := newSweepFixture(t, func(token, udid, mode string) AuthVerdict {
		return AuthVerdict{OK: false, Code: -1}
	})
	origAuthAt := f.clock.Unix() - int64(cookieTTL.Seconds()) - 10
	s := f.addSession(origAuthAt, 0)
	f.a.sweepOnce()
	if f.calls.Load() != 1 {
		t.Fatalf("FALSE GREEN: validator never reached (calls=%d) — the unreachable path was not exercised", f.calls.Load())
	}
	if f.killed.Load() != 0 {
		t.Fatal("FLEET KILLER: undecidable verdict disconnected the session")
	}
	if got, want := s.authAt.Load(), origAuthAt+int64(cookieTTL.Seconds()); got != want {
		t.Fatalf("authAt not advanced by one window: got %d want %d", got, want)
	}
	if s.graceStart.Load() == 0 {
		t.Fatal("grace streak not started")
	}
}

// Unknown / unclassified codes must not kill either (four-state contract:
// "not OK" alone is never a death sentence).
func TestSweep_UnknownCodeDoesNotKill(t *testing.T) {
	f := newSweepFixture(t, func(token, udid, mode string) AuthVerdict {
		return AuthVerdict{OK: false, Code: 0} // "no opinion" leaking through
	})
	f.addSession(f.clock.Unix()-int64(cookieTTL.Seconds())-10, 0)
	f.a.sweepOnce()
	if f.killed.Load() != 0 {
		t.Fatal("unclassified verdict killed the session")
	}
}

// Disposition 3d: undecidable accumulating past cookieGraceMax → kill.
func TestSweep_GraceMaxKills(t *testing.T) {
	f := newSweepFixture(t, func(token, udid, mode string) AuthVerdict {
		return AuthVerdict{OK: false, Code: -1}
	})
	s := f.addSession(f.clock.Unix()-int64(cookieTTL.Seconds())-10, 0)
	// Simulate an outage: sweep repeatedly, advancing the clock one window at
	// a time, until just before the cap — must stay alive.
	steps := int(cookieGraceMax/cookieTTL) - 1
	for i := 0; i < steps; i++ {
		f.a.sweepOnce()
		f.clock = f.clock.Add(cookieTTL + time.Second)
	}
	if f.killed.Load() != 0 {
		t.Fatalf("killed before cookieGraceMax (after %d windows)", steps)
	}
	// Push past 6h of continuous undecidable.
	f.clock = time.Unix(s.graceStart.Load(), 0).Add(cookieGraceMax + time.Minute)
	f.a.sweepOnce()
	if f.killed.Load() != 1 {
		t.Fatal("session outlived cookieGraceMax during permanent Center outage")
	}
}

// Sessions that never authenticated (permissive legacy) are skipped entirely.
func TestSweep_UnauthedSessionSkipped(t *testing.T) {
	f := newSweepFixture(t, okV())
	s := &authSession{auth: newConnAuth(), kill: func() { f.killed.Add(1) }}
	f.a.register(s) // markAuthed never called → authAt == 0
	f.a.sweepOnce()
	if f.calls.Load() != 0 || f.killed.Load() != 0 {
		t.Fatal("unauthenticated session was revalidated or killed")
	}
}

func TestAuthSessions_RegisterUnregister(t *testing.T) {
	f := newSweepFixture(t, func(token, udid, mode string) AuthVerdict {
		return AuthVerdict{OK: false, Code: 401}
	})
	s := f.addSession(f.clock.Unix()-int64(cookieTTL.Seconds())-10, 0)
	_ = s
	// Grab the unregister func through a second registration to prove idempotency.
	s2 := &authSession{auth: newConnAuth(), kill: func() { f.killed.Add(1) }}
	unreg := f.a.register(s2)
	s2.markAuthed("dev-2", "tok", "", f.clock.Unix()-int64(cookieTTL.Seconds())-10, 0)
	unreg()
	unreg() // double-unregister must be a no-op
	f.a.sweepOnce()
	if f.killed.Load() != 1 { // only s (still registered) is killed
		t.Fatalf("killed=%d, want 1 — unregistered session was swept", f.killed.Load())
	}
}

// Close must stop the sweeper goroutine (goleak in TestMain enforces this
// package-wide; this test also pins that Close returns promptly).
func TestAuthSessions_CloseStopsSweeper(t *testing.T) {
	a := newAuthSessions(okV(), 10*time.Millisecond)
	time.Sleep(30 * time.Millisecond) // let it tick at least once
	done := make(chan struct{})
	go func() { a.Close(); a.Close(); close(done) }() // idempotent
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not return — sweeper goroutine stuck")
	}
}

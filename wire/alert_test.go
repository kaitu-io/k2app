package wire

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// alertCapture is a fake Slack endpoint that records the posted bodies.
type alertCapture struct {
	mu   sync.Mutex
	msgs []string
}

func (c *alertCapture) add(s string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.msgs = append(c.msgs, s)
}

func (c *alertCapture) all() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.msgs...)
}

// waitFor polls until cond holds or the deadline passes. Fire() posts on its
// own goroutine (it must never block the auth path), so assertions on delivery
// are inherently asynchronous.
func waitFor(t *testing.T, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return cond()
}

func newCaptureAlerter(t *testing.T) (*SlackAlerter, *alertCapture) {
	t.Helper()
	cap := &alertCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var p struct {
			Text string `json:"text"`
		}
		json.Unmarshal(body, &p)
		cap.add(p.Text)
	}))
	t.Cleanup(srv.Close)
	return NewSlackAlerter(srv.URL, "test-node"), cap
}

// TestSlackAlerter_DisabledWithoutWebhook is the whole opt-in contract: no
// webhook configured => nil alerter, and every path stays a no-op. A node that
// has not opted in must behave exactly as it did before alerting existed.
func TestSlackAlerter_DisabledWithoutWebhook(t *testing.T) {
	a := NewSlackAlerter("", "node-1")
	if a != nil {
		t.Fatalf("empty webhook must yield a nil alerter, got %+v", a)
	}
	if a.Fire("k", "text") {
		t.Error("nil alerter must not report a send")
	}

	// And the decorator must hand back the validator untouched, so an
	// unconfigured node pays nothing — not even a mutex.
	inner := constVerdict(AuthVerdict{Code: AuthCodeUndetermined})
	if got := WithAuthHealthAlerts(inner, nil); got == nil {
		t.Fatal("WithAuthHealthAlerts(inner, nil) must return a usable validator")
	}
}

// TestSlackAlerter_Cooldown pins the throttle. A Center outage touches every
// auth on the node; without a cooldown one incident would post thousands of
// identical messages.
func TestSlackAlerter_Cooldown(t *testing.T) {
	a, cap := newCaptureAlerter(t)
	now := time.Now()
	a.now = func() time.Time { return now }

	if !a.Fire("key1", "first") {
		t.Fatal("first Fire must send")
	}
	if a.Fire("key1", "second") {
		t.Error("a repeat inside the cooldown must be suppressed")
	}
	// A DIFFERENT key is independent — this is what keeps a recovery notice
	// from being swallowed by the outage alert's cooldown.
	if !a.Fire("key2", "other") {
		t.Error("a different key must not share key1's cooldown")
	}

	now = now.Add(alertCooldown + time.Second)
	if !a.Fire("key1", "third") {
		t.Error("Fire must send again once the cooldown lapses")
	}

	if !waitFor(t, func() bool { return len(cap.all()) == 3 }) {
		t.Fatalf("want 3 delivered alerts, got %v", cap.all())
	}
	for _, m := range cap.all() {
		if len(m) == 0 || m[0] != '[' {
			t.Errorf("alert %q must be prefixed with the node label", m)
		}
	}
}

// TestAuthHealth_UnreachableAlertsOnlyAfterThreshold pins §6.4's timing: a
// brief Center blip must stay silent, sustained unreachability must page.
func TestAuthHealth_UnreachableAlertsOnlyAfterThreshold(t *testing.T) {
	a, cap := newCaptureAlerter(t)
	now := time.Now()
	a.now = func() time.Time { return now }
	h := &authHealth{alerter: a, now: func() time.Time { return now }}

	h.observe(AuthVerdict{Code: AuthCodeUndetermined})
	now = now.Add(authUnreachableAlertAfter - time.Second)
	h.observe(AuthVerdict{Code: AuthCodeUndetermined})
	if got := cap.all(); len(got) != 0 {
		t.Fatalf("must stay silent below the threshold, got %v", got)
	}

	now = now.Add(2 * time.Second) // now past the threshold
	h.observe(AuthVerdict{Code: AuthCodeUndetermined})
	if !waitFor(t, func() bool { return len(cap.all()) == 1 }) {
		t.Fatalf("want 1 alert past the threshold, got %v", cap.all())
	}

	// Still unreachable => no second page (the alerted latch, not just the
	// cooldown, must hold).
	now = now.Add(time.Minute)
	h.observe(AuthVerdict{Code: AuthCodeUndetermined})
	if got := cap.all(); len(got) != 1 {
		t.Errorf("sustained unreachability must not re-page, got %v", got)
	}
}

// TestAuthHealth_RecoveryResetsAndNotifies: an alert that never clears trains
// people to ignore it. Any answer from Center — including a rejection — proves
// the path works.
func TestAuthHealth_RecoveryResetsAndNotifies(t *testing.T) {
	for _, recovery := range []AuthVerdict{{OK: true}, {Code: 401}, {Code: 402}, {Code: 403}} {
		a, cap := newCaptureAlerter(t)
		now := time.Now()
		a.now = func() time.Time { return now }
		h := &authHealth{alerter: a, now: func() time.Time { return now }}

		h.observe(AuthVerdict{Code: AuthCodeUndetermined})
		now = now.Add(authUnreachableAlertAfter + time.Second)
		h.observe(AuthVerdict{Code: AuthCodeUndetermined})
		if !waitFor(t, func() bool { return len(cap.all()) == 1 }) {
			t.Fatalf("%+v: expected the outage alert first, got %v", recovery, cap.all())
		}

		h.observe(recovery)
		if !waitFor(t, func() bool { return len(cap.all()) == 2 }) {
			t.Fatalf("%+v: expected a recovery notice, got %v", recovery, cap.all())
		}

		// State must be fully reset: a fresh outage has to re-serve the full
		// threshold rather than paging instantly.
		h.observe(AuthVerdict{Code: AuthCodeUndetermined})
		if got := cap.all(); len(got) != 2 {
			t.Errorf("%+v: post-recovery outage must restart the threshold, got %v", recovery, got)
		}
	}
}

// TestAuthHealth_NodeFaultAlertsImmediately: a missing NodeSecret is not
// transient — it stays broken until a human intervenes, so waiting out a
// window that will never expire on its own only delays the fix.
func TestAuthHealth_NodeFaultAlertsImmediately(t *testing.T) {
	a, cap := newCaptureAlerter(t)
	h := &authHealth{alerter: a, now: time.Now}

	h.observe(AuthVerdict{Code: AuthCodeNodeFault})
	if !waitFor(t, func() bool { return len(cap.all()) == 1 }) {
		t.Fatalf("node fault must page on first occurrence, got %v", cap.all())
	}
}

// TestAuthHealth_NoOpinionIsNeutral: a users-file miss says nothing about
// Center. Treating it as a signal would page on every shared node, which run
// with an empty users file by design.
func TestAuthHealth_NoOpinionIsNeutral(t *testing.T) {
	a, cap := newCaptureAlerter(t)
	now := time.Now()
	h := &authHealth{alerter: a, now: func() time.Time { return now }}

	for i := 0; i < 50; i++ {
		now = now.Add(time.Minute)
		h.observe(AuthVerdict{})
	}
	if got := cap.all(); len(got) != 0 {
		t.Errorf("no-opinion verdicts must never alert, got %v", got)
	}
}

// TestWithAuthHealthAlerts_PassesVerdictThrough is the safety property that
// makes this decorator acceptable on the auth path at all: it observes, it
// never decides. If wrapping could change a verdict, an alerting bug would
// become an admission bug.
func TestWithAuthHealthAlerts_PassesVerdictThrough(t *testing.T) {
	a, _ := newCaptureAlerter(t)
	for _, want := range []AuthVerdict{
		{OK: true, UserID: 42, SvcExp: 99},
		{Code: 401},
		{Code: AuthCodeUndetermined},
		{Code: AuthCodeNodeFault},
		{},
	} {
		got := WithAuthHealthAlerts(constVerdict(want), a)("t", "u", "")
		if got != want {
			t.Errorf("verdict mutated by the alert decorator: got %+v, want %+v", got, want)
		}
	}
}

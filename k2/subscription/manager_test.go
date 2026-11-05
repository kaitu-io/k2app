package subscription

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

// fakeSub is a minimal stand-in for config.Subscription used by Manager
// tests. It supports a Tunnels()/Pick(excluded) surface compatible with
// what Manager calls on it. Constructor builds tunnels from URLs.
//
// Only used inside the subscription/ package — tests in the daemon or
// gateway package will exercise the real *config.Subscription.

func TestManager_NextURL_PicksDifferentURL(t *testing.T) {
	urlA := "k2v5://udid:tok@a.example/?ech=x"
	urlB := "k2v5://udid:tok@b.example/?ech=x"

	mgr := &Manager{}
	sess := newTestSession(t, []string{urlA, urlB})
	mgr.sessions = []*Session{sess}

	got, err := mgr.NextURL(urlA, errors.New("test"))
	if err != nil {
		t.Fatalf("NextURL err: %v", err)
	}
	if got == urlA {
		t.Fatalf("NextURL returned excluded URL: %s", got)
	}
	if got != urlB {
		t.Fatalf("NextURL = %q, want %q", got, urlB)
	}
}

func TestManager_NextURL_UnknownURL_ReturnsErrNoSubscription(t *testing.T) {
	mgr := &Manager{}
	mgr.sessions = []*Session{newTestSession(t, []string{"k2v5://udid:tok@a.example/?ech=x"})}

	_, err := mgr.NextURL("k2v5://udid:tok@unknown.example/?ech=x", nil)
	if !errors.Is(err, ErrNoSubscription) {
		t.Fatalf("err = %v, want ErrNoSubscription", err)
	}
}

func TestManager_NextURL_Exhausted_ReturnsError(t *testing.T) {
	urlA := "k2v5://udid:tok@a.example/?ech=x"
	mgr := &Manager{}
	mgr.sessions = []*Session{newTestSession(t, []string{urlA})}

	_, err := mgr.NextURL(urlA, errors.New("dead"))
	if err == nil {
		t.Fatalf("expected error when only candidate is excluded")
	}
	if !strings.Contains(err.Error(), "all subscribed tunnels failed") {
		t.Fatalf("unexpected err: %v", err)
	}
}

func TestManager_OwnsURL(t *testing.T) {
	urlA := "k2v5://udid:tok@a.example/?ech=x"
	mgr := &Manager{}
	if mgr.OwnsURL(urlA) {
		t.Fatal("OwnsURL with no sessions = true, want false")
	}
	mgr.sessions = []*Session{newTestSession(t, []string{urlA})}
	if !mgr.OwnsURL(urlA) {
		t.Fatal("OwnsURL(session URL) = false, want true")
	}
	if mgr.OwnsURL("k2v5://udid:tok@unknown.example/?ech=x") {
		t.Fatal("OwnsURL(unknown) = true, want false")
	}
}

// Reset (engine compositeProvider.Reset → shell provider): clears every
// session's per-session exclusions, so a URL excluded during a Center outage
// is offered again. Per-URL ownership is unchanged.
func TestManager_Reset_ClearsExclusions(t *testing.T) {
	urlA := "k2v5://udid:tok@a.example/?ech=x"
	urlB := "k2v5://udid:tok@b.example/?ech=x"
	mgr := &Manager{}
	mgr.sessions = []*Session{newTestSession(t, []string{urlA, urlB})}

	if got, err := mgr.NextURL(urlA, nil); err != nil || got != urlB {
		t.Fatalf("NextURL(a) = %q, %v; want b", got, err)
	}
	if _, err := mgr.NextURL(urlB, nil); err == nil {
		t.Fatal("precondition: a and b excluded must exhaust")
	}
	mgr.Reset()
	if got, err := mgr.NextURL(urlB, nil); err != nil || got != urlA {
		t.Fatalf("after Reset NextURL(b) = %q, %v; want a (exclusions cleared)", got, err)
	}
	if !mgr.OwnsURL(urlA) || !mgr.OwnsURL(urlB) {
		t.Fatal("Reset changed URL ownership")
	}
	(&Manager{}).Reset() // no sessions: no-op
}

// Reset runs on the engine's replace-loop goroutine while the shell may be
// resolving / closing sessions and the probe service reads candidates: it
// must be race-free against every other Manager method.
func TestManager_Reset_ConcurrentWithOtherMethods(t *testing.T) {
	urlA := "k2v5://udid:tok@a.example/?ech=x"
	urlB := "k2v5://udid:tok@b.example/?ech=x"
	mgr := &Manager{}
	mgr.sessions = []*Session{newTestSession(t, []string{urlA, urlB})}
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				_, _ = mgr.NextURL(urlA, nil)
				mgr.Reset()
				_ = mgr.OwnsURL(urlB)
				_ = mgr.CandidateURLs()
				_ = mgr.Sessions()
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			mgr.mu.Lock()
			mgr.sessions = []*Session{newTestSession(t, []string{urlA, urlB})}
			mgr.mu.Unlock()
		}
	}()
	wg.Wait()
}

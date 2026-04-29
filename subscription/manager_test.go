package subscription

import (
	"errors"
	"strings"
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

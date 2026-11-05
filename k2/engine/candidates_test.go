package engine

import (
	"errors"
	"testing"
	"time"
)

// fakeClock is a manually advanced clock for exclusion-TTL tests.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

const (
	candA = "k2v5://a"
	candB = "k2v5://b"
	candC = "k2v5://c"
)

func newTestStatic(clk *fakeClock) *staticProvider {
	return newStaticProvider(map[string][]string{candA: {candB, candC}}, clk.now)
}

func mustNext(t *testing.T, p OutboundProvider, cur, want string) {
	t.Helper()
	got, err := p.NextURL(cur, errors.New("dead"))
	if err != nil {
		t.Fatalf("NextURL(%q) err = %v, want %q", cur, err, want)
	}
	if got != want {
		t.Fatalf("NextURL(%q) = %q, want %q", cur, got, want)
	}
}

func exhaust(t *testing.T, p *staticProvider) {
	t.Helper()
	mustNext(t, p, candA, candB)
	mustNext(t, p, candB, candC)
	if _, err := p.NextURL(candC, nil); !errors.Is(err, ErrCandidatesExhausted) {
		t.Fatalf("NextURL(c) err = %v, want ErrCandidatesExhausted", err)
	}
}

func TestStatic_RotatesInOrderThenExhausts(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1_000_000, 0)}
	exhaust(t, newTestStatic(clk))
}

func TestStatic_ExclusionExpiresAfter10Min(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1_000_000, 0)}
	p := newTestStatic(clk)
	exhaust(t, p)
	clk.advance(candidateExcludeTTL - time.Second)
	if _, err := p.NextURL(candC, nil); !errors.Is(err, ErrCandidatesExhausted) {
		t.Fatalf("before TTL: err = %v, want ErrCandidatesExhausted", err)
	}
	clk.advance(2 * time.Second) // a/b excluded at t0 are now past 10min
	mustNext(t, p, candC, candA)
}

func TestStatic_ResetClearsExclusions(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1_000_000, 0)}
	p := newTestStatic(clk)
	exhaust(t, p)
	p.Reset()
	mustNext(t, p, candC, candA)
}

func TestStatic_OwnsURL(t *testing.T) {
	p := newTestStatic(&fakeClock{t: time.Unix(1_000_000, 0)})
	for _, u := range []string{candA, candB, candC} {
		if !p.OwnsURL(u) {
			t.Errorf("OwnsURL(%q) = false, want true", u)
		}
	}
	if p.OwnsURL("k2v5://z") {
		t.Error("OwnsURL(k2v5://z) = true, want false")
	}
}

// Mobile appext and direct engine.Start never run config.ValidateClient, so
// the static provider must enforce the k2v5-only rule itself.
func TestStaticProvider_DropsNonK2v5Entries(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1_000_000, 0)}
	p := newStaticProvider(map[string][]string{
		candA:       {"k2subs://evil", candB, "", "http://x"},
		"k2v5://e":  {},
		"k2v5://e2": {"k2subs://only"},
	}, clk.now)
	for _, u := range []string{"k2subs://evil", "http://x", "", "k2subs://only", "k2v5://e", "k2v5://e2"} {
		if p.OwnsURL(u) {
			t.Errorf("OwnsURL(%q) = true, want dropped", u)
		}
	}
	mustNext(t, p, candA, candB)
	if _, err := p.NextURL(candB, nil); !errors.Is(err, ErrCandidatesExhausted) {
		t.Fatalf("NextURL(b) err = %v, want ErrCandidatesExhausted", err)
	}
}

func TestComposite_UnknownURLIsNoCandidates(t *testing.T) {
	c := newCompositeProvider(newTestStatic(&fakeClock{t: time.Unix(1_000_000, 0)}), nil)
	if _, err := c.NextURL("k2v5://z", nil); !errors.Is(err, ErrNoCandidates) {
		t.Fatalf("err = %v, want ErrNoCandidates", err)
	}
}

func TestComposite_UsesStaticForOwnedURL(t *testing.T) {
	shell := &fakeOwnerShell{owns: candA, next: "k2v5://shell"}
	c := newCompositeProvider(newTestStatic(&fakeClock{t: time.Unix(1_000_000, 0)}), shell)
	mustNext(t, c, candA, candB)
	if shell.calls != 0 {
		t.Fatalf("shell called %d times, want 0 (static owns the URL)", shell.calls)
	}
}

type fakeOwnerShell struct {
	owns, next string
	calls      int
}

func (s *fakeOwnerShell) NextURL(string, error) (string, error) { s.calls++; return s.next, nil }
func (s *fakeOwnerShell) OwnsURL(u string) bool                 { return u == s.owns }

type fakePlainShell struct{ calls int }

func (s *fakePlainShell) NextURL(string, error) (string, error) { s.calls++; return "k2v5://x", nil }

func TestComposite_DelegatesOwnedURLToShell(t *testing.T) {
	shell := &fakeOwnerShell{owns: "k2v5://s", next: "k2v5://s2"}
	c := newCompositeProvider(newTestStatic(&fakeClock{t: time.Unix(1_000_000, 0)}), shell)
	mustNext(t, c, "k2v5://s", "k2v5://s2")
	if _, err := c.NextURL("k2v5://other", nil); !errors.Is(err, ErrNoCandidates) {
		t.Fatalf("unowned URL err = %v, want ErrNoCandidates", err)
	}
}

func TestComposite_ShellWithoutOwnerIsNoCandidates(t *testing.T) {
	shell := &fakePlainShell{}
	c := newCompositeProvider(newTestStatic(&fakeClock{t: time.Unix(1_000_000, 0)}), shell)
	if _, err := c.NextURL("k2v5://s", nil); !errors.Is(err, ErrNoCandidates) {
		t.Fatalf("err = %v, want ErrNoCandidates", err)
	}
	if shell.calls != 0 {
		t.Fatalf("shell called %d times, want 0", shell.calls)
	}
}

func TestComposite_ResetResetsStatic(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1_000_000, 0)}
	st := newTestStatic(clk)
	c := newCompositeProvider(st, nil)
	exhaust(t, st)
	c.Reset()
	mustNext(t, c, candC, candA)
}

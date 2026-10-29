package wire

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
	"time"
)

// Revalidation de-phasing (spec §6.5). Without it, every client that
// reconnected in the same second — a GFW event, a node restart — revalidates
// in the same second forever after, recreating the synchronised Center storm
// the cookie design exists to remove.

// TestAuthSession_JitterIsAssignedAndSpread pins that sessions get DIFFERENT
// jitters within the documented band. A constant (or zero) jitter would pass
// any single-session test while leaving the whole cohort in lockstep.
func TestAuthSession_JitterIsAssignedAndSpread(t *testing.T) {
	maxJitter := int64(authRevalJitterMax.Seconds())
	seen := make(map[int64]bool)
	for i := 0; i < 200; i++ {
		s := newAuthSession(newConnAuth(), func() {})
		if s.revalJitter < 0 || s.revalJitter >= maxJitter {
			t.Fatalf("jitter %d out of band [0,%d)", s.revalJitter, maxJitter)
		}
		seen[s.revalJitter] = true
	}
	// 200 draws over a 120-value band: a handful of distinct values would
	// still be a broken generator. Deliberately loose to stay non-flaky.
	if len(seen) < 20 {
		t.Errorf("jitter is not spreading sessions: only %d distinct values in 200 draws", len(seen))
	}
}

// TestAuthSession_JitterOnlyShortensWindow: the offset is subtracted, never
// added, so revalidation can fire early but never late. This is what keeps
// wire/cookie.go's ~2xcookieTTL revocation bound an upper bound.
func TestAuthSession_JitterOnlyShortensWindow(t *testing.T) {
	base := time.Now()
	ttl := int64(cookieTTL.Seconds())

	// A session with the maximum possible jitter, checked exactly at the
	// un-jittered TTL boundary: it must already be considered stale.
	s := newAuthSession(newConnAuth(), func() {})
	s.revalJitter = int64(authRevalJitterMax.Seconds()) - 1
	s.markAuthed("dev", "tok", "", base.Unix(), 0)

	called := 0
	a := &authSessions{
		validate: func(string, string, string) AuthVerdict { called++; return AuthVerdict{OK: true} },
		// One second past this session's own (shortened) window, which is
		// still well inside the un-jittered cookieTTL.
		now: func() time.Time { return base.Add(time.Duration(ttl-s.revalJitter+1) * time.Second) },
		set:      map[*authSession]struct{}{},
	}
	a.reviewSession(s)
	if called != 1 {
		t.Fatalf("a jittered session must revalidate BEFORE the full TTL; validator calls = %d", called)
	}

	// The same session well inside its own shortened window must not call out.
	called = 0
	a.now = func() time.Time { return base.Add(time.Second) }
	s.markAuthed("dev", "tok", "", base.Unix(), 0) // once-guarded; authAt unchanged
	s.authAt.Store(base.Unix())
	a.reviewSession(s)
	if called != 0 {
		t.Errorf("a fresh session must not revalidate; validator calls = %d", called)
	}
}

// TestAuthSession_ConstructedOnlyViaConstructor is a structural guard, not a
// behavioural one. A hand-built &authSession{} compiles fine and gets
// revalJitter == 0, silently rejoining the lockstep cohort — a defect no
// single-session test can observe. So the construction path itself is pinned.
func TestAuthSession_ConstructedOnlyViaConstructor(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read wire dir: %v", err)
	}
	fset := token.NewFileSet()
	scanned := 0
	for _, e := range entries {
		name := e.Name()
		// Production sources only: tests legitimately build sessions by hand
		// to pin a specific jitter (see the test above).
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		scanned++
		ast.Inspect(file, func(n ast.Node) bool {
			// newAuthSession is the one place allowed to build the literal —
			// it is what assigns the jitter.
			if fn, ok := n.(*ast.FuncDecl); ok && fn.Name.Name == "newAuthSession" {
				return false
			}
			lit, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			id, ok := lit.Type.(*ast.Ident)
			if !ok || id.Name != "authSession" {
				return true
			}
			t.Errorf("%s: authSession built as a composite literal — use newAuthSession() "+
				"or the session silently gets revalJitter=0 (see authRevalJitterMax)",
				fset.Position(lit.Pos()))
			return true
		})
	}
	if scanned == 0 {
		t.Fatal("scanned no production files — the guard would pass vacuously")
	}
}

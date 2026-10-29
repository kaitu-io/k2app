package wire

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestAdmissionGate_EgressSinksAreGated is the structural guard on the
// device-auth enforce gate.
//
// Three egress paths have shipped ungated — TCP-WS proxy streams, QUIC
// datagrams (the PRIMARY UDP path), and the QUIC overflow stream — each found
// only in a later review. They were not the same bug three times; they were the
// same *shape* of bug: the gate was a check to be repeated at every dispatch
// site, so forgetting it failed OPEN and failed SILENTLY.
//
// The fix is not another check. Egress needs two things and only two: a way to
// hand a proxy stream to AcceptTCP, and the server's UDPHandler. Both are handed
// out exclusively by the admission gate (admitTCP / admitUDP), so a path that
// forgets to ask has nothing to proxy with. This test pins that property: it
// fails the moment someone reaches an egress sink without going through the
// gate, which is precisely the moment the next silent bypass would be written.
func TestAdmissionGate_EgressSinksAreGated(t *testing.T) {
	// sink → the only functions allowed to touch it.
	allowed := map[string]map[string]bool{
		// The server's UDPHandler: obtaining it IS the UDP admission gate.
		"udpH": {
			"admitUDP": true, // the gate
			"ServeUDP": true, // the setter
		},
		// The TCP egress queue drained by AcceptTCP.
		"tcpCh": {
			"sendTCP":        true, // the only sender
			"AcceptTCP":      true, // the receiver
			"NewQUICServer":  true, // construction
			"NewTCPWSServer": true, // construction
		},
	}
	// Calls that emit TCP egress, and who may make them.
	allowedCalls := map[string]map[string]bool{
		"sendTCP": {
			"admitTCP":   true, // the gate
			"acceptLoop": true, // propagates a listener error, never a client conn
		},
	}

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var violations []string
	scanned := 0

	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, src, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		scanned++

		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			enclosing := fn.Name.Name
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch node := n.(type) {
				case *ast.SelectorExpr:
					if callers, guarded := allowed[node.Sel.Name]; guarded && !callers[enclosing] {
						violations = append(violations, fmt.Sprintf(
							"%s: %s touches egress sink %q — only %s may",
							fset.Position(node.Pos()), enclosing, node.Sel.Name, names(callers)))
					}
				case *ast.CallExpr:
					sel, ok := node.Fun.(*ast.SelectorExpr)
					if !ok {
						return true
					}
					if callers, guarded := allowedCalls[sel.Sel.Name]; guarded && !callers[enclosing] {
						violations = append(violations, fmt.Sprintf(
							"%s: %s calls %s() — only %s may",
							fset.Position(node.Pos()), enclosing, sel.Sel.Name, names(callers)))
					}
				}
				return true
			})
		}
	}

	if scanned == 0 {
		t.Fatal("scanned no source files — the guard would pass vacuously")
	}
	if len(violations) > 0 {
		sort.Strings(violations)
		t.Fatalf("egress reachable without device-auth admission:\n  %s\n\n"+
			"Egress must be obtained from the admission gate (admitTCP / admitUDP), not\n"+
			"guarded by a check at the call site: a check can be forgotten, and it has been\n"+
			"three times. If this is a genuinely new gate, widen the whitelist above.",
			strings.Join(violations, "\n  "))
	}
}

func names(m map[string]bool) string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return strings.Join(out, "/")
}

// TestAdmissionPolicy covers the admission decision itself — the enforce /
// permissive split and the timing safety around a proxy path that races ahead
// of the metadata stream. (Formerly TestProxyAuthorized, now transport-agnostic:
// QUIC and TCP-WS share one implementation.)
func TestAdmissionPolicy(t *testing.T) {
	t.Run("permissive allows even unauthenticated", func(t *testing.T) {
		p := admissionPolicy{enforce: false, authWait: 10 * time.Millisecond}
		if !p.admit(newConnAuth()) {
			t.Error("permissive must never block — attribution only")
		}
	})

	t.Run("enforce allows authenticated", func(t *testing.T) {
		p := admissionPolicy{enforce: true, authWait: 10 * time.Millisecond}
		a := newConnAuth()
		a.settle(true)
		if !p.admit(a) {
			t.Error("authenticated connection must pass under enforce")
		}
	})

	t.Run("enforce rejects unauthenticated after metadata processed", func(t *testing.T) {
		p := admissionPolicy{enforce: true, authWait: time.Second}
		a := newConnAuth()
		a.settle(false) // metadata seen, validation failed
		start := time.Now()
		if p.admit(a) {
			t.Error("failed-auth connection must be rejected under enforce")
		}
		if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
			t.Errorf("a settled verdict must not wait, waited %v", elapsed)
		}
	})

	t.Run("enforce rejects no-metadata client after timeout", func(t *testing.T) {
		p := admissionPolicy{enforce: true, authWait: 20 * time.Millisecond}
		a := newConnAuth() // never settles — legacy client that sends no metadata
		start := time.Now()
		if p.admit(a) {
			t.Error("no-metadata client must be rejected under enforce")
		}
		if elapsed := time.Since(start); elapsed < 15*time.Millisecond {
			t.Errorf("should have waited ~authWait for late metadata, waited %v", elapsed)
		}
	})

	t.Run("enforce tolerates proxy-before-metadata race", func(t *testing.T) {
		p := admissionPolicy{enforce: true, authWait: 500 * time.Millisecond}
		a := newConnAuth()
		// Metadata lands shortly AFTER the proxy path — a legitimate client whose
		// streams the server accepted out of order.
		go func() {
			time.Sleep(20 * time.Millisecond)
			a.settle(true)
		}()
		if !p.admit(a) {
			t.Error("a valid client whose proxy stream raced ahead must wait then pass")
		}
	})

	t.Run("settle is idempotent and first-wins", func(t *testing.T) {
		a := newConnAuth()
		a.settle(true)
		a.settle(false) // e.g. a deferred fail-closed backstop running after success
		if !a.authenticated() {
			t.Error("the first verdict must win — a deferred backstop must not undo it")
		}
	})
}

// TestConnAuth_ClaimRolloutCount_SequentialSecondCallLoses pins the simple,
// deterministic half of the rollout-counter double-count fix: once one caller
// has claimed the connection's rollout-counter slot, every later caller —
// e.g. a second metadata stream's recordVerdict, or the teardown defer racing
// behind an already-completed verdict — must lose the claim.
func TestConnAuth_ClaimRolloutCount_SequentialSecondCallLoses(t *testing.T) {
	a := newConnAuth()
	if !a.claimRolloutCount() {
		t.Fatal("first claim on a fresh connAuth must win")
	}
	if a.claimRolloutCount() {
		t.Fatal("second claim must lose — a connection may contribute at most one rollout-counter increment")
	}
	if a.claimRolloutCount() {
		t.Fatal("claim must stay lost on repeated calls, not just the second one")
	}
}

// TestConnAuth_ClaimRolloutCount_ConcurrentRaceHasExactlyOneWinner is the
// direct concurrency proof for the CAS itself: this is what stands in for the
// real teardown-defer-vs-in-stream-verdict goroutine race (Important #1),
// which is timing-dependent and impractical to reproduce deterministically.
// Many goroutines hit claimRolloutCount() simultaneously on the SAME
// connAuth — exactly the shape of the race between the accept loop's
// teardown defer and handleMetadataStream (or between N concurrent metadata
// streams on one connection) — and exactly one must observe a win.
func TestConnAuth_ClaimRolloutCount_ConcurrentRaceHasExactlyOneWinner(t *testing.T) {
	const n = 64
	a := newConnAuth()
	var wins atomic.Int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			<-start // maximize contention: release every goroutine at once
			if a.claimRolloutCount() {
				wins.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()

	if got := wins.Load(); got != 1 {
		t.Fatalf("claimRolloutCount: %d of %d concurrent callers won, want exactly 1", got, n)
	}
}

package engine

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kaitu-io/k2/wire"
)

// failSiteAllowList: the only functions (by name) in the engine package's
// non-test files that may reference e.fail / failExhausted (call or method
// value). R1: the replace loop ends the session through exactly two exits —
// an account verdict (failIfUserAction) and sustained online exhaustion
// (failAtExhaustCap → failExhausted). Start's e.fail calls are the session
// setup errors, outside the replace loop.
var failSiteAllowList = map[string]map[string]bool{
	"fail":          {"Start": true, "failIfUserAction": true, "failExhausted": true},
	"failExhausted": {"failAtExhaustCap": true},
}

// failSites scans the engine package's non-test .go files under dir and
// returns "callee ← function (file:line)" for every reference to a method
// named fail / failExhausted outside its allow-list, plus the per-callee
// count of allowed sites.
func failSites(t *testing.T, dir string) (violations []string, allowed map[string]int) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	allowed = map[string]int{}
	fset := token.NewFileSet()
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		if f.Name.Name != "engine" {
			continue
		}
		var visit func(fn string, n ast.Node)
		visit = func(fn string, n ast.Node) {
			ast.Inspect(n, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				allow, watched := failSiteAllowList[sel.Sel.Name]
				if !watched {
					return true
				}
				if allow[fn] {
					allowed[sel.Sel.Name]++
					return true
				}
				pos := fset.Position(sel.Pos())
				violations = append(violations, sel.Sel.Name+" ← "+fn+" ("+filepath.Base(pos.Filename)+":"+strconv.Itoa(pos.Line)+")")
				return true
			})
		}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				visit(d.Name.Name, d.Body)
			default:
				visit("<package-level>", d)
			}
		}
	}
	sort.Strings(violations)
	return violations, allowed
}

// TestReplaceLoopFailSitesAllowList is the R1 structural guard: a new
// e.fail / failExhausted reference anywhere in the engine package outside the
// allow-list turns it red — a spurious "node dead" path can no longer end the
// session by adding a call. failExhausted has exactly one caller (the cap),
// failIfUserAction exactly one e.fail.
func TestReplaceLoopFailSitesAllowList(t *testing.T) {
	violations, allowed := failSites(t, ".")
	if len(violations) > 0 {
		t.Fatalf("e.fail / failExhausted referenced outside the allow-list (R1: the replace loop may end the session only via failIfUserAction or the exhaustion cap):\n  %s",
			strings.Join(violations, "\n  "))
	}
	if allowed["failExhausted"] != 1 {
		t.Fatalf("failExhausted references = %d, want exactly 1 (failAtExhaustCap)", allowed["failExhausted"])
	}
	// fail: Start's setup errors + failIfUserAction + failExhausted. The
	// scanner must have seen them, or it is scanning nothing.
	if allowed["fail"] < 3 {
		t.Fatalf("allowed fail references = %d, want ≥ 3: the scan is not reaching engine.go", allowed["fail"])
	}
}

// The guard itself: a scratch copy of the package with one extra e.fail in a
// replace-loop function must be flagged (so a broken scanner cannot pass).
func TestReplaceLoopFailSitesGuardCatchesNewSite(t *testing.T) {
	dir := t.TempDir()
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, f), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	p := filepath.Join(dir, "hold.go")
	b, _ := os.ReadFile(p)
	b = append(b, []byte("\nfunc (e *Engine) zzProbeFail() { _ = e.fail(nil) }\n"+
		"func (e *Engine) zzProbeExhaust(t *outboundTransport) { e.failExhausted(t, nil) }\n")...)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	violations, _ := failSites(t, dir)
	if len(violations) != 2 {
		t.Fatalf("guard flagged %v, want the two injected sites", violations)
	}
}

// R1 race budget: every engine race ctx outlives the wire race's own worst
// case by ≥ 1s (otherwise a slow-but-alive node's metadata verdict is cut
// into a bare timeout = node death, PVE S15). The node-side bound
// (metaReadTimeout > k2s remote auth) lives with both constants in wire:
// wire TestMetaReadTimeoutOutlivesNodeAuthTimeout.
func TestRaceCtxOutlivesWireWorstCase(t *testing.T) {
	worst := wire.RaceWorstCase()
	if raceCtxTimeout-worst < time.Second {
		t.Fatalf("raceCtxTimeout %v - wire.RaceWorstCase %v = %v, want ≥ 1s", raceCtxTimeout, worst, raceCtxTimeout-worst)
	}
	t.Logf("wire.RaceWorstCase = %v, engine raceCtxTimeout = %v", worst, raceCtxTimeout)
}

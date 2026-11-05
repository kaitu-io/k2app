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

// failSiteAllowList: the only functions (key "Type.Method", or "Func";
// a closure is "<enclosing>.func" and is never allowed) in the engine
// package's non-test files that may reference these names. R1: the replace
// loop ends the session through exactly two exits — an account verdict
// (failIfUserAction) and sustained online exhaustion (failAtExhaustCap →
// failExhausted). Start's e.fail calls are the session setup errors.
//
// engineOnly names must be selected on the enclosing *Engine method's own
// receiver ("e.fail"): on any other expression (an alias, another type) the
// fail-ish ones are a violation outright, while Stop / cancel /
// reRaceCancel on a non-engine receiver (t.Stop() on a timer) are ignored.
// OnOutboundFatal (k2r / desktop gateway rebuild = the session ends) is
// matched on any receiver.
var failSiteAllowList = map[string]map[string]bool{
	"fail":                     {"Engine.Start": true, "Engine.failIfUserAction": true, "Engine.failExhausted": true},
	"failExhausted":            {"Engine.failAtExhaustCap": true},
	"OnOutboundFatal":          {"Engine.failIfUserAction": true, "Engine.failExhausted": true},
	"failIfUserAction":         {"Engine.outboundReplaceLoop": true, "Engine.escalate": true, "Engine.holdUntilOnline": true, "Engine.retryOnce": true, "Engine.failIfExhaustedByAccount": true},
	"failIfExhaustedByAccount": {"Engine.escalate": true, "Engine.retryPhaseB": true},
	"Stop":                     {},
	"cancel":                   {"Engine.Start": true, "Engine.Stop": true, "Engine.fail": true},
	"reRaceCancel":             {"Engine.Start": true, "Engine.Stop": true, "Engine.fail": true},
}

// engineOnly: names checked against the enclosing method's receiver;
// strictReceiver: of those, the ones where any other receiver is a
// violation (an alias of e, or a look-alike method).
var (
	engineOnly     = map[string]bool{"fail": true, "failExhausted": true, "failIfUserAction": true, "failIfExhaustedByAccount": true, "Stop": true, "cancel": true, "reRaceCancel": true}
	strictReceiver = map[string]bool{"fail": true, "failExhausted": true, "failIfUserAction": true, "failIfExhaustedByAccount": true}
)

// failSites scans the engine package's non-test .go files under dir and
// returns "name ← function (file:line)" for every watched reference outside
// its allow-list, plus the per-name count of allowed references.
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
		report := func(name, key string, pos token.Pos) {
			p := fset.Position(pos)
			violations = append(violations, name+" ← "+key+" ("+filepath.Base(p.Filename)+":"+strconv.Itoa(p.Line)+")")
		}
		// visit walks n as part of function key; recv is the *Engine
		// receiver's name ("" when key is not an Engine method).
		var visit func(key, recv string, n ast.Node)
		visit = func(key, recv string, n ast.Node) {
			ast.Inspect(n, func(n ast.Node) bool {
				if lit, ok := n.(*ast.FuncLit); ok {
					visit(key+".func", recv, lit.Body)
					return false
				}
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				name := sel.Sel.Name
				allow, watched := failSiteAllowList[name]
				if !watched {
					return true
				}
				if engineOnly[name] {
					id, isIdent := sel.X.(*ast.Ident)
					onEngine := isIdent && recv != "" && id.Name == recv
					if !onEngine {
						if strictReceiver[name] {
							report(name+" (not on the Engine receiver)", key, sel.Pos())
						}
						return true
					}
				}
				if allow[key] {
					allowed[name]++
					return true
				}
				report(name, key, sel.Pos())
				return true
			})
		}
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok {
				visit("<package-level>", "", d)
				continue
			}
			if fd.Body == nil {
				continue
			}
			key, recv := fd.Name.Name, ""
			if fd.Recv != nil && len(fd.Recv.List) == 1 {
				typ := fd.Recv.List[0].Type
				if st, ok := typ.(*ast.StarExpr); ok {
					typ = st.X
				}
				if id, ok := typ.(*ast.Ident); ok {
					key = id.Name + "." + fd.Name.Name
					if id.Name == "Engine" && len(fd.Recv.List[0].Names) == 1 {
						recv = fd.Recv.List[0].Names[0].Name
					}
				}
			}
			visit(key, recv, fd.Body)
		}
	}
	sort.Strings(violations)
	return violations, allowed
}

// TestReplaceLoopFailSitesAllowList is the R1 structural guard: a new
// session-ending reference (e.fail, failExhausted, OnOutboundFatal, a new
// caller of failIfUserAction / failIfExhaustedByAccount, e.Stop / e.cancel /
// e.reRaceCancel) anywhere in the engine package outside the allow-list
// turns it red — a spurious "node dead" path can no longer end the session
// by adding a call.
func TestReplaceLoopFailSitesAllowList(t *testing.T) {
	violations, allowed := failSites(t, ".")
	if len(violations) > 0 {
		t.Fatalf("session-ending reference outside the allow-list (R1: the replace loop may end the session only via failIfUserAction or the exhaustion cap):\n  %s",
			strings.Join(violations, "\n  "))
	}
	if allowed["failExhausted"] != 1 {
		t.Fatalf("failExhausted references = %d, want exactly 1 (failAtExhaustCap)", allowed["failExhausted"])
	}
	if allowed["OnOutboundFatal"] != 2 {
		t.Fatalf("OnOutboundFatal references = %d, want exactly 2 (failIfUserAction, failExhausted)", allowed["OnOutboundFatal"])
	}
	// The scanner must see the known sites, or it is scanning nothing.
	if allowed["fail"] < 3 || allowed["failIfUserAction"] < 5 || allowed["failIfExhaustedByAccount"] < 2 || allowed["cancel"] < 3 {
		t.Fatalf("allowed counts %v: the scan is not reaching the known sites", allowed)
	}
}

// The guard itself: each injected reference, in a scratch copy of the
// package, must be flagged exactly once (so a broken scanner cannot pass).
func TestReplaceLoopFailSitesGuardCatchesNewSite(t *testing.T) {
	cases := map[string]string{
		"new e.fail":                     "func (e *Engine) zzA() { _ = e.fail(nil) }",
		"new failExhausted caller":       "func (e *Engine) zzA(t *outboundTransport) { e.failExhausted(t, nil) }",
		"new OnOutboundFatal":            "func (e *Engine) zzA(h EventHandler) { h.OnOutboundFatal(\"x\", nil) }",
		"new failIfUserAction caller":    "func (e *Engine) zzA(t *outboundTransport) { _ = e.failIfUserAction(t, nil) }",
		"new failIfExhaustedByAccount":   "func (e *Engine) zzA(t *outboundTransport) { _ = e.failIfExhaustedByAccount(t, nil) }",
		"e.Stop from loop code":          "func (e *Engine) retryOnceZZ() { _ = e.Stop() }",
		"e.cancel from loop code":        "func (e *Engine) retryOnceZZ() { e.cancel() }",
		"e.reRaceCancel from loop code":  "func (e *Engine) retryOnceZZ() { e.reRaceCancel() }",
		"aliased receiver":               "func (e *Engine) zzA() { x := e; _ = x.fail(nil) }",
		"plain func with an *Engine":    "func zzB(e *Engine) { _ = e.fail(nil) }",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
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
				if f == "hold.go" {
					b = append(b, []byte("\n"+body+"\n")...)
				}
				if err := os.WriteFile(filepath.Join(dir, f), b, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			violations, _ := failSites(t, dir)
			if len(violations) != 1 {
				t.Fatalf("guard flagged %v, want exactly the injected site", violations)
			}
		})
	}
}

// A closure inside an allow-listed function is not allowed.
func TestReplaceLoopFailSitesClosureNotAllowed(t *testing.T) {
	dir := t.TempDir()
	src := "package engine\n\ntype Engine struct{}\n\nfunc (e *Engine) fail(error) error { return nil }\n\n" +
		"func (e *Engine) Start() { go func() { _ = e.fail(nil) }() }\n"
	if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	violations, _ := failSites(t, dir)
	if len(violations) != 1 || !strings.Contains(violations[0], "Engine.Start.func") {
		t.Fatalf("violations = %v, want the closure in Start", violations)
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

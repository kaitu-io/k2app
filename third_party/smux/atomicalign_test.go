package smux

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

// sync/atomic's 64-bit functions need an 8-byte-aligned address, which a
// struct field is not guaranteed on 32-bit targets (arm, 386, mips — k2r's
// OpenWRT builds); a misaligned one panics on first use. atomic.Int64 /
// atomic.Uint64 are always aligned. Ban the functions so a new counter
// can't reintroduce it (Session.reqSeq did: offset 148 on 386).
func TestNo64BitAtomicFuncs(t *testing.T) {
	banned := map[string]bool{}
	for _, op := range []string{"Add", "Load", "Store", "Swap", "CompareAndSwap"} {
		banned[op+"Int64"], banned[op+"Uint64"] = true, true
	}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, f := range files {
		af, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(af, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if x, ok := sel.X.(*ast.Ident); ok && x.Name == "atomic" && banned[sel.Sel.Name] {
				t.Errorf("%s: atomic.%s — use atomic.Int64/atomic.Uint64 (32-bit alignment)", fset.Position(sel.Pos()), sel.Sel.Name)
			}
			return true
		})
	}
}

package lint

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestConcurrencyLint_CloseUnderLock scans all .go files in the k2 module for
// .Close() calls inside mu.Lock()→mu.Unlock() spans (Rule #4 violation).
// Only flags state locks (field named "mu"), not serialization locks
// (connectMu, initUDPMu, writeMu, etc.) which are allowed to hold across I/O.
//
// Allowlist: add "// concurrency:allow — [reason]" on the Lock() line to skip.
func TestConcurrencyLint_CloseUnderLock(t *testing.T) {
	root := findModuleRoot(t)
	violations := 0

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			base := filepath.Base(path)
			if base == "vendor" || base == ".git" || base == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		// Skip test files — lock-probe tests intentionally do Lock/Unlock.
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}

		fset := token.NewFileSet()
		f, parseErr := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if parseErr != nil {
			return nil // skip unparseable files
		}

		ast.Inspect(f, func(n ast.Node) bool {
			fn, ok := n.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				return true
			}
			violations += checkCloseUnderLock(t, fset, fn.Body.List, path)
			return false // don't recurse into nested func decls
		})

		// Also check func literals (closures, goroutines).
		ast.Inspect(f, func(n ast.Node) bool {
			fl, ok := n.(*ast.FuncLit)
			if !ok || fl.Body == nil {
				return true
			}
			violations += checkCloseUnderLock(t, fset, fl.Body.List, path)
			return true
		})

		return nil
	})
	if err != nil {
		t.Fatalf("walk error: %v", err)
	}
	if violations > 0 {
		t.Fatalf("found %d Close() under state lock violations (Rule #4)", violations)
	}
}

// checkCloseUnderLock scans a statement list for mu.Lock→mu.Unlock spans
// containing .Close() calls. Only tracks state locks (field "mu").
// Handles Unlock-inside-if-then-return as an unlock point.
func checkCloseUnderLock(t *testing.T, fset *token.FileSet, stmts []ast.Stmt, path string) int {
	t.Helper()
	violations := 0
	inLock := false
	var lockPos token.Pos

	for _, stmt := range stmts {
		if isStateLockCall(stmt) {
			if hasAllowComment(fset, stmt, path) {
				// Skip ahead to next Unlock at this level.
				inLock = false
				continue
			}
			inLock = true
			lockPos = stmt.Pos()
			continue
		}

		if isStateUnlockCall(stmt) {
			inLock = false
			continue
		}

		if !inLock {
			continue
		}

		// Check for if-block that contains Unlock+return — the code after
		// the if is no longer under lock for the "unlocked" path, but Close()
		// inside the if-block (after Unlock) should not be flagged.
		// We scan for Close() calls but skip those that appear after an
		// Unlock() within the same block.
		findCloseViolations(t, fset, stmt, path, lockPos, &violations)
	}

	return violations
}

// findCloseViolations walks a statement looking for Close() calls that appear
// while the state lock is held. It tracks unlock points within if/switch blocks.
func findCloseViolations(t *testing.T, fset *token.FileSet, stmt ast.Stmt, path string, lockPos token.Pos, violations *int) {
	t.Helper()

	// For if statements, check each branch for unlock-then-close patterns.
	if ifStmt, ok := stmt.(*ast.IfStmt); ok {
		checkBlockForCloseViolations(t, fset, ifStmt.Body.List, path, lockPos, violations)
		if ifStmt.Else != nil {
			if elseBlock, ok := ifStmt.Else.(*ast.BlockStmt); ok {
				checkBlockForCloseViolations(t, fset, elseBlock.List, path, lockPos, violations)
			} else {
				findCloseViolations(t, fset, ifStmt.Else, path, lockPos, violations)
			}
		}
		return
	}

	// For other statements, just look for Close() calls.
	ast.Inspect(stmt, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if sel.Sel.Name == "Close" || sel.Sel.Name == "CloseWithError" || sel.Sel.Name == "close" {
			relPath, _ := filepath.Rel(findModuleRootCached(), path)
			if relPath == "" {
				relPath = path
			}
			pos := fset.Position(call.Pos())
			lockPosInfo := fset.Position(lockPos)
			t.Errorf("Rule #4 violation: %s:%d — .%s() called under state lock (locked at line %d)",
				relPath, pos.Line, sel.Sel.Name, lockPosInfo.Line)
			*violations++
		}
		return true
	})
}

// checkBlockForCloseViolations checks a block's statements, skipping Close()
// calls that appear after an Unlock() within the same block.
func checkBlockForCloseViolations(t *testing.T, fset *token.FileSet, stmts []ast.Stmt, path string, lockPos token.Pos, violations *int) {
	t.Helper()
	for _, s := range stmts {
		if isStateUnlockCall(s) {
			return // everything after unlock in this block is fine
		}
		findCloseViolations(t, fset, s, path, lockPos, violations)
	}
}

// TestConcurrencyLint_DeferUnlockScope scans for defer mu.Unlock() in
// functions longer than 15 lines (Rule #5 violation). Only flags state locks.
func TestConcurrencyLint_DeferUnlockScope(t *testing.T) {
	root := findModuleRoot(t)
	violations := 0

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			base := filepath.Base(path)
			if base == "vendor" || base == ".git" || base == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		fset := token.NewFileSet()
		f, parseErr := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if parseErr != nil {
			return nil
		}

		ast.Inspect(f, func(n ast.Node) bool {
			fn, ok := n.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				return true
			}

			bodyStart := fset.Position(fn.Body.Lbrace).Line
			bodyEnd := fset.Position(fn.Body.Rbrace).Line
			bodyLines := bodyEnd - bodyStart + 1

			if bodyLines <= 15 {
				return true
			}

			for _, stmt := range fn.Body.List {
				ds, ok := stmt.(*ast.DeferStmt)
				if !ok {
					continue
				}
				if isDeferStateUnlock(ds) {
					if hasAllowComment(fset, stmt, path) {
						continue
					}
					relPath, _ := filepath.Rel(findModuleRootCached(), path)
					if relPath == "" {
						relPath = path
					}
					pos := fset.Position(ds.Pos())
					t.Errorf("Rule #5 violation: %s:%d — defer mu.Unlock() in %d-line function %s (max 15)",
						relPath, pos.Line, bodyLines, fn.Name.Name)
					violations++
				}
			}
			return true
		})

		return nil
	})
	if err != nil {
		t.Fatalf("walk error: %v", err)
	}
	if violations > 0 {
		t.Fatalf("found %d defer Unlock() scope violations (Rule #5)", violations)
	}
}

// --- helpers ---

var cachedRoot string

func findModuleRoot(t *testing.T) string {
	t.Helper()
	if cachedRoot != "" {
		return cachedRoot
	}
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			cachedRoot = dir
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find go.mod")
		}
		dir = parent
	}
}

func findModuleRootCached() string {
	return cachedRoot
}

// lockFieldName extracts the mutex field name from a call like x.mu.Lock().
// Returns the field name (e.g. "mu", "connectMu") or "" if not a field lock call.
func lockFieldName(call *ast.CallExpr) string {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	// sel is .Lock()/.Unlock() — the receiver should be x.mu (another selector).
	innerSel, ok := sel.X.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	return innerSel.Sel.Name
}

// isStateLockCall checks if stmt is x.mu.Lock() or x.mu.RLock() where field name is "mu".
func isStateLockCall(stmt ast.Stmt) bool {
	es, ok := stmt.(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := es.X.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if sel.Sel.Name != "Lock" && sel.Sel.Name != "RLock" {
		return false
	}
	return lockFieldName(call) == "mu"
}

// isStateUnlockCall checks if stmt is x.mu.Unlock() or x.mu.RUnlock() where field name is "mu".
func isStateUnlockCall(stmt ast.Stmt) bool {
	es, ok := stmt.(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := es.X.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if sel.Sel.Name != "Unlock" && sel.Sel.Name != "RUnlock" {
		return false
	}
	return lockFieldName(call) == "mu"
}

// isDeferStateUnlock checks if a defer statement is deferring x.mu.Unlock()
// where field name is "mu".
func isDeferStateUnlock(ds *ast.DeferStmt) bool {
	sel, ok := ds.Call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if sel.Sel.Name != "Unlock" && sel.Sel.Name != "RUnlock" {
		return false
	}
	return lockFieldName(ds.Call) == "mu"
}

// hasAllowComment checks if the statement has a "// concurrency:allow" comment
// on the same line or up to 3 lines above (covers multi-line comments).
func hasAllowComment(fset *token.FileSet, stmt ast.Stmt, path string) bool {
	pos := fset.Position(stmt.Pos())
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	lines := strings.Split(string(data), "\n")
	for _, lineIdx := range []int{pos.Line - 1, pos.Line - 2, pos.Line - 3, pos.Line - 4} {
		if lineIdx >= 0 && lineIdx < len(lines) {
			if strings.Contains(lines[lineIdx], "concurrency:allow") {
				return true
			}
		}
	}
	return false
}

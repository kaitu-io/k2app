package wire

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCredentials_NeverPassedToLogSinks is the Phase 1 static guard for spec §9
// "凭据不入日志". A logged credential is not hypothetical: desktop ships an S3
// log-upload channel (desktop/CLAUDE.md), so one leaked token/NodeSecret leaves
// the device and lands in searchable storage. Human review misses this every
// time (§6.2).
//
// It scans wire/ AND server/ (both touch device-auth) for call sites that would
// interpolate a credential-named identifier into a log OR an error:
//   - slog.{Debug,Info,Warn,Error} and *slog.Logger method calls
//   - fmt.Errorf / fmt.Sprintf / errors.New  — scanning slog ALONE misses these:
//     `fmt.Errorf("...%s", token)` puts the credential inside an error whose own
//     arg name is `err`, and a downstream `slog.Error(..., "err", err)` then
//     leaks it (spec §9's error-wrapping hole).
// Any *ast.Ident or *ast.SelectorExpr arg whose (trailing) name is in the
// denylist fails the test. String-literal KEYS ("token") are fine — only
// identifier/selector VALUES are flagged.
//
// Phase 1 denylist: token / NodeSecret only. `cookie` is added in Phase 2.
// DOCUMENTED BLIND SPOTS (this grep-shaped guard is a complement, not a
// substitute — §9's durable fix is a redacting String()/LogValue() type):
//   - a credential first copied into an innocuously-named local
//     (`s := meta.Token; slog.Info(..., s)`),
//   - passed through a helper, or built into a string earlier via a %-verb.
func TestCredentials_NeverPassedToLogSinks(t *testing.T) {
	denylist := map[string]bool{"token": true, "nodesecret": true, "secrettoken": true}
	sinks := map[string]bool{
		"Debug": true, "Info": true, "Warn": true, "Error": true, // slog + *slog.Logger
		"Errorf": true, "Sprintf": true, "New": true, // fmt.Errorf / fmt.Sprintf / errors.New
	}

	var files []string
	for _, dir := range []string{".", "../server"} {
		g, _ := filepath.Glob(filepath.Join(dir, "*.go"))
		files = append(files, g...)
	}

	var violations []string
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
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !sinks[sel.Sel.Name] {
				return true
			}
			for _, arg := range call.Args {
				name := ""
				switch a := arg.(type) {
				case *ast.Ident: // bare local/param: token, nodeSecret
					name = a.Name
				case *ast.SelectorExpr: // field: meta.Token, node.SecretToken
					name = a.Sel.Name
				}
				if denylist[strings.ToLower(name)] {
					violations = append(violations, fmt.Sprintf(
						"%s: %s(..., %s, ...) passes a credential to a log/error sink",
						fset.Position(call.Pos()), sel.Sel.Name, name))
				}
			}
			return true
		})
	}
	if len(violations) > 0 {
		t.Fatalf("credential(s) reachable by log upload:\n%s", strings.Join(violations, "\n"))
	}
}

package wire

import (
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Credentials must never reach logs (spec §6.2 end + §9): desktop uploads
// device logs to S3, so anything logged leaves the device into retrievable
// storage. This guard scans EVERY slog call in wire/ and server/ and fails
// when a credential-shaped expression appears as an argument. Log key_id,
// an 8-byte session_id prefix, and verdicts — never cookie/token/secret
// values. Precedent: server NodeSecret is already marked "never log".
//
// Static because this class of constraint rots under manual review: the next
// slog line added months from now must fail CI, not wait for a reviewer.
//
// KNOWN BLIND SPOT (spec §9): this guard only inspects the DIRECT arguments of
// slog calls. It does NOT catch credentials laundered through an error or a
// pre-built string: `fmt.Errorf("... %s", cookie)` / `fmt.Sprintf(... token ...)`
// stores the secret in a value whose name is `err`/`msg`, and a later
// `slog.Error(..., "err", err)` leaks it while presenting an innocent argument
// name. Closing that hole fully needs either (a) extending this scan to
// fmt.Errorf/fmt.Sprintf/errors.New call sites that interpolate cookie/token/
// nodeSecret-named variables, or (b) — more robust — carrying these credentials
// in a type with a redacting String()/LogValue() so any accidental inclusion
// self-redacts. Until one of those lands, err-wrapping leaks are on human
// review. This limitation is recorded honestly rather than papered over.

// forbiddenLogValue matches identifiers that carry credential material.
var forbiddenLogValue = regexp.MustCompile(`(?i)\b(token|cookie|secret|jwt)\b`)

// logValueAllowlist holds exact expression texts that MENTION a credential
// but do not CONTAIN one. Every addition needs a justifying comment.
var logValueAllowlist = map[string]bool{
	"st.cookieHit": true, // bool: whether the cookie fast path was taken
	"cookieHit":    true, // same, local copy
	// bool: whether the server issued a cookie (metadata_client.go sendMetadata),
	// not the cookie value itself. See slog.Debug("wire: metadata auth OK", ...).
	`cookie != ""`: true,
}

func TestNoCredentialsInLogs(t *testing.T) {
	fset := token.NewFileSet()
	for _, dir := range []string{".", "../server"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			name := e.Name()
			if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			path := filepath.Join(dir, name)
			f, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || !isSlogCall(call) {
					return true
				}
				for _, arg := range call.Args {
					if _, isLit := arg.(*ast.BasicLit); isLit {
						continue // string keys / literals carry no runtime value
					}
					src := exprSource(fset, arg)
					if logValueAllowlist[src] {
						continue
					}
					if forbiddenLogValue.MatchString(src) {
						t.Errorf("%s: credential-shaped expression %q passed to a log call — log key_id / session prefix / verdict instead (spec §6.2/§9)",
							fset.Position(arg.Pos()), src)
					}
				}
				return true
			})
		}
	}
}

// isSlogCall matches slog.<Level>/With and <someLogger>.<Level>/With where the
// receiver text contains "log" (slog, connLog, log, logger, …). Over-matching
// is acceptable: a false positive is reviewed once; a false negative leaks.
func isSlogCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	switch sel.Sel.Name {
	case "Debug", "Info", "Warn", "Error", "With":
	default:
		return false
	}
	var b strings.Builder
	printer.Fprint(&b, token.NewFileSet(), sel.X)
	return strings.Contains(strings.ToLower(b.String()), "log")
}

func exprSource(fset *token.FileSet, e ast.Expr) string {
	var b strings.Builder
	printer.Fprint(&b, fset, e)
	return b.String()
}

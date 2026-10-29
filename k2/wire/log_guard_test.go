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
// storage. This guard scans EVERY slog call AND every fmt.Errorf/fmt.Sprintf/
// errors.New call in wire/ and server/ and fails when a credential-shaped
// expression appears as an argument. Log key_id, an 8-byte session_id prefix,
// and verdicts — never cookie/token/secret/jwt values. Precedent: server
// NodeSecret is already marked "never log".
//
// This is the CONSOLIDATED guard: it supersedes and subsumes two guards that
// used to scan the same two directories with independently-drifting coverage
// — the Phase 1 guard (wire/credential_log_guard_test.go, denylist
// token/nodeSecret/secretToken, sinks slog + Errorf/Sprintf/New) and this
// guard's own first version (Phase 2, denylist +cookie/+jwt/+secret, sinks
// slog only). Neither alone caught a cookie/jwt laundered through
// fmt.Errorf/fmt.Sprintf before reaching a log call. Keeping two guards in
// sync by hand is exactly the kind of drift this constraint must not depend
// on — one guard, union of both denylists, union of both sink sets.
//
// Static because this class of constraint rots under manual review: the next
// slog/Errorf/Sprintf line added months from now must fail CI, not wait for
// a reviewer.
//
// KNOWN BLIND SPOTS (spec §9), honestly recorded rather than papered over:
//   - This guard inspects the DIRECT arguments of slog calls and the DIRECT
//     arguments of fmt.Errorf/fmt.Sprintf/errors.New calls. It does NOT chase
//     a credential through TWO hops: a credential first copied into an
//     innocuously-named local (`s := meta.Token; ...; slog.Info(..., "v", s)`),
//     or passed through an intermediate helper function, is invisible to a
//     purely syntactic scan of the call site — `s`/the helper's return value
//     carries no credential-shaped name. Closing this fully needs a type with
//     a redacting String()/LogValue() so any accidental inclusion self-redacts
//     regardless of what the carrying variable is named — not in scope here.
//   - `forbiddenLogValue` matches on regexp word boundaries (`\b`), which do
//     NOT fire between two adjacent letters. Verified empirically: the regex
//     catches "NodeSecret"/"SecretToken"/"nodeSecret" (explicit alternation
//     terms below, matching Phase 1's exact-name coverage) but does NOT catch
//     arbitrary compound identifiers like "sessionToken" or "authCookie" —
//     there is no word boundary between "session"/"auth" and the credential
//     word. This blind spot pre-dates this guard (Phase 1's exact-string
//     denylist had the identical gap for any compound it didn't enumerate by
//     name) and is not fixable by adding more alternation terms without
//     enumerating every future compound name — same underlying fix as above.

// forbiddenLogValue matches identifiers that carry credential material.
// nodesecret/secrettoken are explicit terms (not just secret|token) because
// \b does not fire mid-identifier ("NodeSecret" has no boundary between
// "Node" and "Secret") — verified empirically, see blind-spot note above.
// This restores exact parity with Phase 1's denylist, which matched those two
// compounds by whole-identifier equality.
var forbiddenLogValue = regexp.MustCompile(`(?i)\b(token|cookie|secret|jwt|nodesecret|secrettoken)\b`)

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
				if !ok || !isLogOrErrorSink(call) {
					return true
				}
				for _, arg := range call.Args {
					if isConstStringExpr(arg) {
						continue // string keys / literal (incl. "a" + "b" concatenation) carry no runtime value
					}
					src := exprSource(fset, arg)
					if logValueAllowlist[src] {
						continue
					}
					if forbiddenLogValue.MatchString(src) {
						t.Errorf("%s: credential-shaped expression %q passed to a log/error sink — log key_id / session prefix / verdict instead (spec §6.2/§9)",
							fset.Position(arg.Pos()), src)
					}
				}
				return true
			})
		}
	}
}

// isLogOrErrorSink matches two families of call sites, mirroring the union of
// both guards this one consolidates:
//   - slog.<Level>/With and <someLogger>.<Level>/With where the receiver text
//     contains "log" (slog, connLog, log, logger, …) — direct log sinks.
//   - fmt.Errorf / fmt.Sprintf / errors.New — a credential interpolated here
//     lands in a value later logged under an innocent name (err, msg). Matched
//     by method name alone, without a receiver check, mirroring Phase 1's
//     credential_log_guard_test.go approach (the receiver is always a package
//     selector like `fmt`/`errors`, not a local — over-matching on the method
//     name alone is acceptable here for the same reason it's acceptable for
//     slog: a false positive is reviewed once, a false negative leaks).
func isLogOrErrorSink(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	switch sel.Sel.Name {
	case "Errorf", "Sprintf", "New":
		return true
	case "Debug", "Info", "Warn", "Error", "With":
		var b strings.Builder
		printer.Fprint(&b, token.NewFileSet(), sel.X)
		return strings.Contains(strings.ToLower(b.String()), "log")
	default:
		return false
	}
}

// isConstStringExpr reports whether e is a compile-time-constant string built
// purely from literals and "+" concatenation (error messages are routinely
// split across lines this way). Such an expression carries no runtime value:
// English prose in a literal that happens to contain the word "token" is not
// a credential leak the way an interpolated variable would be. This widens
// the original single-BasicLit skip, which missed concatenated literals —
// found live when the Errorf/Sprintf sink extension (this consolidation)
// flagged server.go's "...admits any non-empty udid/token" literal message.
func isConstStringExpr(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.BasicLit:
		return v.Kind == token.STRING
	case *ast.BinaryExpr:
		return v.Op == token.ADD && isConstStringExpr(v.X) && isConstStringExpr(v.Y)
	default:
		return false
	}
}

func exprSource(fset *token.FileSet, e ast.Expr) string {
	var b strings.Builder
	printer.Fprint(&b, fset, e)
	return b.String()
}

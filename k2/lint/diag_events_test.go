package lint

import (
	"bufio"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// diagEventsDoc is the DIAG event registry, relative to the k2 module root.
const diagEventsDoc = "docs/diag-events.md"

// diagLiteralRe extracts the event name from a Go string literal's value.
// DIAG events are emitted as the slog message, e.g. slog.Warn("DIAG: dns-fail", ...);
// the name is the kebab-case token after the prefix. Trailing text after the
// name (", skipping ..." in gateway/uci_linux.go, " — this node ..." in
// wire/alert.go's Slack text) is not part of the name.
var diagLiteralRe = regexp.MustCompile(`^DIAG: ([a-z0-9][a-z0-9-]*)`)

// diagDocRowRe matches a registry table row whose first cell is the bare
// event name in backticks: | `dns-fail` | WARN | ... |
var diagDocRowRe = regexp.MustCompile("^\\|\\s*`([a-z0-9][a-z0-9-]*)`\\s*\\|")

// TestDiagEvents_RegistryMatchesCode keeps docs/diag-events.md and the code in
// lockstep, in both directions:
//   - every "DIAG: <name>" string literal in a non-test .go file must have a
//     row in docs/diag-events.md (a new event nobody can look up is the
//     failure this prevents — the table had drifted to 72 of ~124 events);
//   - every row in docs/diag-events.md must name an event that still exists
//     in code (a removed event left in the table sends support grepping for
//     a line that can never appear).
//
// Only string literals are scanned (via go/ast), so comments that mention an
// event never count as an emission.
func TestDiagEvents_RegistryMatchesCode(t *testing.T) {
	root := findModuleRoot(t)

	inCode := collectDiagEventsFromCode(t, root) // name -> first location
	inDoc := collectDiagEventsFromDoc(t, filepath.Join(root, diagEventsDoc))

	if len(inCode) == 0 {
		t.Fatal("found 0 DIAG events in code — the extraction regex or walk is broken, not the registry")
	}

	var missingFromDoc, missingFromCode []string
	for name, loc := range inCode {
		if _, ok := inDoc[name]; !ok {
			missingFromDoc = append(missingFromDoc, name+" (emitted at "+loc+")")
		}
	}
	for name, line := range inDoc {
		if _, ok := inCode[name]; !ok {
			missingFromCode = append(missingFromCode, name+" ("+diagEventsDoc+":"+strconv.Itoa(line)+")")
		}
	}
	sort.Strings(missingFromDoc)
	sort.Strings(missingFromCode)

	if len(missingFromDoc) > 0 {
		t.Errorf("%d DIAG event(s) emitted in code but not registered in k2/%s.\n"+
			"Add a row like  | `name` | LEVEL | file | meaning / fields |  to the matching section of k2/%s:\n  %s",
			len(missingFromDoc), diagEventsDoc, diagEventsDoc, strings.Join(missingFromDoc, "\n  "))
	}
	if len(missingFromCode) > 0 {
		t.Errorf("%d DIAG event(s) registered in k2/%s but emitted nowhere in non-test code.\n"+
			"If the event was removed, delete its table row from k2/%s and list it under \"Removed events\" (a bullet, not a table row);\n"+
			"if it was renamed, fix the row's name:\n  %s",
			len(missingFromCode), diagEventsDoc, diagEventsDoc, strings.Join(missingFromCode, "\n  "))
	}
}

func collectDiagEventsFromCode(t *testing.T, root string) map[string]string {
	t.Helper()
	events := map[string]string{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			base := filepath.Base(path)
			if base == "vendor" || base == ".git" || base == "testdata" || base == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if perr != nil {
			// An unparseable file would hide its events; fail rather than skip.
			t.Errorf("parse %s: %v", path, perr)
			return nil
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			val, uerr := strconv.Unquote(lit.Value)
			if uerr != nil {
				return true
			}
			m := diagLiteralRe.FindStringSubmatch(val)
			if m == nil {
				return true
			}
			if _, seen := events[m[1]]; !seen {
				rel, _ := filepath.Rel(root, path)
				events[m[1]] = rel + ":" + strconv.Itoa(fset.Position(lit.Pos()).Line)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return events
}

func collectDiagEventsFromDoc(t *testing.T, path string) map[string]int {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open DIAG registry: %v (it must exist at k2/%s)", err, diagEventsDoc)
	}
	defer f.Close()

	events := map[string]int{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	line := 0
	for sc.Scan() {
		line++
		m := diagDocRowRe.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		if prev, dup := events[m[1]]; dup {
			t.Errorf("k2/%s:%d: event %q already registered at line %d — keep one row per event", diagEventsDoc, line, m[1], prev)
			continue
		}
		events[m[1]] = line
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("read DIAG registry: %v", err)
	}
	return events
}

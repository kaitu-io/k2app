package daemon

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestSlogMigration_NoOldLogCalls verifies that no old log.Printf/Println/Fatalf
// calls remain in the non-test daemon source files after the slog migration.
func TestSlogMigration_NoOldLogCalls(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	daemonDir := filepath.Dir(file)

	// Use grep --include to only check non-test .go files.
	out, err := exec.Command("grep", "-r",
		"--include=*.go",
		"--exclude=*_test.go",
		`log\.Printf\|log\.Println\|log\.Fatalf`,
		daemonDir).CombinedOutput()
	// grep exits non-zero (exit 1) when no match found — that's the success case.
	if err == nil {
		t.Errorf("old log calls still present in daemon/ source files:\n%s", string(out))
		return
	}
	// Only exit code 1 (no match) is expected.
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() != 1 {
		t.Errorf("grep error (exit %d): %s", exitErr.ExitCode(), string(out))
	}
}

// TestSlogMigration_NoOldLogImport verifies that no non-test file in the daemon
// package imports the bare "log" package (only "log/slog" is allowed).
func TestSlogMigration_NoOldLogImport(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	daemonDir := filepath.Dir(file)

	// Use grep -r to find non-test files with import "log" (bare log, not log/slog).
	out, err := exec.Command("grep", "-rn",
		"--include=*.go",
		"--exclude=*_test.go",
		`import "log"`,
		daemonDir).CombinedOutput()
	if err == nil {
		// grep found matches — failure.
		t.Errorf("files still importing bare \"log\" package:\n%s", string(out))
		return
	}
	// Only exit code 1 (no match) is acceptable.
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() != 1 {
		t.Errorf("grep error (exit %d): %s", exitErr.ExitCode(), string(out))
	}
	_ = strings.TrimSpace("") // keep strings import
}

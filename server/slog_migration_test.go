package server

// T5 RED: slog migration verification tests.
//
// These tests verify that server/ has been fully migrated from "log" to "log/slog".
// They FAIL before migration (log.Printf calls present) and PASS after migration.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoLegacyLogCalls verifies that server.go and handler.go contain no
// log.Printf / log.Println / log.Fatalf calls from the stdlib "log" package.
func TestNoLegacyLogCalls(t *testing.T) {
	files := []string{
		filepath.Join("..", "server", "server.go"),
		filepath.Join("..", "server", "handler.go"),
	}

	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		content := string(data)
		for _, pattern := range []string{"log.Printf", "log.Println", "log.Fatalf", "log.Fatal("} {
			if strings.Contains(content, pattern) {
				t.Errorf("%s still contains legacy log call %q — migrate to log/slog", f, pattern)
			}
		}
	}
}

// TestNoLegacyLogImport verifies that server.go and handler.go do not import "log"
// (only "log/slog" is allowed).
func TestNoLegacyLogImport(t *testing.T) {
	files := []string{
		filepath.Join("..", "server", "server.go"),
		filepath.Join("..", "server", "handler.go"),
	}

	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		content := string(data)
		// Check for bare "log" import (not "log/slog").
		// The pattern `"log"` with surrounding quotes and newline captures the import statement.
		if strings.Contains(content, `"log"`) {
			t.Errorf("%s still imports bare \"log\" package — should use \"log/slog\" only", f)
		}
	}
}

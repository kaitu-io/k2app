package engine

import (
	"os"
	"regexp"
	"testing"
)

// TestNoOldLogCalls verifies that engine.go uses log/slog instead of the
// legacy "log" package. This test is part of the T4 slog migration.
func TestNoOldLogCalls(t *testing.T) {
	src, err := os.ReadFile("engine.go")
	if err != nil {
		t.Fatalf("read engine.go: %v", err)
	}

	oldLog := regexp.MustCompile(`\blog\.Printf\b|\blog\.Println\b|\blog\.Fatalf\b|\blog\.Fatal\b`)
	if matches := oldLog.FindAll(src, -1); len(matches) > 0 {
		t.Errorf("engine.go still has %d old log call(s): use slog instead of log.Printf/Println/Fatalf", len(matches))
		for _, m := range matches {
			t.Logf("  found: %s", m)
		}
	}

	// Verify log/slog is imported (not the legacy "log" package)
	legacyImport := regexp.MustCompile(`"log"\n`)
	if legacyImport.Match(src) {
		t.Error(`engine.go imports "log" — must use "log/slog" only`)
	}
}

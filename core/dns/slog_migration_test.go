package dns

import (
	"os"
	"regexp"
	"testing"
)

// TestNoOldLogCalls verifies that all dns/*.go source files use log/slog
// instead of the legacy "log" package. This test is part of the T4 slog migration.
func TestNoOldLogCalls(t *testing.T) {
	files := []string{"direct.go", "middleware.go", "proxy.go"}

	oldLog := regexp.MustCompile(`\blog\.Printf\b|\blog\.Println\b|\blog\.Fatalf\b|\blog\.Fatal\b`)
	legacyImport := regexp.MustCompile(`"log"\n`)

	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile(name)
			if err != nil {
				t.Fatalf("read %s: %v", name, err)
			}

			if matches := oldLog.FindAll(src, -1); len(matches) > 0 {
				t.Errorf("%s still has %d old log call(s): use slog instead of log.Printf/Println/Fatalf", name, len(matches))
				for _, m := range matches {
					t.Logf("  found: %s", m)
				}
			}

			if legacyImport.Match(src) {
				t.Errorf(`%s imports "log" — must use "log/slog" only`, name)
			}
		})
	}
}

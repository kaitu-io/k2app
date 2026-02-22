package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestNoLegacyLogCalls verifies that no non-test Go files in cmd/k2/ use
// the bare "log" package calls (log.Printf, log.Println, log.Fatalf, etc.).
// This enforces the slog migration for the cmd/k2 package.
func TestNoLegacyLogCalls(t *testing.T) {
	dir := filepath.Join(".")

	// Pattern that matches legacy log package calls.
	legacyCallPattern := regexp.MustCompile(`\blog\.(Printf|Println|Fatalf|Fatal|Print|Panicf|Panic)\(`)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		// Only check production Go source files, skip test files.
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		content, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Errorf("read %s: %v", name, err)
			continue
		}

		if legacyCallPattern.Match(content) {
			t.Errorf("file %s contains legacy log package calls (log.Printf/Println/Fatalf etc.) — migrate to slog", name)
		}
	}
}

// TestNoBareLegacyLogImport verifies that no non-test Go files in cmd/k2/
// import the bare "log" standard library package.
func TestNoBareLegacyLogImport(t *testing.T) {
	dir := filepath.Join(".")

	// Matches: import "log" or "log" in import block (not log/slog).
	bareLogImport := regexp.MustCompile(`"log"`)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		// Skip test files — they may use log for captureStderr helpers during transition.
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		content, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Errorf("read %s: %v", name, err)
			continue
		}

		if bareLogImport.Match(content) {
			t.Errorf("file %s imports bare \"log\" package — use \"log/slog\" instead", name)
		}
	}
}

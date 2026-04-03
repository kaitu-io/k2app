package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captureStderr redirects os.Stderr to a buffer for the duration of f.
// Since the package now uses slog (which writes to os.Stderr by default),
// this captures slog output by redirecting the underlying file descriptor.
func captureStderr(f func()) string {
	var buf bytes.Buffer
	old := os.Stderr

	r, w, _ := os.Pipe()
	os.Stderr = w

	f()

	w.Close()
	os.Stderr = old

	buf.ReadFrom(r)
	return buf.String()
}

// TestCmdTestConfig_Valid verifies that -t with a valid config file prints success
// and does NOT exit with a fatal error.
func TestCmdTestConfig_Valid(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "k2.yaml")

	// Write a valid config
	validYAML := `server: "k2v5://user:token@example.com:443"
mode: tun
`
	if err := os.WriteFile(cfgPath, []byte(validYAML), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// Should not panic or os.Exit — capture output
	var out bytes.Buffer
	err := runTestConfig(cfgPath, &out)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "OK") && !strings.Contains(got, "valid") {
		t.Errorf("expected success output, got: %q", got)
	}
}

// TestCmdTestConfig_Invalid verifies that -t with bad YAML returns an error.
func TestCmdTestConfig_Invalid(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "bad.yaml")

	// Write invalid YAML (genuine parse error)
	badYAML := "key: [unclosed bracket\n"
	if err := os.WriteFile(cfgPath, []byte(badYAML), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var out bytes.Buffer
	err := runTestConfig(cfgPath, &out)
	if err == nil {
		t.Fatal("expected error for bad YAML, got nil")
	}
}

// TestCmdTestConfig_Missing verifies that -t with a missing file returns an error.
func TestCmdTestConfig_Missing(t *testing.T) {
	var out bytes.Buffer
	err := runTestConfig("/nonexistent/path/k2.yaml", &out)
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

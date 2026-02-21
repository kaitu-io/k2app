package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestK2sTestConfig_Valid verifies that runTestConfig with a valid config file
// prints success and returns nil error.
func TestK2sTestConfig_Valid(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "server.yaml")

	// Write a valid server config
	validYAML := `listen: ":9443"
`
	if err := os.WriteFile(cfgPath, []byte(validYAML), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

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

// TestK2sTestConfig_Invalid verifies that runTestConfig with bad YAML returns an error.
func TestK2sTestConfig_Invalid(t *testing.T) {
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

// TestK2sSignalStop_NoPID verifies runSignal prints an error when PID file is absent.
func TestK2sSignalStop_NoPID(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "nope.pid")

	var out bytes.Buffer
	err := runSignal("stop", pidPath, &out)
	if err == nil {
		t.Fatal("expected error when PID file does not exist")
	}

	msg := err.Error()
	if !strings.Contains(strings.ToLower(msg), "pid") && !strings.Contains(strings.ToLower(msg), "not running") {
		t.Errorf("expected pid-related error, got: %q", msg)
	}
}

// TestK2sDeprecatedRunAlias verifies that deprecatedWarn prints a deprecation warning.
func TestK2sDeprecatedRunAlias(t *testing.T) {
	var buf bytes.Buffer
	deprecatedWarn("run", "k2s (no subcommand)", &buf)
	out := buf.String()
	if !strings.Contains(strings.ToLower(out), "deprecated") && !strings.Contains(strings.ToLower(out), "warning") {
		t.Errorf("expected deprecation warning, got: %q", out)
	}
	if !strings.Contains(out, "run") {
		t.Errorf("expected 'run' in warning message, got: %q", out)
	}
}

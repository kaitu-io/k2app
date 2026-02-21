package main

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captureStderr redirects log output and stderr to a buffer for the duration of f.
func captureStderr(f func()) string {
	var buf bytes.Buffer
	old := os.Stderr

	r, w, _ := os.Pipe()
	os.Stderr = w

	log.SetOutput(w)
	oldFlags := log.Flags()
	log.SetFlags(0)

	f()

	w.Close()
	os.Stderr = old
	log.SetOutput(os.Stderr)
	log.SetFlags(oldFlags)

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

// TestSignalStop_NoPID verifies cmdSignal prints an error when PID file is absent.
func TestSignalStop_NoPID(t *testing.T) {
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

// TestSignalStop_StalePID verifies cmdSignal handles a stale PID (dead process).
func TestSignalStop_StalePID(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "stale.pid")

	// Write a PID that cannot possibly be alive (very large number)
	// pid 9999999 is almost certainly not running
	if err := os.WriteFile(pidPath, []byte("9999999\n"), 0644); err != nil {
		t.Fatalf("write pid: %v", err)
	}

	var out bytes.Buffer
	err := runSignal("stop", pidPath, &out)
	if err == nil {
		t.Fatal("expected error for stale PID")
	}

	// PID file should be cleaned up by CheckStale
	if _, statErr := os.Stat(pidPath); statErr == nil {
		t.Error("expected stale PID file to be removed")
	}
}

// TestDeprecatedRunAlias verifies that "run" command prints a deprecation warning.
func TestDeprecatedRunAlias(t *testing.T) {
	var buf bytes.Buffer
	deprecatedWarn("run", "k2 (no subcommand)", &buf)
	out := buf.String()
	if !strings.Contains(strings.ToLower(out), "deprecated") && !strings.Contains(strings.ToLower(out), "warning") {
		t.Errorf("expected deprecation warning, got: %q", out)
	}
	if !strings.Contains(out, "run") {
		t.Errorf("expected 'run' in warning message, got: %q", out)
	}
}

// TestDeprecatedUpAlias verifies that "up" command prints a deprecation warning.
func TestDeprecatedUpAlias(t *testing.T) {
	var buf bytes.Buffer
	deprecatedWarn("up", "k2 ctl up", &buf)
	out := buf.String()
	if !strings.Contains(strings.ToLower(out), "deprecated") && !strings.Contains(strings.ToLower(out), "warning") {
		t.Errorf("expected deprecation warning, got: %q", out)
	}
	if !strings.Contains(out, "up") {
		t.Errorf("expected 'up' in warning message, got: %q", out)
	}
}

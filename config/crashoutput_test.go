package config

import (
	"os"
	"path/filepath"
	"runtime/debug"
	"testing"
)

// TestSetupCrashOutput_CreatesFileAndRegisters verifies that SetupCrashOutput
// creates k2-crash.log in the given dir, is safely repeatable (SetCrashOutput
// override semantics), and treats an empty dir as a no-op.
func TestSetupCrashOutput_CreatesFileAndRegisters(t *testing.T) {
	// Reset process-global crash output when done so the temp file (removed by
	// t.TempDir cleanup) is not left registered for the rest of the test binary.
	t.Cleanup(func() { _ = debug.SetCrashOutput(nil, debug.CrashOptions{}) })

	dir := t.TempDir()
	if err := SetupCrashOutput(dir); err != nil {
		t.Fatalf("SetupCrashOutput(%q) error: %v", dir, err)
	}

	path := filepath.Join(dir, "k2-crash.log")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}

	// Second call must not error (SetCrashOutput overrides the previous one).
	if err := SetupCrashOutput(dir); err != nil {
		t.Fatalf("second SetupCrashOutput error: %v", err)
	}
}

// TestSetupCrashOutput_EmptyDirIsNoOp verifies an empty logDir is a silent no-op.
func TestSetupCrashOutput_EmptyDirIsNoOp(t *testing.T) {
	if err := SetupCrashOutput(""); err != nil {
		t.Fatalf("SetupCrashOutput(\"\") should be a no-op, got: %v", err)
	}
}

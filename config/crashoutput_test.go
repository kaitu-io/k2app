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
	dir := t.TempDir()
	// Reset process-global crash output when done. Registered AFTER t.TempDir so
	// it runs BEFORE TempDir's RemoveAll (t.Cleanup is LIFO): the runtime holds a
	// dup'd handle to k2-crash.log until SetCrashOutput(nil), and Windows refuses
	// to delete a file that's still open — so the reset must precede the removal.
	t.Cleanup(func() { _ = debug.SetCrashOutput(nil, debug.CrashOptions{}) })
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

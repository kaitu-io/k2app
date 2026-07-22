//go:build linux

package gateway

import (
	"os"
	"testing"
)

func withTempStateDir(t *testing.T) {
	t.Helper()
	old := stateDirOverride
	stateDirOverride = t.TempDir()
	t.Cleanup(func() { stateDirOverride = old })
}

func TestControlKeyHashRoundtrip(t *testing.T) {
	withTempStateDir(t)

	if got := loadControlKeyHash(); got != "" {
		t.Fatalf("empty dir: load = %q, want empty", got)
	}
	if err := saveControlKeyHash("deadbeef"); err != nil {
		t.Fatalf("save: %v", err)
	}
	if got := loadControlKeyHash(); got != "deadbeef" {
		t.Fatalf("load = %q, want deadbeef", got)
	}
	if err := clearControlKeyHash(); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if got := loadControlKeyHash(); got != "" {
		t.Fatalf("after clear: load = %q, want empty", got)
	}
	// clear is idempotent
	if err := clearControlKeyHash(); err != nil {
		t.Fatalf("second clear: %v", err)
	}
}

func TestControlKeyHashCorruptFile(t *testing.T) {
	withTempStateDir(t)

	// write garbage to file
	if err := os.MkdirAll(stateDir(), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(controlKeyPath(), []byte("{not json"), 0600); err != nil {
		t.Fatalf("write garbage: %v", err)
	}
	// corrupt file should return "" (not panic or error)
	if got := loadControlKeyHash(); got != "" {
		t.Fatalf("corrupt file: load = %q, want empty", got)
	}
}

func TestHashControlKey(t *testing.T) {
	// sha256("rck_test") hardened to prevent implementation drift
	const want = "3dc4d7deda72501f14ffa0094b91af91ea1ff112667eb3712a4f9fbd5f022e97"
	got := hashControlKey("rck_test")
	if got != want {
		t.Fatalf("hash = %q, want %q", got, want)
	}
	if len(got) != 64 {
		t.Fatalf("hash length = %d, want 64 hex chars", len(got))
	}
	if got == hashControlKey("rck_other") {
		t.Fatal("different inputs must hash differently")
	}
}

//go:build windows

package wintun

import (
	"os"
	"path/filepath"
	"testing"
)

// TestEmbeddedDLLsNotEmpty verifies that both embedded DLL blobs are non-empty.
// A zero-length embed means the go:embed directive failed or the source DLL
// file was missing at build time.
func TestEmbeddedDLLsNotEmpty(t *testing.T) {
	if len(dllAmd64) == 0 {
		t.Fatal("dllAmd64 is empty — go:embed wintun_amd64.dll failed")
	}
	t.Logf("dllAmd64 size: %d bytes", len(dllAmd64))

	if len(dllArm64) == 0 {
		t.Fatal("dllArm64 is empty — go:embed wintun_arm64.dll failed")
	}
	t.Logf("dllArm64 size: %d bytes", len(dllArm64))
}

// TestEnsureExtracted_WritesDLL calls EnsureExtracted and verifies that
// wintun.dll exists at the returned path and is non-empty.
func TestEnsureExtracted_WritesDLL(t *testing.T) {
	dir, err := EnsureExtracted()
	if err != nil {
		t.Fatalf("EnsureExtracted() error: %v", err)
	}
	if dir == "" {
		t.Fatal("EnsureExtracted() returned empty path")
	}

	dllFile := filepath.Join(dir, "wintun.dll")
	info, err := os.Stat(dllFile)
	if err != nil {
		t.Fatalf("wintun.dll stat error: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("wintun.dll is empty")
	}
	t.Logf("wintun.dll at %s (%d bytes)", dllFile, info.Size())
}

// TestIsNativeARM64_NoPanic calls isNativeARM64() and logs the result.
// The function uses IsWow64Process2 which may not exist on older Windows
// versions — it must degrade gracefully without panic.
func TestIsNativeARM64_NoPanic(t *testing.T) {
	result := isNativeARM64()
	t.Logf("isNativeARM64() = %v", result)
}

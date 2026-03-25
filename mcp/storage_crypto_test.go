package main

import (
	"encoding/hex"
	"fmt"
	"runtime"
	"strings"
	"testing"
)

func TestDeriveKeyMatchesRust(t *testing.T) {
	key := deriveKey("FC891097-D4C1-3B7A-8611-0F5C8ED3A23B")
	got := hex.EncodeToString(key[:])
	want := "49dd3fdef9a830c1733b5ab444031718f0229189e90b2049145b5c08674bc9ea"
	if got != want {
		t.Errorf("HKDF key mismatch:\n  got:  %s\n  want: %s", got, want)
	}
}

func TestDecryptMatchesRust(t *testing.T) {
	key := deriveKey("FC891097-D4C1-3B7A-8611-0F5C8ED3A23B")
	encrypted := "ENC1:AAECAwQFBgcICQoLMRflcKFmeDJTJkbxbBBhGBa6ZFerb1DUGGcCLTKfm2bQ"
	plaintext, err := decryptValue(encrypted, key)
	if err != nil {
		t.Fatalf("decrypt failed: %v", err)
	}
	if plaintext != "\"hello-from-rust\"" {
		t.Errorf("plaintext mismatch: got %q, want %q", plaintext, "\"hello-from-rust\"")
	}
}

func TestIsEncrypted(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"ENC1:abc123", true},
		{"ENC1:", true},
		{"enc1:abc", false},
		{"plaintext", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isEncrypted(tt.input); got != tt.want {
			t.Errorf("isEncrypted(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestDecryptBadPrefix(t *testing.T) {
	var key [32]byte
	_, err := decryptValue("not-encrypted", key)
	if err == nil {
		t.Error("expected error for non-encrypted value")
	}
}

func TestDecryptBadBase64(t *testing.T) {
	var key [32]byte
	_, err := decryptValue("ENC1:not-valid-base64!!!", key)
	if err == nil {
		t.Error("expected error for bad base64")
	}
}

func TestDecryptTooShort(t *testing.T) {
	var key [32]byte
	// base64 of 10 bytes (less than 12+16 minimum)
	_, err := decryptValue("ENC1:AAAAAAAAAAAAAA==", key)
	if err == nil {
		t.Error("expected error for too-short ciphertext")
	}
}

func TestHardwareIDNotEmpty(t *testing.T) {
	id, err := getHardwareID()
	if err != nil {
		t.Fatalf("getHardwareID() failed: %v", err)
	}
	if id == "" {
		t.Fatal("hardware ID is empty")
	}
	t.Logf("hardware ID: %s", id)
	// CI parses this line to compare with Rust output
	fmt.Printf("CROSS_LANG_GATE_HWID=%s\n", id)
}

func TestHardwareIDFormat(t *testing.T) {
	id, err := getHardwareID()
	if err != nil {
		t.Skipf("getHardwareID() failed: %v", err)
	}

	switch runtime.GOOS {
	case "darwin":
		// macOS: UUID format — 36 chars, 4 dashes (e.g. FC891097-D4C1-3B7A-8611-0F5C8ED3A23B)
		if len(id) != 36 {
			t.Errorf("macOS UUID length = %d, want 36", len(id))
		}
		dashes := strings.Count(id, "-")
		if dashes != 4 {
			t.Errorf("macOS UUID dashes = %d, want 4", dashes)
		}

	case "windows":
		// Windows: must not be all-F sentinel
		cleaned := strings.ReplaceAll(strings.ReplaceAll(strings.ToUpper(id), "F", ""), "-", "")
		if cleaned == "" {
			t.Error("Windows UUID is all-F sentinel")
		}

	case "linux":
		// Linux: 32 hex chars from /etc/machine-id
		if len(id) != 32 {
			t.Errorf("Linux machine-id length = %d, want 32", len(id))
		}
		if _, err := hex.DecodeString(id); err != nil {
			t.Errorf("Linux machine-id is not valid hex: %v", err)
		}
	}
}

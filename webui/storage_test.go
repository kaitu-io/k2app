//go:build linux

package webui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDeriveKeyDeterministic(t *testing.T) {
	k1 := deriveKey("test")
	k2 := deriveKey("test")
	if k1 != k2 {
		t.Error("deriveKey: same input produced different keys")
	}
}

func TestDeriveKeyDifferentInputs(t *testing.T) {
	k1 := deriveKey("test")
	k2 := deriveKey("other")
	if k1 == k2 {
		t.Error("deriveKey: different inputs produced the same key")
	}
}

func TestEncryptDecryptRoundtrip(t *testing.T) {
	key := deriveKey("test")
	plaintext := "hello, webui storage!"

	encrypted := encryptValue(plaintext, key)
	if !isEncrypted(encrypted) {
		t.Fatalf("encryptValue: result missing ENC1: prefix: %q", encrypted)
	}

	got, err := decryptValue(encrypted, key)
	if err != nil {
		t.Fatalf("decryptValue: %v", err)
	}
	if got != plaintext {
		t.Errorf("roundtrip: got %q, want %q", got, plaintext)
	}
}

func TestDecryptWrongKeyFails(t *testing.T) {
	key := deriveKey("test")
	wrongKey := deriveKey("wrong")

	encrypted := encryptValue("secret", key)
	_, err := decryptValue(encrypted, wrongKey)
	if err == nil {
		t.Error("decryptValue: expected error with wrong key, got nil")
	}
}

func TestStorageCRUD(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "storage.json")
	s := NewStorageWithKey(path, "test")

	// Set + Get
	if err := s.Set("foo", "bar"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	val, err := s.Get("foo")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if val != "bar" {
		t.Errorf("Get: got %q, want %q", val, "bar")
	}

	// Has
	if !s.Has("foo") {
		t.Error("Has(foo) = false, want true")
	}
	if s.Has("nonexistent") {
		t.Error("Has(nonexistent) = true, want false")
	}

	// Keys
	if err := s.Set("baz", "qux"); err != nil {
		t.Fatalf("Set baz: %v", err)
	}
	keys := s.Keys()
	if len(keys) != 2 {
		t.Errorf("Keys: got %v, want 2 entries", keys)
	}

	// Remove
	s.Remove("foo")
	if s.Has("foo") {
		t.Error("Has(foo) after Remove = true, want false")
	}
	if len(s.Keys()) != 1 {
		t.Errorf("Keys after Remove: got %d, want 1", len(s.Keys()))
	}

	// Clear
	s.Clear()
	if len(s.Keys()) != 0 {
		t.Errorf("Keys after Clear: got %d, want 0", len(s.Keys()))
	}
}

func TestStoragePersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "storage.json")

	s1 := NewStorageWithKey(path, "test")
	if err := s1.Set("token", "abc123"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// New instance reads the same file.
	s2 := NewStorageWithKey(path, "test")
	val, err := s2.Get("token")
	if err != nil {
		t.Fatalf("Get after reload: %v", err)
	}
	if val != "abc123" {
		t.Errorf("Get after reload: got %q, want %q", val, "abc123")
	}
}

func TestStoragePlaintextBackwardCompat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "storage.json")

	// Write a plaintext storage file (no ENC1: prefix) directly to the
	// on-disk format, then verify a fresh Storage reads it as plaintext.
	sf := storageFile{Items: map[string]string{
		"plainkey": "plainvalue",
	}}
	data, _ := json.MarshalIndent(sf, "", "  ")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	s := NewStorageWithKey(path, "test")
	val, err := s.Get("plainkey")
	if err != nil {
		t.Fatalf("Get plaintext value: %v", err)
	}
	if val != "plainvalue" {
		t.Errorf("Get plaintext: got %q, want %q", val, "plainvalue")
	}
}

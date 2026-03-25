package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadTauriStoragePlaintext(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "storage.json")

	content := `{
		"k2.auth.token": "\"my-access-token\"",
		"k2.auth.refresh": "\"my-refresh-token\"",
		"device-udid": "\"04cacd29-e71a-4884-842d-a2a5892d4db9\""
	}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	session, err := readTauriStorage(path, nil)
	if err != nil {
		t.Fatalf("readTauriStorage failed: %v", err)
	}

	if session.AccessToken != "my-access-token" {
		t.Errorf("AccessToken = %q, want %q", session.AccessToken, "my-access-token")
	}
	if session.RefreshToken != "my-refresh-token" {
		t.Errorf("RefreshToken = %q, want %q", session.RefreshToken, "my-refresh-token")
	}
	if session.RawUDID != "04cacd29-e71a-4884-842d-a2a5892d4db9" {
		t.Errorf("RawUDID = %q, want %q", session.RawUDID, "04cacd29-e71a-4884-842d-a2a5892d4db9")
	}
}

func TestUDIDHashMatchesWebapp(t *testing.T) {
	got := hashUDID("04cacd29-e71a-4884-842d-a2a5892d4db9")
	want := "932a7cc1a75b5830a1dd59f057b608d3"
	if got != want {
		t.Errorf("hashUDID mismatch:\n  got:  %s\n  want: %s", got, want)
	}
}

func TestReadTauriStorageMixed(t *testing.T) {
	// Use test vector key
	key := deriveKey("FC891097-D4C1-3B7A-8611-0F5C8ED3A23B")

	dir := t.TempDir()
	path := filepath.Join(dir, "storage.json")

	// Mix of encrypted (from Rust test vector) and plaintext values
	content := `{
		"k2.auth.token": "ENC1:AAECAwQFBgcICQoLMRflcKFmeDJTJkbxbBBhGBa6ZFerb1DUGGcCLTKfm2bQ",
		"k2.auth.refresh": "\"my-refresh-token\"",
		"device-udid": "\"04cacd29-e71a-4884-842d-a2a5892d4db9\""
	}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	session, err := readTauriStorage(path, &key)
	if err != nil {
		t.Fatalf("readTauriStorage failed: %v", err)
	}

	// Encrypted value decrypts to "hello-from-rust" (after stripping JSON quotes)
	if session.AccessToken != "hello-from-rust" {
		t.Errorf("AccessToken = %q, want %q", session.AccessToken, "hello-from-rust")
	}
	if session.RefreshToken != "my-refresh-token" {
		t.Errorf("RefreshToken = %q, want %q", session.RefreshToken, "my-refresh-token")
	}
	if session.HashedUDID != "932a7cc1a75b5830a1dd59f057b608d3" {
		t.Errorf("HashedUDID = %q, want %q", session.HashedUDID, "932a7cc1a75b5830a1dd59f057b608d3")
	}
}

func TestReadTauriStorageEncryptedSkippedWithoutKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "storage.json")

	content := `{
		"k2.auth.token": "ENC1:AAECAwQFBgcICQoLMRflcKFmeDJTJkbxbBBhGBa6ZFerb1DUGGcCLTKfm2bQ",
		"device-udid": "\"my-udid\""
	}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	session, err := readTauriStorage(path, nil)
	if err != nil {
		t.Fatalf("readTauriStorage failed: %v", err)
	}

	// Encrypted token should be empty (skipped)
	if session.AccessToken != "" {
		t.Errorf("AccessToken should be empty when key is nil, got %q", session.AccessToken)
	}
	// Plaintext values still work
	if session.RawUDID != "my-udid" {
		t.Errorf("RawUDID = %q, want %q", session.RawUDID, "my-udid")
	}
}

func TestReadTauriStorageMissing(t *testing.T) {
	_, err := readTauriStorage("/nonexistent/path/storage.json", nil)
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

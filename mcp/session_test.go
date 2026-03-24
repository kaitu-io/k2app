package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSession_SaveAndRestore(t *testing.T) {
	dir := t.TempDir()
	s := NewSession(dir)

	issuedAt := time.Now().Truncate(time.Second)
	s.SetTokens("access-abc", "refresh-xyz", "user@example.com", issuedAt)

	if err := s.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Restore into a fresh session
	s2 := NewSession(dir)
	if err := s2.Restore(); err != nil {
		t.Fatalf("Restore failed: %v", err)
	}

	if s2.AccessToken != "access-abc" {
		t.Errorf("expected AccessToken 'access-abc', got '%s'", s2.AccessToken)
	}
	if s2.RefreshToken != "refresh-xyz" {
		t.Errorf("expected RefreshToken 'refresh-xyz', got '%s'", s2.RefreshToken)
	}
	if s2.Email != "user@example.com" {
		t.Errorf("expected Email 'user@example.com', got '%s'", s2.Email)
	}
	if !s2.IssuedAt.Equal(issuedAt) {
		t.Errorf("expected IssuedAt %v, got %v", issuedAt, s2.IssuedAt)
	}
}

func TestSession_RestoreNonExistent(t *testing.T) {
	dir := t.TempDir()
	s := NewSession(dir)
	// should not return an error if file doesn't exist
	if err := s.Restore(); err != nil {
		t.Fatalf("unexpected error on non-existent file: %v", err)
	}
}

func TestSession_Clear(t *testing.T) {
	dir := t.TempDir()
	s := NewSession(dir)
	s.SetTokens("access-abc", "refresh-xyz", "user@example.com", time.Now())

	if err := s.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	if err := s.Clear(); err != nil {
		t.Fatalf("Clear failed: %v", err)
	}

	if s.AccessToken != "" {
		t.Error("expected AccessToken to be empty after Clear")
	}
	if s.RefreshToken != "" {
		t.Error("expected RefreshToken to be empty after Clear")
	}
	if s.LoggedIn() {
		t.Error("expected LoggedIn() to be false after Clear")
	}

	// file should not exist
	sessionPath := filepath.Join(dir, "mcp-session.json")
	if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
		t.Error("expected session file to be removed after Clear")
	}
}

func TestSession_UDID_Generated(t *testing.T) {
	dir := t.TempDir()
	s := NewSession(dir)

	udid1 := s.UDID()
	if len(udid1) != 16 {
		t.Errorf("expected UDID length 16, got %d: %q", len(udid1), udid1)
	}
	// should be hex characters only
	for _, c := range udid1 {
		if !strings.ContainsRune("0123456789abcdef", c) {
			t.Errorf("UDID contains non-hex character: %q in %q", c, udid1)
			break
		}
	}

	// second call should return the same value
	udid2 := s.UDID()
	if udid1 != udid2 {
		t.Errorf("UDID not stable: first=%q, second=%q", udid1, udid2)
	}
}

func TestSession_UDID_ReadsFromFile(t *testing.T) {
	dir := t.TempDir()
	udidPath := filepath.Join(dir, "mcp-udid")
	existing := "abcdef0123456789"
	if err := os.WriteFile(udidPath, []byte(existing), 0600); err != nil {
		t.Fatalf("failed to write udid file: %v", err)
	}

	s := &Session{dir: dir}
	udid := s.UDID()
	if udid != existing {
		t.Errorf("expected UDID %q from file, got %q", existing, udid)
	}
}

func TestSession_FilePermissions(t *testing.T) {
	dir := t.TempDir()
	s := NewSession(dir)
	s.SetTokens("tok", "ref", "e@e.com", time.Now())

	if err := s.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	sessionPath := filepath.Join(dir, "mcp-session.json")
	info, err := os.Stat(sessionPath)
	if err != nil {
		t.Fatalf("stat session file: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("expected file permissions 0600, got %o", info.Mode().Perm())
	}
}

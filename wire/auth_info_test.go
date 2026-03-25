package wire

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestEnsureAuth_GeneratesNew(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")

	auth, err := EnsureAuth(path)
	if err != nil {
		t.Fatalf("EnsureAuth: %v", err)
	}

	// UDID should be 8 hex chars
	if len(auth.UDID) != 8 {
		t.Errorf("UDID length = %d, want 8", len(auth.UDID))
	}
	for _, c := range auth.UDID {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("UDID contains non-hex char %q", string(c))
		}
	}

	// Token should be 32 hex chars
	if len(auth.Token) != 32 {
		t.Errorf("Token length = %d, want 32", len(auth.Token))
	}
	for _, c := range auth.Token {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("Token contains non-hex char %q", string(c))
		}
	}

	// File should exist and be valid JSON
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("auth file not written: %v", err)
	}
	var loaded AuthInfo
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("auth file invalid JSON: %v", err)
	}
	if loaded.UDID != auth.UDID {
		t.Errorf("file User = %q, want %q", loaded.UDID, auth.UDID)
	}
	if loaded.Token != auth.Token {
		t.Errorf("file Pass = %q, want %q", loaded.Token, auth.Token)
	}
}

func TestEnsureAuth_LoadsExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")

	// First call generates
	auth1, err := EnsureAuth(path)
	if err != nil {
		t.Fatalf("first EnsureAuth: %v", err)
	}

	// Second call loads same values
	auth2, err := EnsureAuth(path)
	if err != nil {
		t.Fatalf("second EnsureAuth: %v", err)
	}

	if auth1.UDID != auth2.UDID {
		t.Errorf("UDID changed: %q vs %q", auth1.UDID, auth2.UDID)
	}
	if auth1.Token != auth2.Token {
		t.Errorf("Token changed: %q vs %q", auth1.Token, auth2.Token)
	}
}

func TestEnsureAuth_FilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permissions not applicable on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")

	_, err := EnsureAuth(path)
	if err != nil {
		t.Fatalf("EnsureAuth: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("permissions = %o, want 0600", perm)
	}
}

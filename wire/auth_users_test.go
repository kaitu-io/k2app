package wire

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadUsersFile_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users")
	os.WriteFile(path, []byte("# comment\na1b2:token1\n\ne5f6:token2\n"), 0600)

	users, err := LoadUsersFile(path)
	if err != nil {
		t.Fatalf("LoadUsersFile: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("expected 2 users, got %d", len(users))
	}
	if users["a1b2"] != "token1" {
		t.Error("user a1b2 token mismatch")
	}
	if users["e5f6"] != "token2" {
		t.Error("user e5f6 token mismatch")
	}
}

func TestLoadUsersFile_Empty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users")
	os.WriteFile(path, []byte("# only comments\n\n"), 0600)

	users, err := LoadUsersFile(path)
	if err != nil {
		t.Fatalf("LoadUsersFile: %v", err)
	}
	if len(users) != 0 {
		t.Errorf("expected 0 users, got %d", len(users))
	}
}

func TestLoadUsersFile_Malformed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users")
	os.WriteFile(path, []byte("valid1:tok1\nno-colon\n:empty-udid\nempty-token:\nvalid2:tok2\n"), 0600)

	users, err := LoadUsersFile(path)
	if err != nil {
		t.Fatalf("LoadUsersFile: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("expected 2 valid users, got %d", len(users))
	}
}

func TestLoadUsersFile_NotFound(t *testing.T) {
	_, err := LoadUsersFile("/nonexistent/users")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestNewUsersFileValidator_Match(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users")
	os.WriteFile(path, []byte("testudid:testtoken\n"), 0600)

	v := NewUsersFileValidator(path)
	if !v("testtoken", "testudid") {
		t.Error("expected valid credentials to pass")
	}
}

func TestNewUsersFileValidator_WrongToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users")
	os.WriteFile(path, []byte("testudid:testtoken\n"), 0600)

	v := NewUsersFileValidator(path)
	if v("wrongtoken", "testudid") {
		t.Error("expected wrong token to fail")
	}
}

func TestNewUsersFileValidator_UnknownUDID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users")
	os.WriteFile(path, []byte("testudid:testtoken\n"), 0600)

	v := NewUsersFileValidator(path)
	if v("testtoken", "unknown") {
		t.Error("expected unknown UDID to fail")
	}
}

func TestNewUsersFileValidator_HotReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users")
	os.WriteFile(path, []byte("user1:token1\n"), 0600)

	v := NewUsersFileValidator(path)

	if !v("token1", "user1") {
		t.Error("user1 should be valid")
	}
	if v("token2", "user2") {
		t.Error("user2 should be invalid")
	}

	// Update file: remove user1, add user2.
	os.WriteFile(path, []byte("user2:token2\n"), 0600)

	if v("token1", "user1") {
		t.Error("user1 should be invalid after update")
	}
	if !v("token2", "user2") {
		t.Error("user2 should be valid after update")
	}
}

func TestNewUsersFileValidator_FileMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users")
	os.WriteFile(path, []byte("testudid:testtoken\n"), 0600)

	v := NewUsersFileValidator(path)
	if !v("testtoken", "testudid") {
		t.Error("should pass when file exists")
	}

	os.Remove(path)
	if v("testtoken", "testudid") {
		t.Error("should fail when file removed")
	}
}

func TestNewUsersFileValidator_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users")
	os.WriteFile(path, []byte(""), 0600)

	v := NewUsersFileValidator(path)
	if v("anytoken", "anyudid") {
		t.Error("empty file should reject all")
	}
}

func TestChainValidators(t *testing.T) {
	reject := func(jwt, udid string) bool { return false }
	accept := func(jwt, udid string) bool { return true }

	// Both reject → false.
	if ChainValidators(reject, reject)("a", "b") {
		t.Error("both reject should fail")
	}
	// First accepts → true (short-circuit).
	if !ChainValidators(accept, reject)("a", "b") {
		t.Error("first accept should pass")
	}
	// Second accepts → true.
	if !ChainValidators(reject, accept)("a", "b") {
		t.Error("second accept should pass")
	}
}

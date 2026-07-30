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
	// v(token, udid, mode)
	if v("testtoken", "testudid", "").OK != true {
		t.Error("expected valid credentials to pass")
	}
}

func TestNewUsersFileValidator_WrongToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users")
	os.WriteFile(path, []byte("testudid:testtoken\n"), 0600)

	v := NewUsersFileValidator(path)
	got := v("wrongtoken", "testudid", "")
	if got.OK != false || got.Code != 0 {
		t.Errorf("expected no-opinion for wrong token, got %+v", got)
	}
}

func TestNewUsersFileValidator_UnknownUDID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users")
	os.WriteFile(path, []byte("testudid:testtoken\n"), 0600)

	v := NewUsersFileValidator(path)
	got := v("testtoken", "unknown", "")
	if got.OK != false || got.Code != 0 {
		t.Errorf("expected no-opinion for unknown udid, got %+v", got)
	}
}

func TestNewUsersFileValidator_HotReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users")
	os.WriteFile(path, []byte("user1:token1\n"), 0600)

	v := NewUsersFileValidator(path)

	// v(token, udid, mode)
	if !v("token1", "user1", "").OK {
		t.Error("user1 should be valid")
	}
	if v("token2", "user2", "").OK {
		t.Error("user2 should be invalid")
	}

	// Update file: remove user1, add user2.
	os.WriteFile(path, []byte("user2:token2\n"), 0600)

	if v("token1", "user1", "").OK {
		t.Error("user1 should be invalid after update")
	}
	if !v("token2", "user2", "").OK {
		t.Error("user2 should be valid after update")
	}
}

func TestNewUsersFileValidator_FileMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users")
	os.WriteFile(path, []byte("testudid:testtoken\n"), 0600)

	v := NewUsersFileValidator(path)
	if !v("testtoken", "testudid", "").OK {
		t.Error("should pass when file exists")
	}

	os.Remove(path)
	got := v("testtoken", "testudid", "")
	if got.OK != false || got.Code != 0 {
		t.Errorf("expected no-opinion when file removed, got %+v", got)
	}
}

func TestNewUsersFileValidator_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users")
	os.WriteFile(path, []byte(""), 0600)

	v := NewUsersFileValidator(path)
	got := v("anytoken", "anyudid", "")
	if got.OK != false || got.Code != 0 {
		t.Errorf("expected no-opinion for empty file, got %+v", got)
	}
}

// TestNewUsersFileValidator_NeverIssuesExplicitRejection pins the no-opinion
// rule: whatever goes wrong locally (missing file, empty file, unknown udid,
// wrong token), the users-file validator must not preempt Center's verdict.
func TestNewUsersFileValidator_NeverIssuesExplicitRejection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users")
	os.WriteFile(path, []byte("alice:token1\n"), 0600)
	v := NewUsersFileValidator(path)
	for name, args := range map[string][2]string{
		"wrong token":  {"WRONG", "alice"},
		"unknown udid": {"token1", "nobody"},
	} {
		got := v(args[0], args[1], "")
		if got.OK || got.Code != 0 {
			t.Errorf("%s: got %+v, want no-opinion {OK:false Code:0}", name, got)
		}
	}
}

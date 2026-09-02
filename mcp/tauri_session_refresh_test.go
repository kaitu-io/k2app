package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// writeTauriStorage writes a plaintext desktop storage.json holding the given
// access token (plaintext values need no hardware key to read).
func writeTauriStorage(t *testing.T, path, access string) {
	t.Helper()
	content := fmt.Sprintf(`{
		"k2.auth.token": "\"%s\"",
		"k2.auth.refresh": "\"desktop-refresh\"",
		"device-udid": "\"04cacd29-e71a-4884-842d-a2a5892d4db9\""
	}`, access)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// newTauriOwnedSession returns a Session restored from a fake desktop storage.json.
func newTauriOwnedSession(t *testing.T, storagePath string) *Session {
	t.Helper()
	sess := NewSession(t.TempDir())
	sess.tauriPath = func() string { return storagePath }
	sess.hwID = func() (string, error) { return "test-hardware-id", nil }
	if !sess.RestoreFromTauri() {
		t.Fatal("RestoreFromTauri failed on the fake storage.json")
	}
	return sess
}

// centerStub accepts exactly one bearer token and counts refresh attempts.
func centerStub(t *testing.T, validToken string, refreshHits *int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/auth/refresh" {
			atomic.AddInt32(refreshHits, 1)
			json.NewEncoder(w).Encode(centerResponse{Code: 401, Message: "refresh must not be called"})
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+validToken {
			json.NewEncoder(w).Encode(centerResponse{Code: 401, Message: "token expired"})
			return
		}
		json.NewEncoder(w).Encode(centerResponse{Code: 0, Message: "ok", Data: json.RawMessage(`{"ok":true}`)})
	}))
}

// The desktop app refreshed its own tokens (storage.json changed). MCP must
// pick the new token up by re-reading the file — never by calling
// /api/auth/refresh, which rotates device.TokenIssueAt on Center and
// invalidates both of the desktop's tokens (api/api_auth.go, logic_auth.go).
func TestCenterClient_TauriOwnedTokens_ReloadsInsteadOfRefreshing(t *testing.T) {
	storage := filepath.Join(t.TempDir(), "storage.json")
	writeTauriStorage(t, storage, "old-token")
	sess := newTauriOwnedSession(t, storage)

	var refreshHits int32
	srv := centerStub(t, "new-token", &refreshHits)
	defer srv.Close()

	c := NewCenterClient(srv.URL)
	c.SetToken("old-token")
	c.SetRefreshSource(sess)

	// The desktop rotates its session behind our back.
	writeTauriStorage(t, storage, "new-token")

	var out map[string]any
	if err := c.Get("/api/user", &out); err != nil {
		t.Fatalf("expected the reloaded desktop token to succeed, got %v", err)
	}
	if got := atomic.LoadInt32(&refreshHits); got != 0 {
		t.Fatalf("/api/auth/refresh was called %d times with desktop-owned tokens", got)
	}
	if sess.AccessToken != "new-token" {
		t.Fatalf("session should carry the reloaded token, got %q", sess.AccessToken)
	}
}

// Storage unchanged: the request fails with the 401 and refresh is still never attempted.
func TestCenterClient_TauriOwnedTokens_NeverRefreshes(t *testing.T) {
	storage := filepath.Join(t.TempDir(), "storage.json")
	writeTauriStorage(t, storage, "old-token")
	sess := newTauriOwnedSession(t, storage)

	var refreshHits int32
	srv := centerStub(t, "some-other-token", &refreshHits)
	defer srv.Close()

	c := NewCenterClient(srv.URL)
	c.SetToken("old-token")
	c.SetRefreshSource(sess)

	var out map[string]any
	err := c.Get("/api/user", &out)
	var ce *CenterError
	if !errors.As(err, &ce) || ce.Code != 401 {
		t.Fatalf("expected a 401 CenterError, got %v", err)
	}
	if got := atomic.LoadInt32(&refreshHits); got != 0 {
		t.Fatalf("/api/auth/refresh was called %d times with desktop-owned tokens", got)
	}
}

// MCP's own session (login through MCP) still owns and refreshes its tokens.
func TestSession_OwnsTokens(t *testing.T) {
	own := NewSession(t.TempDir())
	if !own.OwnsTokens() {
		t.Fatal("an MCP-login session must own its tokens")
	}
	storage := filepath.Join(t.TempDir(), "storage.json")
	writeTauriStorage(t, storage, "tok")
	shared := newTauriOwnedSession(t, storage)
	if shared.OwnsTokens() {
		t.Fatal("a Tauri-restored session must not own its tokens")
	}
}

// A 401 on a shared session points the user at the desktop app instead of
// at MCP login, which would be shadowed by Tauri storage on the next start.
func TestHandleCenterError_401_TauriOwnedSession(t *testing.T) {
	storage := filepath.Join(t.TempDir(), "storage.json")
	writeTauriStorage(t, storage, "tok")
	app := newTestApp(t, "http://127.0.0.1:1")
	app.session = newTauriOwnedSession(t, storage)

	txt := textContent(t, app.handleCenterError(&CenterError{Code: 401, Message: "expired"}))
	if !strings.Contains(txt, "desktop app") {
		t.Fatalf("expected desktop-app guidance for a shared session, got %q", txt)
	}
}

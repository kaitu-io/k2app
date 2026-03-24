package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// sessionData is the on-disk representation of the session.
type sessionData struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	Email        string    `json:"email"`
	IssuedAt     time.Time `json:"issued_at"`
}

// Session manages authentication tokens and device identity.
type Session struct {
	mu           sync.RWMutex
	AccessToken  string
	RefreshToken string
	Email        string
	IssuedAt     time.Time
	dir          string
}

// NewSession creates a new Session that persists data to dir.
func NewSession(dir string) *Session {
	return &Session{dir: dir}
}

// SetTokens sets the auth tokens under a write lock.
func (s *Session) SetTokens(access, refresh, email string, issuedAt time.Time) {
	s.mu.Lock()
	s.AccessToken = access
	s.RefreshToken = refresh
	s.Email = email
	s.IssuedAt = issuedAt
	s.mu.Unlock()
}

// LoggedIn returns true if an access token is present.
func (s *Session) LoggedIn() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.AccessToken != ""
}

// Save persists the session to dir/session.json with 0600 permissions.
func (s *Session) Save() error {
	s.mu.RLock()
	data := sessionData{
		AccessToken:  s.AccessToken,
		RefreshToken: s.RefreshToken,
		Email:        s.Email,
		IssuedAt:     s.IssuedAt,
	}
	s.mu.RUnlock()

	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("session save marshal: %w", err)
	}

	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return fmt.Errorf("session save mkdir: %w", err)
	}

	path := filepath.Join(s.dir, "session.json")
	if err := os.WriteFile(path, b, 0600); err != nil {
		return fmt.Errorf("session save write: %w", err)
	}
	return nil
}

// Restore loads session from dir/session.json.
// No error is returned if the file does not exist.
func (s *Session) Restore() error {
	path := filepath.Join(s.dir, "session.json")
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("session restore read: %w", err)
	}

	var data sessionData
	if err := json.Unmarshal(b, &data); err != nil {
		return fmt.Errorf("session restore unmarshal: %w", err)
	}

	s.mu.Lock()
	s.AccessToken = data.AccessToken
	s.RefreshToken = data.RefreshToken
	s.Email = data.Email
	s.IssuedAt = data.IssuedAt
	s.mu.Unlock()
	return nil
}

// GetRefreshToken returns the current refresh token under a read lock.
// This satisfies the RefreshSource interface.
func (s *Session) GetRefreshToken() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.RefreshToken
}

// UpdateTokens replaces the access and refresh tokens in memory and persists
// them to disk. issuedAt is a Unix timestamp. This satisfies RefreshSource.
func (s *Session) UpdateTokens(access, refresh string, issuedAt int64) {
	s.mu.Lock()
	s.AccessToken = access
	s.RefreshToken = refresh
	s.IssuedAt = time.Unix(issuedAt, 0)
	s.mu.Unlock()
	_ = s.Save()
}

// Clear wipes all tokens in memory and removes the session file.
func (s *Session) Clear() error {
	s.mu.Lock()
	s.AccessToken = ""
	s.RefreshToken = ""
	s.Email = ""
	s.IssuedAt = time.Time{}
	s.mu.Unlock()

	path := filepath.Join(s.dir, "session.json")
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("session clear remove: %w", err)
	}
	return nil
}

// UDID returns a stable 16-char hex device identifier.
// It reads from dir/mcp-udid if present; otherwise generates
// sha256(hostname + first-non-loopback-MAC)[:16] and stores it.
func (s *Session) UDID() string {
	path := filepath.Join(s.dir, "mcp-udid")

	b, err := os.ReadFile(path)
	if err == nil && len(b) >= 16 {
		return string(b[:16])
	}

	id := generateUDID()

	// best-effort persist
	_ = os.MkdirAll(s.dir, 0700)
	_ = os.WriteFile(path, []byte(id), 0600)

	return id
}

// generateUDID computes sha256(hostname + first-non-loopback-MAC)[:16] hex.
func generateUDID() string {
	hostname, _ := os.Hostname()

	var mac string
	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if len(iface.HardwareAddr) > 0 {
			mac = iface.HardwareAddr.String()
			break
		}
	}

	h := sha256.Sum256([]byte(hostname + mac))
	return hex.EncodeToString(h[:])[:16]
}

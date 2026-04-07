package signal

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sasha-s/go-deadlock"
	"golang.org/x/crypto/bcrypt"
)

var udidRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,30}[a-z0-9]$`)

// ValidateUDID returns an error if udid does not match the k2p UDID format.
// Valid UDIDs: lowercase alphanumeric with internal hyphens, 3–32 characters total.
func ValidateUDID(udid string) error {
	if !udidRe.MatchString(udid) {
		return fmt.Errorf("signal: invalid UDID %q", udid)
	}
	return nil
}

// Store is an in-memory registry of peer entries.
type Store struct {
	mu    deadlock.RWMutex
	peers map[string]*PeerEntry
}

// NewStore returns an empty Store.
func NewStore() *Store {
	return &Store{
		peers: make(map[string]*PeerEntry),
	}
}

// Claim registers a new UDID and returns its registration token.
// Returns an error if the UDID already exists.
//
// bcrypt hashing (~100–300ms) is performed OUTSIDE the lock.
func (s *Store) Claim(udid string) (string, error) {
	// Check existence under read lock before doing expensive work.
	s.mu.RLock()
	_, exists := s.peers[udid]
	s.mu.RUnlock()

	if exists {
		return "", fmt.Errorf("signal: claim: udid %q already registered", udid)
	}

	// Generate token and hash OUTSIDE any lock.
	token, err := generateToken()
	if err != nil {
		return "", fmt.Errorf("signal: claim: generate token: %w", err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(token), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("signal: claim: bcrypt: %w", err)
	}

	// Acquire write lock only to store the result.
	s.mu.Lock()
	// Re-check under write lock to avoid TOCTOU race.
	if _, exists := s.peers[udid]; exists {
		s.mu.Unlock()
		return "", fmt.Errorf("signal: claim: udid %q already registered", udid)
	}
	s.peers[udid] = &PeerEntry{
		UDID:         udid,
		RegTokenHash: string(hash),
		UpdatedAt:    time.Now(),
	}
	s.mu.Unlock()

	return token, nil
}

// Update applies a RegisterRequest to the stored entry after verifying the token.
//
// bcrypt verification (~100–300ms) is performed OUTSIDE the lock.
func (s *Store) Update(udid, token string, req RegisterRequest) error {
	// Copy the hash under read lock.
	s.mu.RLock()
	entry, ok := s.peers[udid]
	var hash string
	if ok {
		hash = entry.RegTokenHash
	}
	s.mu.RUnlock()

	if !ok {
		return fmt.Errorf("signal: update: udid %q not found", udid)
	}

	// Verify token OUTSIDE any lock.
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(token)); err != nil {
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			return fmt.Errorf("signal: update: invalid token")
		}
		return fmt.Errorf("signal: update: bcrypt: %w", err)
	}

	// Re-acquire write lock to update fields.
	s.mu.Lock()
	e, ok := s.peers[udid]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("signal: update: udid %q not found", udid)
	}
	e.Networks = req.Networks
	e.DNS = req.DNS
	e.UpdatedAt = time.Now()
	s.mu.Unlock()

	return nil
}

// PeerSnapshot is a read-only copy of a PeerEntry's public fields.
// Does not contain the mutex or WS connection.
type PeerSnapshot struct {
	UDID      string
	Networks  []string
	DNS       string
	UpdatedAt time.Time
}

// Get returns a snapshot of the entry for udid.
func (s *Store) Get(udid string) (PeerSnapshot, bool) {
	s.mu.RLock()
	e, ok := s.peers[udid]
	if !ok {
		s.mu.RUnlock()
		return PeerSnapshot{}, false
	}
	var nets []string
	if len(e.Networks) > 0 {
		nets = make([]string, len(e.Networks))
		copy(nets, e.Networks)
	}
	out := PeerSnapshot{
		UDID:      e.UDID,
		Networks:  nets,
		DNS:       e.DNS,
		UpdatedAt: e.UpdatedAt,
	}
	s.mu.RUnlock()
	return out, true
}

// SetWS stores (or clears) the WebSocket connection for udid.
func (s *Store) SetWS(udid string, ws *websocket.Conn) {
	s.mu.Lock()
	if e, ok := s.peers[udid]; ok {
		e.WS = ws
	}
	s.mu.Unlock()
}

// GetWS returns the WebSocket connection for udid, or nil if not set.
func (s *Store) GetWS(udid string) *websocket.Conn {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.peers[udid]
	if !ok {
		return nil
	}
	return e.WS
}

// IsOnline reports whether udid has an active WebSocket connection.
func (s *Store) IsOnline(udid string) bool {
	return s.GetWS(udid) != nil
}

// ValidateToken returns true if token matches the stored bcrypt hash for udid.
//
// bcrypt verification is performed OUTSIDE the lock.
func (s *Store) ValidateToken(udid, token string) bool {
	s.mu.RLock()
	e, ok := s.peers[udid]
	var hash string
	if ok {
		hash = e.RegTokenHash
	}
	s.mu.RUnlock()

	if !ok {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(token)) == nil
}

// WriteToWS sends a WebSocket message to the peer's connection with write serialization.
// Returns an error if the peer is not connected or the write fails.
func (s *Store) WriteToWS(udid string, msgType int, data []byte) error {
	s.mu.RLock()
	e, ok := s.peers[udid]
	if !ok || e.WS == nil {
		s.mu.RUnlock()
		return fmt.Errorf("signal: peer %q not connected", udid)
	}
	ws := e.WS      // snapshot under RLock — prevents TOCTOU with SetWS(nil)
	wmu := &e.WriteMu // PeerEntry is heap-allocated, pointer remains valid after RUnlock
	s.mu.RUnlock()
	wmu.Lock()
	defer wmu.Unlock()
	return ws.WriteMessage(msgType, data)
}

// generateToken returns a cryptographically random 32-byte hex string.
func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

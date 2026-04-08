package wire

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/kaitu-io/k2/safego"
)

const (
	// ticketRenewalThreshold is the fraction of TTL remaining below which
	// a ticket is automatically renewed. 0.5 = 50% remaining.
	ticketRenewalThreshold = 0.5
)

// AuthTicket represents a server-issued auth ticket.
type AuthTicket struct {
	Token      string
	DeviceID   string
	Mode       string // "gateway" or "" (normal client)
	UDPEnabled bool
	CreatedAt  time.Time
	ExpiresAt  time.Time
}

// AuthResponse is the JSON response from POST /k2v5/auth.
type AuthResponse struct {
	Ticket  string `json:"ticket"`
	Expires int    `json:"expires"` // seconds
	UDP     bool   `json:"udp"`
	Version int    `json:"version"`
}

// TicketStore manages auth tickets in memory.
type TicketStore struct {
	mu          sync.RWMutex
	byToken     map[string]*AuthTicket
	byDevice    map[string]string // deviceID → current token
	graceTokens map[string]time.Time // old token → grace expiry

	ticketTTL   time.Duration
	gracePeriod time.Duration
}

// NewTicketStore creates a new ticket store with default settings.
func NewTicketStore() *TicketStore {
	return &TicketStore{
		byToken:     make(map[string]*AuthTicket),
		byDevice:    make(map[string]string),
		graceTokens: make(map[string]time.Time),
		ticketTTL:   time.Hour,
		gracePeriod: 30 * time.Second,
	}
}

// Issue creates a new ticket for the device, invalidating any previous ticket.
func (s *TicketStore) Issue(deviceID string, udpEnabled bool) (*AuthTicket, error) {
	token, err := randomHex(32)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	ticket := &AuthTicket{
		Token:      token,
		DeviceID:   deviceID,
		UDPEnabled: udpEnabled,
		CreatedAt:  now,
		ExpiresAt:  now.Add(s.ticketTTL),
	}

	s.mu.Lock()
	// concurrency:allow — pure in-memory map operations, no I/O or external calls
	defer s.mu.Unlock()

	// Move old ticket to grace period
	if oldToken, ok := s.byDevice[deviceID]; ok {
		delete(s.byToken, oldToken)
		s.graceTokens[oldToken] = now.Add(s.gracePeriod)
	}

	s.byToken[token] = ticket
	s.byDevice[deviceID] = token

	return ticket, nil
}

// Validate checks if a ticket is valid and returns it.
func (s *TicketStore) Validate(token string) (*AuthTicket, bool) {
	s.mu.RLock()
	// concurrency:allow — pure in-memory map operations, no I/O or external calls
	defer s.mu.RUnlock()

	now := time.Now()

	// Check active tickets
	if ticket, ok := s.byToken[token]; ok {
		if now.Before(ticket.ExpiresAt) {
			return ticket, true
		}
		return nil, false
	}

	// Check grace period tickets
	if graceExpiry, ok := s.graceTokens[token]; ok {
		if now.Before(graceExpiry) {
			// Return a minimal ticket — enough to authenticate
			return &AuthTicket{Token: token, DeviceID: "grace"}, true
		}
	}

	return nil, false
}

// Remaining returns seconds until ticket expires.
func (s *TicketStore) Remaining(token string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if ticket, ok := s.byToken[token]; ok {
		rem := time.Until(ticket.ExpiresAt).Seconds()
		if rem < 0 {
			return 0
		}
		return int(rem)
	}
	return 0
}

// StartCleanup starts a background goroutine that periodically removes expired
// tickets and grace tokens. The goroutine stops when done is closed.
func (s *TicketStore) StartCleanup(interval time.Duration, done chan struct{}) {
	safego.Go(func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.cleanupSafe()
			case <-done:
				return
			}
		}
	})
}

// cleanupSafe wraps cleanup() with panic recovery so the cleanup loop survives.
func (s *TicketStore) cleanupSafe() {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("auth: cleanup panic recovered", "panic", r,
				"stack", string(debug.Stack()))
		}
	}()
	s.cleanup()
}

// cleanup removes expired tickets and grace tokens.
func (s *TicketStore) cleanup() {
	s.mu.Lock()
	// concurrency:allow — pure in-memory map operations, no I/O or external calls
	defer s.mu.Unlock()

	now := time.Now()

	// Clean expired tickets
	for token, ticket := range s.byToken {
		if now.After(ticket.ExpiresAt) {
			delete(s.byToken, token)
			// Also clean byDevice if this was the current token
			if currentToken, ok := s.byDevice[ticket.DeviceID]; ok && currentToken == token {
				delete(s.byDevice, ticket.DeviceID)
			}
		}
	}

	// Clean expired grace tokens
	for token, expiry := range s.graceTokens {
		if now.After(expiry) {
			delete(s.graceTokens, token)
		}
	}
}

// JWTValidator validates a JWT token, device UDID, and optional client mode.
// Mode is "gateway" for router clients or "" for normal clients.
// Validators that don't support mode-based authorization should ignore the mode parameter.
type JWTValidator func(jwt, udid, mode string) bool

// AuthHandler handles POST /k2v5/auth requests.
type AuthHandler struct {
	store    *TicketStore
	validate JWTValidator
}

// NewAuthHandler creates a new auth handler.
func NewAuthHandler(store *TicketStore, validate JWTValidator) *AuthHandler {
	return &AuthHandler{store: store, validate: validate}
}

func (h *AuthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	deviceID := r.Header.Get("X-K2-Device")
	if deviceID == "" {
		http.Error(w, `{"error":"missing X-K2-Device"}`, http.StatusUnauthorized)
		return
	}

	// Try ticket auth first
	if ticketToken := r.Header.Get("X-K2-Ticket"); ticketToken != "" {
		_, ok := h.store.Validate(ticketToken)
		if !ok {
			http.Error(w, `{"error":"invalid ticket"}`, http.StatusUnauthorized)
			return
		}
		// Ticket valid — return remaining time
		h.respondTicket(w, ticketToken, deviceID)
		return
	}

	// Full auth with JWT
	auth := r.Header.Get("Authorization")
	jwt := strings.TrimPrefix(auth, "Bearer ")
	if jwt == "" || jwt == auth { // no "Bearer " prefix found
		http.Error(w, `{"error":"missing Authorization"}`, http.StatusUnauthorized)
		return
	}

	mode := r.Header.Get("X-K2-Mode") // "gateway" or "" (normal)

	if !h.validate(jwt, deviceID, mode) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	// Issue new ticket
	ticket, err := h.store.Issue(deviceID, true)
	if err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	ticket.Mode = mode

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(AuthResponse{
		Ticket:  ticket.Token,
		Expires: h.store.Remaining(ticket.Token),
		UDP:     ticket.UDPEnabled,
		Version: 1,
	})
}

func (h *AuthHandler) respondTicket(w http.ResponseWriter, token, deviceID string) {
	h.store.mu.RLock()
	ticket, ok := h.store.byToken[token]
	h.store.mu.RUnlock()

	if !ok {
		// Shouldn't happen (already validated), but handle gracefully
		http.Error(w, `{"error":"invalid ticket"}`, http.StatusUnauthorized)
		return
	}

	remaining := time.Until(ticket.ExpiresAt)
	total := h.store.ticketTTL

	// Check if renewal is needed (< 50% TTL remaining)
	if remaining < time.Duration(float64(total)*ticketRenewalThreshold) {
		// Auto-renew: issue new ticket, old enters grace
		newTicket, err := h.store.Issue(deviceID, true)
		if err != nil {
			http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(AuthResponse{
			Ticket:  newTicket.Token,
			Expires: h.store.Remaining(newTicket.Token),
			UDP:     newTicket.UDPEnabled,
			Version: 1,
		})
		return
	}

	// Ticket fresh enough, return current info
	remainingSecs := int(remaining.Seconds())
	if remainingSecs < 0 {
		remainingSecs = 0
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(AuthResponse{
		Ticket:  token,
		Expires: remainingSecs,
		UDP:     true,
		Version: 1,
	})
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

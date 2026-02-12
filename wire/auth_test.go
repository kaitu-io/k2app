package wire

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTicketStore_IssueAndValidate(t *testing.T) {
	store := NewTicketStore()

	ticket, err := store.Issue("device1", true)
	if err != nil {
		t.Fatalf("Issue failed: %v", err)
	}
	if len(ticket.Token) != 64 { // 32 bytes = 64 hex chars
		t.Errorf("ticket token length = %d, want 64", len(ticket.Token))
	}
	if !ticket.UDPEnabled {
		t.Error("UDP should be enabled")
	}

	// Validate the ticket
	got, ok := store.Validate(ticket.Token)
	if !ok {
		t.Fatal("Validate returned false for valid ticket")
	}
	if got.DeviceID != "device1" {
		t.Errorf("device = %q, want %q", got.DeviceID, "device1")
	}
}

func TestTicketStore_OnePerDevice(t *testing.T) {
	store := NewTicketStore()
	store.gracePeriod = 50 * time.Millisecond // short grace for testing

	t1, _ := store.Issue("device1", true)
	t2, _ := store.Issue("device1", true) // t1 enters grace period

	if t1.Token == t2.Token {
		t.Error("second issue should create a different token")
	}

	// Both valid during grace period
	_, ok1 := store.Validate(t1.Token)
	_, ok2 := store.Validate(t2.Token)
	if !ok1 {
		t.Error("old ticket should be valid during grace period")
	}
	if !ok2 {
		t.Error("new ticket should be valid")
	}

	// After grace period, only new ticket valid
	time.Sleep(60 * time.Millisecond)

	_, ok1 = store.Validate(t1.Token)
	_, ok2 = store.Validate(t2.Token)
	if ok1 {
		t.Error("old ticket should be invalid after grace period")
	}
	if !ok2 {
		t.Error("new ticket should still be valid")
	}
}

func TestTicketStore_Expiry(t *testing.T) {
	store := NewTicketStore()
	store.ticketTTL = 50 * time.Millisecond // short TTL for testing

	ticket, _ := store.Issue("device1", true)

	// Valid immediately
	_, ok := store.Validate(ticket.Token)
	if !ok {
		t.Fatal("ticket should be valid immediately")
	}

	// Wait for expiry
	time.Sleep(60 * time.Millisecond)

	_, ok = store.Validate(ticket.Token)
	if ok {
		t.Error("ticket should be expired")
	}
}

func TestTicketStore_GracePeriod(t *testing.T) {
	store := NewTicketStore()
	store.gracePeriod = 100 * time.Millisecond

	t1, _ := store.Issue("device1", true)
	t2, _ := store.Issue("device1", true) // t1 enters grace period

	// Both should be valid during grace period
	_, ok1 := store.Validate(t1.Token)
	_, ok2 := store.Validate(t2.Token)
	if !ok1 {
		t.Error("old ticket should be valid during grace period")
	}
	if !ok2 {
		t.Error("new ticket should be valid")
	}

	// Wait for grace period to expire
	time.Sleep(120 * time.Millisecond)

	_, ok1 = store.Validate(t1.Token)
	if ok1 {
		t.Error("old ticket should be invalid after grace period")
	}
}

func TestAuthHandler_FullAuth(t *testing.T) {
	store := NewTicketStore()
	validator := func(jwt, udid string) bool {
		return jwt == "valid-jwt" && udid == "device1"
	}
	handler := NewAuthHandler(store, validator)

	body := `{}`
	req := httptest.NewRequest("POST", "/k2v5/auth", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer valid-jwt")
	req.Header.Set("X-K2-Device", "device1")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", w.Code, w.Body.String())
	}

	var resp AuthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if resp.Ticket == "" {
		t.Error("response should contain ticket")
	}
	if resp.Expires <= 0 {
		t.Error("expires should be positive")
	}
}

func TestAuthHandler_TicketAuth(t *testing.T) {
	store := NewTicketStore()
	validator := func(jwt, udid string) bool { return true }
	handler := NewAuthHandler(store, validator)

	// First, issue a ticket
	ticket, _ := store.Issue("device1", true)

	// Now authenticate with ticket
	req := httptest.NewRequest("POST", "/k2v5/auth", nil)
	req.Header.Set("X-K2-Ticket", ticket.Token)
	req.Header.Set("X-K2-Device", "device1")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", w.Code, w.Body.String())
	}
}

func TestAuthHandler_InvalidJWT(t *testing.T) {
	store := NewTicketStore()
	validator := func(jwt, udid string) bool { return false }
	handler := NewAuthHandler(store, validator)

	req := httptest.NewRequest("POST", "/k2v5/auth", nil)
	req.Header.Set("Authorization", "Bearer bad-jwt")
	req.Header.Set("X-K2-Device", "device1")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestAuthHandler_MissingDevice(t *testing.T) {
	store := NewTicketStore()
	validator := func(jwt, udid string) bool { return true }
	handler := NewAuthHandler(store, validator)

	req := httptest.NewRequest("POST", "/k2v5/auth", nil)
	req.Header.Set("Authorization", "Bearer valid-jwt")
	// No X-K2-Device header
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestAuthHandler_InvalidTicket(t *testing.T) {
	store := NewTicketStore()
	validator := func(jwt, udid string) bool { return true }
	handler := NewAuthHandler(store, validator)

	req := httptest.NewRequest("POST", "/k2v5/auth", nil)
	req.Header.Set("X-K2-Ticket", "nonexistent-ticket")
	req.Header.Set("X-K2-Device", "device1")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

// TestAuthHandler_TicketRenewal_Fresh verifies AC15: ticket with > 50% TTL
// remaining returns current ticket without re-issuing.
func TestAuthHandler_TicketRenewal_Fresh(t *testing.T) {
	store := NewTicketStore()
	store.ticketTTL = 100 * time.Second // 100s total TTL
	validator := func(jwt, udid string) bool { return true }
	handler := NewAuthHandler(store, validator)

	// Issue a ticket (it will have ~100s remaining = 100% TTL)
	ticket, _ := store.Issue("device1", true)
	originalToken := ticket.Token

	// Sleep a small amount so remaining is ~90% (> 50% threshold)
	time.Sleep(10 * time.Millisecond)

	// Authenticate with the ticket
	req := httptest.NewRequest("POST", "/k2v5/auth", nil)
	req.Header.Set("X-K2-Ticket", originalToken)
	req.Header.Set("X-K2-Device", "device1")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", w.Code, w.Body.String())
	}

	var resp AuthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json decode: %v", err)
	}

	// Should return the SAME token (no renewal)
	if resp.Ticket != originalToken {
		t.Errorf("ticket changed: got %q, want original %q", resp.Ticket, originalToken)
	}
}

// TestAuthHandler_TicketRenewal_Expiring verifies AC16: ticket with < 50% TTL
// remaining triggers renewal, issuing a new ticket and moving old to grace.
func TestAuthHandler_TicketRenewal_Expiring(t *testing.T) {
	store := NewTicketStore()
	store.ticketTTL = 100 * time.Millisecond // 100ms total TTL
	validator := func(jwt, udid string) bool { return true }
	handler := NewAuthHandler(store, validator)

	// Issue a ticket
	ticket, _ := store.Issue("device1", true)
	originalToken := ticket.Token

	// Sleep until remaining TTL is < 50% (e.g., 60ms elapsed, 40ms remaining = 40%)
	time.Sleep(65 * time.Millisecond)

	// Authenticate with the ticket
	req := httptest.NewRequest("POST", "/k2v5/auth", nil)
	req.Header.Set("X-K2-Ticket", originalToken)
	req.Header.Set("X-K2-Device", "device1")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", w.Code, w.Body.String())
	}

	var resp AuthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json decode: %v", err)
	}

	// Should return a NEW token (renewal triggered)
	if resp.Ticket == originalToken {
		t.Errorf("ticket not renewed: got same token %q", resp.Ticket)
	}

	// Old token should be in grace period
	_, ok := store.Validate(originalToken)
	if !ok {
		t.Error("old ticket should still be valid during grace period")
	}
}

// TestTicketStore_StartCleanup_Tickets verifies AC17: expired tickets are
// removed by background cleanup goroutine.
func TestTicketStore_StartCleanup_Tickets(t *testing.T) {
	store := NewTicketStore()
	store.ticketTTL = 50 * time.Millisecond

	// Issue a ticket
	ticket, _ := store.Issue("device1", true)

	// Start cleanup with short interval
	done := make(chan struct{})
	defer close(done)
	store.StartCleanup(30*time.Millisecond, done)

	// Wait for ticket to expire
	time.Sleep(60 * time.Millisecond)

	// Wait for at least one cleanup cycle
	time.Sleep(40 * time.Millisecond)

	// Ticket should be removed from store
	store.mu.RLock()
	_, exists := store.byToken[ticket.Token]
	store.mu.RUnlock()

	if exists {
		t.Error("expired ticket should be removed by cleanup")
	}
}

// TestTicketStore_StartCleanup_GraceTokens verifies AC18: expired grace tokens
// are removed by background cleanup goroutine.
func TestTicketStore_StartCleanup_GraceTokens(t *testing.T) {
	store := NewTicketStore()
	store.gracePeriod = 50 * time.Millisecond

	// Issue two tickets for same device (first enters grace)
	t1, _ := store.Issue("device1", true)
	oldToken := t1.Token
	store.Issue("device1", true) // t1 enters grace period

	// Start cleanup with short interval
	done := make(chan struct{})
	defer close(done)
	store.StartCleanup(30*time.Millisecond, done)

	// Wait for grace period to expire
	time.Sleep(60 * time.Millisecond)

	// Wait for at least one cleanup cycle
	time.Sleep(40 * time.Millisecond)

	// Grace token should be removed
	store.mu.RLock()
	_, exists := store.graceTokens[oldToken]
	store.mu.RUnlock()

	if exists {
		t.Error("expired grace token should be removed by cleanup")
	}
}

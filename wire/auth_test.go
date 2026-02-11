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

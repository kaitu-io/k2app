package signal

import (
	"testing"

	"github.com/gorilla/websocket"
)

func TestStore_ClaimAndGet(t *testing.T) {
	s := NewStore()

	// Claim a new UDID.
	token, err := s.Claim("udid-1")
	if err != nil {
		t.Fatalf("Claim: unexpected error: %v", err)
	}
	if token == "" {
		t.Fatal("Claim: expected non-empty token")
	}

	// Duplicate claim must fail.
	_, err = s.Claim("udid-1")
	if err == nil {
		t.Fatal("Claim: expected error for duplicate UDID")
	}

	// Get returns the stored entry (no WS).
	entry, ok := s.Get("udid-1")
	if !ok {
		t.Fatal("Get: expected entry to exist")
	}
	if entry.UDID != "udid-1" {
		t.Errorf("Get: UDID = %q, want %q", entry.UDID, "udid-1")
	}

	// Get non-existent UDID.
	_, ok = s.Get("no-such-udid")
	if ok {
		t.Error("Get: expected not found for unknown UDID")
	}
}

func TestStore_Update_ValidToken(t *testing.T) {
	s := NewStore()

	token, err := s.Claim("home-1")
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}

	req := RegisterRequest{
		UDID:     "home-1",
		Networks: []string{"192.168.1.0/24"},
		DNS:      "192.168.1.1",
	}
	if err := s.Update("home-1", token, req); err != nil {
		t.Fatalf("Update: unexpected error: %v", err)
	}

	entry, ok := s.Get("home-1")
	if !ok {
		t.Fatal("Get: entry not found after Update")
	}
	if entry.DNS != "192.168.1.1" {
		t.Errorf("DNS = %q, want %q", entry.DNS, "192.168.1.1")
	}
	if len(entry.Networks) != 1 || entry.Networks[0] != "192.168.1.0/24" {
		t.Errorf("Networks = %v, want [192.168.1.0/24]", entry.Networks)
	}
}

func TestStore_Update_BadToken(t *testing.T) {
	s := NewStore()

	if _, err := s.Claim("home-2"); err != nil {
		t.Fatalf("Claim: %v", err)
	}

	req := RegisterRequest{UDID: "home-2"}
	err := s.Update("home-2", "wrong-token", req)
	if err == nil {
		t.Fatal("Update: expected error for bad token")
	}
}

func TestStore_Update_NotFound(t *testing.T) {
	s := NewStore()

	req := RegisterRequest{UDID: "ghost"}
	err := s.Update("ghost", "any-token", req)
	if err == nil {
		t.Fatal("Update: expected error for unknown UDID")
	}
}

func TestStore_SetWS_IsOnline(t *testing.T) {
	s := NewStore()

	if _, err := s.Claim("home-3"); err != nil {
		t.Fatalf("Claim: %v", err)
	}

	// Before SetWS: offline.
	if s.IsOnline("home-3") {
		t.Error("IsOnline: expected false before SetWS")
	}

	// SetWS with a non-nil conn: online.
	// We use a non-nil sentinel value via a fake *websocket.Conn pointer.
	// websocket.Conn has no exported constructor; allocate via new().
	fakeWS := new(websocket.Conn)
	s.SetWS("home-3", fakeWS)
	if !s.IsOnline("home-3") {
		t.Error("IsOnline: expected true after SetWS(non-nil)")
	}
	if s.GetWS("home-3") != fakeWS {
		t.Error("GetWS: returned unexpected value")
	}

	// SetWS(nil): offline.
	s.SetWS("home-3", nil)
	if s.IsOnline("home-3") {
		t.Error("IsOnline: expected false after SetWS(nil)")
	}
}

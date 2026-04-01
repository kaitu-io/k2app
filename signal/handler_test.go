package signal

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newTestHandler returns a Handler backed by a fresh Store for unit tests.
func newTestHandler() *Handler {
	return NewHandler(NewStore(), "", nil)
}

// doJSON performs a JSON request against the handler and returns the response.
func doJSON(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func doJSONWithAuth(t *testing.T, h http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

// TestHandler_ClaimAndRegisterAndLookup verifies the full happy-path flow.
func TestHandler_ClaimAndRegisterAndLookup(t *testing.T) {
	h := newTestHandler()

	// 1. Claim.
	rr := doJSON(t, h, http.MethodPost, "/k2p/claim", ClaimRequest{UDID: "home-1"})
	if rr.Code != http.StatusOK {
		t.Fatalf("claim: got %d, want 200; body: %s", rr.Code, rr.Body)
	}
	var cr ClaimResponse
	if err := json.NewDecoder(rr.Body).Decode(&cr); err != nil {
		t.Fatalf("claim: decode response: %v", err)
	}
	if cr.RegToken == "" {
		t.Fatal("claim: expected non-empty reg_token")
	}

	// 2. Register with the returned token.
	regReq := RegisterRequest{
		UDID:     "home-1",
		Networks: []string{"192.168.1.0/24"},
		DNS:      "192.168.1.1",
	}
	rr = doJSONWithAuth(t, h, http.MethodPost, "/k2p/register", cr.RegToken, regReq)
	if rr.Code != http.StatusOK {
		t.Fatalf("register: got %d, want 200; body: %s", rr.Code, rr.Body)
	}

	// 3. Lookup verifies the stored data.
	rr = doJSON(t, h, http.MethodGet, "/k2p/lookup/home-1", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("lookup: got %d, want 200; body: %s", rr.Code, rr.Body)
	}
	var lr LookupResponse
	if err := json.NewDecoder(rr.Body).Decode(&lr); err != nil {
		t.Fatalf("lookup: decode response: %v", err)
	}
	if lr.UDID != "home-1" {
		t.Errorf("lookup: UDID = %q, want %q", lr.UDID, "home-1")
	}
	if lr.DNS != "192.168.1.1" {
		t.Errorf("lookup: DNS = %q, want %q", lr.DNS, "192.168.1.1")
	}
	if len(lr.Networks) != 1 || lr.Networks[0] != "192.168.1.0/24" {
		t.Errorf("lookup: Networks = %v, want [192.168.1.0/24]", lr.Networks)
	}
}

// TestHandler_ClaimDuplicate verifies that a second claim for the same UDID returns 409.
func TestHandler_ClaimDuplicate(t *testing.T) {
	h := newTestHandler()

	rr := doJSON(t, h, http.MethodPost, "/k2p/claim", ClaimRequest{UDID: "dup-udid"})
	if rr.Code != http.StatusOK {
		t.Fatalf("first claim: got %d, want 200", rr.Code)
	}

	rr = doJSON(t, h, http.MethodPost, "/k2p/claim", ClaimRequest{UDID: "dup-udid"})
	if rr.Code != http.StatusConflict {
		t.Fatalf("duplicate claim: got %d, want 409", rr.Code)
	}
}

// TestHandler_RegisterBadToken verifies that a wrong token returns 403.
func TestHandler_RegisterBadToken(t *testing.T) {
	h := newTestHandler()

	rr := doJSON(t, h, http.MethodPost, "/k2p/claim", ClaimRequest{UDID: "auth-udid"})
	if rr.Code != http.StatusOK {
		t.Fatalf("claim: got %d, want 200", rr.Code)
	}

	regReq := RegisterRequest{UDID: "auth-udid"}
	rr = doJSONWithAuth(t, h, http.MethodPost, "/k2p/register", "wrong-token", regReq)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("register bad token: got %d, want 403", rr.Code)
	}
}

// TestHandler_RegisterMissingAuth verifies that a missing Authorization header returns 401.
func TestHandler_RegisterMissingAuth(t *testing.T) {
	h := newTestHandler()

	rr := doJSON(t, h, http.MethodPost, "/k2p/claim", ClaimRequest{UDID: "noauth-udid"})
	if rr.Code != http.StatusOK {
		t.Fatalf("claim: got %d, want 200", rr.Code)
	}

	// No auth header.
	regReq := RegisterRequest{UDID: "noauth-udid"}
	rr = doJSON(t, h, http.MethodPost, "/k2p/register", regReq)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("register no auth: got %d, want 401", rr.Code)
	}
}

// TestHandler_LookupNotFound verifies that an unknown UDID returns 404.
func TestHandler_LookupNotFound(t *testing.T) {
	h := newTestHandler()

	rr := doJSON(t, h, http.MethodGet, "/k2p/lookup/ghost-udid", nil)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("lookup not found: got %d, want 404", rr.Code)
	}
}

// TestHandler_ClaimInvalidUDID verifies that claim rejects invalid UDIDs with 400.
func TestHandler_ClaimInvalidUDID(t *testing.T) {
	h := newTestHandler()

	cases := []string{"HOME-ABC", "a", "ab", "home.abc", "home_abc"}
	for _, udid := range cases {
		rr := doJSON(t, h, http.MethodPost, "/k2p/claim", ClaimRequest{UDID: udid})
		if rr.Code != http.StatusBadRequest {
			t.Errorf("claim(%q): got %d, want 400", udid, rr.Code)
		}
	}
}

// TestHandler_RegisterInvalidUDID verifies that register rejects invalid UDIDs with 400.
func TestHandler_RegisterInvalidUDID(t *testing.T) {
	h := newTestHandler()

	// First claim a valid UDID to get a token.
	rr := doJSON(t, h, http.MethodPost, "/k2p/claim", ClaimRequest{UDID: "valid-udid"})
	if rr.Code != http.StatusOK {
		t.Fatalf("claim: got %d, want 200", rr.Code)
	}
	var cr ClaimResponse
	if err := json.NewDecoder(rr.Body).Decode(&cr); err != nil {
		t.Fatalf("claim: decode response: %v", err)
	}

	// Attempt to register with invalid UDIDs.
	invalidUDIDs := []string{"HOME-ABC", "a", "ab", "home.abc", "home_abc"}
	for _, udid := range invalidUDIDs {
		regReq := RegisterRequest{UDID: udid}
		rr = doJSONWithAuth(t, h, http.MethodPost, "/k2p/register", cr.RegToken, regReq)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("register(%q): got %d, want 400", udid, rr.Code)
		}
	}
}


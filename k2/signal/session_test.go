package signal

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// claimAndRegister is a test helper that claims a UDID and registers it.
// Returns the registration token.
func claimAndRegister(t *testing.T, h *Handler, udid string) string {
	t.Helper()

	rr := doJSON(t, h, http.MethodPost, "/k2p/claim", ClaimRequest{UDID: udid})
	if rr.Code != http.StatusOK {
		t.Fatalf("claim %q: got %d, want 200; body: %s", udid, rr.Code, rr.Body)
	}
	var cr ClaimResponse
	if err := json.NewDecoder(rr.Body).Decode(&cr); err != nil {
		t.Fatalf("claim %q: decode: %v", udid, err)
	}

	regReq := RegisterRequest{
		UDID:     udid,
		Networks: []string{"192.168.1.0/24"},
		DNS:      "192.168.1.1",
	}
	rr = doJSONWithAuth(t, h, http.MethodPost, "/k2p/register", cr.RegToken, regReq)
	if rr.Code != http.StatusOK {
		t.Fatalf("register %q: got %d, want 200; body: %s", udid, rr.Code, rr.Body)
	}

	return cr.RegToken
}

// dialWS connects a WS client to the test server with the given query params.
func dialWS(t *testing.T, srvURL, params string) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + srvURL[4:] + "/k2p/ws?" + params
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		body := ""
		if resp != nil && resp.Body != nil {
			b := make([]byte, 512)
			n, _ := resp.Body.Read(b)
			body = string(b[:n])
		}
		t.Fatalf("dial WS %q: %v; body: %s", params, err, body)
	}
	return conn
}

// readWSMessage reads a WSMessage from the WS connection with a timeout.
func readWSMessage(t *testing.T, conn *websocket.Conn, timeout time.Duration) WSMessage {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(timeout)) //nolint:errcheck
	var msg WSMessage
	if err := conn.ReadJSON(&msg); err != nil {
		t.Fatalf("read WS message: %v", err)
	}
	conn.SetReadDeadline(time.Time{}) //nolint:errcheck
	return msg
}

func TestSession_HomeAwayExchange(t *testing.T) {
	h := newTestHandler()
	srv := httptest.NewServer(h)
	defer srv.Close()

	udid := "session-test"
	token := claimAndRegister(t, h, udid)

	// 1. Home connects via WS.
	homeWS := dialWS(t, srv.URL, "udid="+udid+"&token="+token+"&role=home")
	defer homeWS.Close()

	// 2. Away connects via WS — triggers session creation.
	awayWS := dialWS(t, srv.URL, "udid="+udid+"&role=away")
	defer awayWS.Close()

	// 3. Home receives "session-start" from the router.
	homeMsg := readWSMessage(t, homeWS, 2*time.Second)
	if homeMsg.Type != "session-start" {
		t.Fatalf("home: got type %q, want session-start", homeMsg.Type)
	}
	if homeMsg.Session == "" {
		t.Fatal("home: session-start has empty session ID")
	}
	sessionID := homeMsg.Session

	// 4. Away receives "session-start" echo with the session ID.
	awayMsg := readWSMessage(t, awayWS, 2*time.Second)
	if awayMsg.Type != "session-start" {
		t.Fatalf("away: got type %q, want session-start", awayMsg.Type)
	}
	if awayMsg.Session != sessionID {
		t.Fatalf("away: session %q != home session %q", awayMsg.Session, sessionID)
	}

	// 5. Away sends a candidate → Home receives it.
	awayCandidate := WSMessage{
		Type:      "candidate",
		Candidate: "candidate:1 1 udp 2130706431 192.168.1.100 12345 typ host",
	}
	if err := awayWS.WriteJSON(awayCandidate); err != nil {
		t.Fatalf("away write candidate: %v", err)
	}

	homeMsg = readWSMessage(t, homeWS, 2*time.Second)
	if homeMsg.Type != "candidate" {
		t.Fatalf("home: got type %q, want candidate", homeMsg.Type)
	}
	if homeMsg.Candidate != awayCandidate.Candidate {
		t.Fatalf("home: candidate mismatch: got %q, want %q", homeMsg.Candidate, awayCandidate.Candidate)
	}
	if homeMsg.Session != sessionID {
		t.Fatalf("home: candidate session %q != %q", homeMsg.Session, sessionID)
	}

	// 6. Home sends a candidate → Away receives it.
	homeCandidate := WSMessage{
		Type:      "candidate",
		Session:   sessionID,
		Candidate: "candidate:2 1 udp 1694498815 10.0.0.1 54321 typ srflx",
	}
	if err := homeWS.WriteJSON(homeCandidate); err != nil {
		t.Fatalf("home write candidate: %v", err)
	}

	awayMsg = readWSMessage(t, awayWS, 2*time.Second)
	if awayMsg.Type != "candidate" {
		t.Fatalf("away: got type %q, want candidate", awayMsg.Type)
	}
	if awayMsg.Candidate != homeCandidate.Candidate {
		t.Fatalf("away: candidate mismatch: got %q, want %q", awayMsg.Candidate, homeCandidate.Candidate)
	}
}

func TestSession_AwayDisconnectNotifiesHome(t *testing.T) {
	h := newTestHandler()
	srv := httptest.NewServer(h)
	defer srv.Close()

	udid := "disconnect-test"
	token := claimAndRegister(t, h, udid)

	homeWS := dialWS(t, srv.URL, "udid="+udid+"&token="+token+"&role=home")
	defer homeWS.Close()

	awayWS := dialWS(t, srv.URL, "udid="+udid+"&role=away")

	// Drain session-start on both sides.
	readWSMessage(t, homeWS, 2*time.Second)
	readWSMessage(t, awayWS, 2*time.Second)

	// Away disconnects.
	awayWS.Close()

	// Home should receive "session-end".
	homeMsg := readWSMessage(t, homeWS, 2*time.Second)
	if homeMsg.Type != "session-end" {
		t.Fatalf("home: got type %q, want session-end", homeMsg.Type)
	}
}

func TestSession_HomeNotConnected(t *testing.T) {
	h := newTestHandler()
	srv := httptest.NewServer(h)
	defer srv.Close()

	udid := "offline-home"
	claimAndRegister(t, h, udid)
	// Home does NOT connect via WS.

	// Away connects — should get "error" because Home is offline.
	awayWS := dialWS(t, srv.URL, "udid="+udid+"&role=away")
	defer awayWS.Close()

	msg := readWSMessage(t, awayWS, 2*time.Second)
	if msg.Type != "error" {
		t.Fatalf("away: got type %q, want error", msg.Type)
	}
}

func TestSession_BadRoleReturns400(t *testing.T) {
	h := newTestHandler()
	srv := httptest.NewServer(h)
	defer srv.Close()

	// No role parameter.
	req := httptest.NewRequest(http.MethodGet, "/k2p/ws?udid=test", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("no role: got %d, want 400", rr.Code)
	}
}

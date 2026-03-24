package signal

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestRelay_BinaryForward verifies that binary messages are forwarded
// bidirectionally between home and away connections.
func TestRelay_BinaryForward(t *testing.T) {
	relay := NewRelay()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /k2p/relay", relay.HandleRelay)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	wsURL := "ws" + srv.URL[len("http"):]

	// Connect home.
	homeConn, _, err := websocket.DefaultDialer.Dial(wsURL+"/k2p/relay?udid=test-home&role=home", nil)
	if err != nil {
		t.Fatalf("home dial: %v", err)
	}
	defer homeConn.Close()

	// Connect away.
	awayConn, _, err := websocket.DefaultDialer.Dial(wsURL+"/k2p/relay?udid=test-home&role=away", nil)
	if err != nil {
		t.Fatalf("away dial: %v", err)
	}
	defer awayConn.Close()

	// Allow time for the pair to be established and forwarding goroutines to start.
	time.Sleep(100 * time.Millisecond)

	// Away → Home.
	awayMsg := []byte{0x01, 0x02, 0x03, 0xff}
	if err := awayConn.WriteMessage(websocket.BinaryMessage, awayMsg); err != nil {
		t.Fatalf("away write: %v", err)
	}

	homeConn.SetReadDeadline(time.Now().Add(2 * time.Second)) //nolint:errcheck
	msgType, got, err := homeConn.ReadMessage()
	if err != nil {
		t.Fatalf("home read: %v", err)
	}
	if msgType != websocket.BinaryMessage {
		t.Errorf("home: expected BinaryMessage, got %d", msgType)
	}
	if string(got) != string(awayMsg) {
		t.Errorf("home: got %v, want %v", got, awayMsg)
	}

	// Home → Away.
	homeMsg := []byte{0xaa, 0xbb}
	if err := homeConn.WriteMessage(websocket.BinaryMessage, homeMsg); err != nil {
		t.Fatalf("home write: %v", err)
	}

	awayConn.SetReadDeadline(time.Now().Add(2 * time.Second)) //nolint:errcheck
	msgType, got, err = awayConn.ReadMessage()
	if err != nil {
		t.Fatalf("away read: %v", err)
	}
	if msgType != websocket.BinaryMessage {
		t.Errorf("away: expected BinaryMessage, got %d", msgType)
	}
	if string(got) != string(homeMsg) {
		t.Errorf("away: got %v, want %v", got, homeMsg)
	}
}

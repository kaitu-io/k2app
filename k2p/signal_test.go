package k2p

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestSignalClient_RegisterAndLookup(t *testing.T) {
	var registered PeerInfo
	mux := http.NewServeMux()
	mux.HandleFunc("POST /k2p/register", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, "unauthorized", 401)
			return
		}
		var req RegisterRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		registered = PeerInfo{
			UDID:     req.UDID,
			Networks: req.Networks,
			DNS:      req.DNS,
			Online:   true,
		}
		w.WriteHeader(200)
	})
	mux.HandleFunc("GET /k2p/lookup/{udid}", func(w http.ResponseWriter, r *http.Request) {
		udid := r.PathValue("udid")
		if registered.UDID != udid {
			http.Error(w, "not found", 404)
			return
		}
		json.NewEncoder(w).Encode(registered)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx := context.Background()
	client := NewSignalClient(srv.URL, "test-token")

	err := client.Register(ctx, RegisterRequest{
		UDID:     "home-001",
		Networks: []string{"192.168.1.0/24"},
		DNS:      "192.168.1.1",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	info, err := client.Lookup(ctx, "home-001")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if len(info.Networks) != 1 || info.Networks[0] != "192.168.1.0/24" {
		t.Errorf("expected networks [192.168.1.0/24], got %v", info.Networks)
	}
	if info.DNS != "192.168.1.1" {
		t.Errorf("expected dns 192.168.1.1, got %s", info.DNS)
	}
}

func TestSignalClient_LookupNotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /k2p/lookup/{udid}", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", 404)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := NewSignalClient(srv.URL, "token")
	_, err := client.Lookup(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent peer")
	}
}

func TestSignalClient_Claim(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /k2p/claim", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			UDID string `json:"udid"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if body.UDID == "already-claimed" {
			http.Error(w, "conflict", 409)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"reg_token": "tok-abc123"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx := context.Background()
	client := NewSignalClient(srv.URL, "test-token")

	token, err := client.Claim(ctx, "home-001")
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if token != "tok-abc123" {
		t.Errorf("expected tok-abc123, got %s", token)
	}

	_, err = client.Claim(ctx, "already-claimed")
	if err == nil {
		t.Fatal("expected error for already claimed UDID")
	}
	if !strings.Contains(err.Error(), "already claimed") {
		t.Errorf("expected 'already claimed' in error, got: %v", err)
	}
}

func TestSignalClient_LookupWithTURN(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /k2p/lookup/{udid}", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(PeerInfo{
			UDID:     r.PathValue("udid"),
			Networks: []string{"10.0.0.0/8"},
			DNS:      "10.0.0.1",
			Online:   true,
			TURN: &TURNCredentials{
				URLs:     []string{"turn:turn.example.com:3478"},
				Username: "user123",
				Password: "pass456",
				TTL:      3600,
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := NewSignalClient(srv.URL, "token")
	info, err := client.Lookup(context.Background(), "home-001")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if info.TURN == nil {
		t.Fatal("expected TURN credentials")
	}
	if info.TURN.Username != "user123" {
		t.Errorf("expected username user123, got %s", info.TURN.Username)
	}
	if info.TURN.TTL != 3600 {
		t.Errorf("expected TTL 3600, got %d", info.TURN.TTL)
	}
}

// testWSServer creates a minimal WS server that simulates signal server behavior
// for ConnectHome and ListenHome testing.
func testWSServer(t *testing.T) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

	mux := http.NewServeMux()
	mux.HandleFunc("/k2p/ws", func(w http.ResponseWriter, r *http.Request) {
		role := r.URL.Query().Get("role")
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("WS upgrade: %v", err)
			return
		}
		defer conn.Close()

		switch role {
		case "away":
			// Send session-start, then relay messages.
			conn.WriteJSON(WSMessage{Type: "session-start", Session: "test-session-1"})
			// Echo back messages (simulate relay).
			for {
				var msg WSMessage
				if err := conn.ReadJSON(&msg); err != nil {
					return
				}
				// Echo back as-is (simulating relay from home).
				conn.WriteJSON(msg)
			}

		case "home":
			// Send session-start for a new session, then relay messages.
			conn.WriteJSON(WSMessage{Type: "session-start", Session: "test-session-1"})
			// Read and echo messages.
			for {
				var msg WSMessage
				if err := conn.ReadJSON(&msg); err != nil {
					return
				}
				conn.WriteJSON(msg)
			}
		}
	})
	return httptest.NewServer(mux)
}

func TestSignalClient_ConnectHome(t *testing.T) {
	srv := testWSServer(t)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client := NewSignalClient(srv.URL, "test-token")
	exchange, err := client.ConnectHome(ctx, "home-001")
	if err != nil {
		t.Fatalf("ConnectHome: %v", err)
	}
	defer exchange.Close()

	if exchange.Session() != "test-session-1" {
		t.Errorf("expected session test-session-1, got %s", exchange.Session())
	}

	// Send a candidate and verify we get the echo back.
	if err := exchange.SendCandidate("candidate:1 udp 2130706431 192.168.1.1 50000 typ host"); err != nil {
		t.Fatalf("SendCandidate: %v", err)
	}

	select {
	case msg := <-exchange.Incoming():
		if msg.Type != "candidate" {
			t.Errorf("expected type candidate, got %s", msg.Type)
		}
		if msg.Candidate != "candidate:1 udp 2130706431 192.168.1.1 50000 typ host" {
			t.Errorf("unexpected candidate: %s", msg.Candidate)
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for echoed candidate")
	}
}

func TestSignalClient_ConnectHome_SendCredentials(t *testing.T) {
	srv := testWSServer(t)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client := NewSignalClient(srv.URL, "test-token")
	exchange, err := client.ConnectHome(ctx, "home-001")
	if err != nil {
		t.Fatalf("ConnectHome: %v", err)
	}
	defer exchange.Close()

	// Send credentials and verify echo.
	if err := exchange.SendCredentials("ufrag123", "pwd456"); err != nil {
		t.Fatalf("SendCredentials: %v", err)
	}

	// The echo should arrive on both credsCh and inCh.
	ufrag, pwd, err := waitForCredentials(ctx, exchange)
	if err != nil {
		t.Fatalf("waitForCredentials: %v", err)
	}
	if ufrag != "ufrag123" || pwd != "pwd456" {
		t.Errorf("got ufrag=%q pwd=%q, want ufrag123/pwd456", ufrag, pwd)
	}
}

func TestSignalClient_ListenHome(t *testing.T) {
	srv := testWSServer(t)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client := NewSignalClient(srv.URL, "test-token")
	ch, err := client.ListenHome(ctx, "home-001")
	if err != nil {
		t.Fatalf("ListenHome: %v", err)
	}

	// Should receive a CandidateExchange for the session.
	select {
	case exchange := <-ch:
		if exchange == nil {
			t.Fatal("received nil exchange")
		}
		if exchange.Session() != "test-session-1" {
			t.Errorf("expected session test-session-1, got %s", exchange.Session())
		}
		if exchange.ownsConn {
			t.Error("Home-side exchange should not own the connection")
		}
		// Send credentials through the exchange.
		if err := exchange.SendCredentials("home-ufrag", "home-pwd"); err != nil {
			t.Fatalf("SendCredentials: %v", err)
		}
		// Should get echo back.
		select {
		case msg := <-exchange.Incoming():
			if msg.Type != "credentials" {
				t.Errorf("expected credentials, got %s", msg.Type)
			}
			if msg.Ufrag != "home-ufrag" {
				t.Errorf("expected home-ufrag, got %s", msg.Ufrag)
			}
		case <-ctx.Done():
			t.Fatal("timeout waiting for echoed credentials")
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for exchange from ListenHome")
	}
}

func TestWaitForCredentials_ContextCancel(t *testing.T) {
	exchange := &CandidateExchange{
		credsCh: make(chan WSMessage, 1),
		done:    make(chan struct{}),
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, _, err := waitForCredentials(ctx, exchange)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

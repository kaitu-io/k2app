package k2p2p

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
	mux.HandleFunc("POST /p2p/register", func(w http.ResponseWriter, r *http.Request) {
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
			UDID:      req.UDID,
			Addr:      req.Addr,
			NATType:   req.NATType,
			UpdatedAt: time.Now(),
		}
		w.WriteHeader(200)
	})
	mux.HandleFunc("GET /p2p/lookup/{udid}", func(w http.ResponseWriter, r *http.Request) {
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
		UDID:    "home-001",
		Addr:    "1.2.3.4:5678",
		NATType: "easy",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	info, err := client.Lookup(ctx, "home-001")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if info.Addr != "1.2.3.4:5678" {
		t.Errorf("expected addr 1.2.3.4:5678, got %s", info.Addr)
	}
}

func TestSignalClient_LookupNotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /p2p/lookup/{udid}", func(w http.ResponseWriter, r *http.Request) {
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

func TestSignalClient_ConnectRequest(t *testing.T) {
	var received ConnectRequest
	mux := http.NewServeMux()
	mux.HandleFunc("POST /p2p/connect", func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		w.WriteHeader(200)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := NewSignalClient(srv.URL, "test-token")
	err := client.RequestConnect(context.Background(), ConnectRequest{
		HomeUDID: "home-001",
		AwayAddr: "5.6.7.8:9012",
		NATType:  "easy",
	})
	if err != nil {
		t.Fatalf("RequestConnect: %v", err)
	}
	if received.HomeUDID != "home-001" {
		t.Errorf("expected home-001, got %s", received.HomeUDID)
	}
	if received.AwayAddr != "5.6.7.8:9012" {
		t.Errorf("expected 5.6.7.8:9012, got %s", received.AwayAddr)
	}
}

func TestSignalClient_ListenNotify(t *testing.T) {
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		time.Sleep(100 * time.Millisecond)
		msg, _ := json.Marshal(PunchNotify{AwayAddr: "5.6.7.8:9012", NATType: "easy"})
		conn.WriteMessage(websocket.TextMessage, msg)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	client := NewSignalClient(wsURL, "test-token")

	notifyCh, err := client.ListenNotify(ctx, "home-001")
	if err != nil {
		t.Fatalf("ListenNotify: %v", err)
	}

	select {
	case notify := <-notifyCh:
		if notify.AwayAddr != "5.6.7.8:9012" {
			t.Errorf("expected 5.6.7.8:9012, got %s", notify.AwayAddr)
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for notification")
	}
}

package k2p

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
			UDID:      req.UDID,
			Addr:      req.Addr,
			NATType:   req.NATType,
			Networks:  req.Networks,
			DNS:       req.DNS,
			Online:    true,
			UpdatedAt: time.Now(),
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
		Addr:     "1.2.3.4:5678",
		NATType:  "easy",
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
	if info.Addr != "1.2.3.4:5678" {
		t.Errorf("expected addr 1.2.3.4:5678, got %s", info.Addr)
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

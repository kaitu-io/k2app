package config

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchControlKeyHash(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"tunnels":[{"url":"k2v5://h.example.com:443/"}],"refresh":600,"control_key_hash":"aabbccdd"}`)
	}))
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "https://")
	sub, err := NewSubscription("k2subs://u:p@"+host+"/api/subs", "")
	if err != nil {
		t.Fatalf("NewSubscription: %v", err)
	}
	var cbGot string
	sub.SetOnControlKeyHash(func(h string) { cbGot = h })

	if err := sub.Fetch(context.Background(), srv.Client()); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got := sub.ControlKeyHash(); got != "aabbccdd" {
		t.Fatalf("ControlKeyHash = %q, want aabbccdd", got)
	}
	if cbGot != "aabbccdd" {
		t.Fatalf("callback got %q, want aabbccdd", cbGot)
	}
}

func TestFetchControlKeyHashAbsent(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"tunnels":[{"url":"k2v5://h.example.com:443/"}]}`)
	}))
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "https://")
	sub, err := NewSubscription("k2subs://u:p@"+host+"/api/subs", "")
	if err != nil {
		t.Fatalf("NewSubscription: %v", err)
	}
	called := false
	sub.SetOnControlKeyHash(func(string) { called = true })
	if err := sub.Fetch(context.Background(), srv.Client()); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if sub.ControlKeyHash() != "" || called {
		t.Fatalf("absent hash must not set state or fire callback")
	}
}

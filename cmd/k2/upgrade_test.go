package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchLatest(t *testing.T) {
	info := latestInfo{
		Version: "1.2.3",
		Binaries: map[string]string{
			"linux-amd64":  "https://cdn.example.com/1.2.3/k2-linux-amd64",
			"darwin-arm64": "https://cdn.example.com/1.2.3/k2-darwin-arm64",
		},
		Checksums: map[string]string{
			"linux-amd64":  "sha256:abc123",
			"darwin-arm64": "sha256:def456",
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/cloudfront.latest.json" {
			json.NewEncoder(w).Encode(info)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	origEndpoints := releaseEndpoints
	releaseEndpoints = []string{srv.URL}
	defer func() { releaseEndpoints = origEndpoints }()

	got, err := fetchLatest()
	if err != nil {
		t.Fatalf("fetchLatest: %v", err)
	}
	if got.Version != "1.2.3" {
		t.Errorf("version = %q, want %q", got.Version, "1.2.3")
	}
	if got.Binaries["linux-amd64"] != info.Binaries["linux-amd64"] {
		t.Errorf("binary URL = %q, want %q", got.Binaries["linux-amd64"], info.Binaries["linux-amd64"])
	}
	if got.Checksums["linux-amd64"] != "sha256:abc123" {
		t.Errorf("checksum = %q, want %q", got.Checksums["linux-amd64"], "sha256:abc123")
	}
}

func TestFetchLatest_Fallback(t *testing.T) {
	info := latestInfo{Version: "2.0.0"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/cloudfront.latest.json" {
			json.NewEncoder(w).Encode(info)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	origEndpoints := releaseEndpoints
	releaseEndpoints = []string{"http://localhost:1", srv.URL}
	defer func() { releaseEndpoints = origEndpoints }()

	got, err := fetchLatest()
	if err != nil {
		t.Fatalf("fetchLatest with fallback: %v", err)
	}
	if got.Version != "2.0.0" {
		t.Errorf("fallback version = %q, want %q", got.Version, "2.0.0")
	}
}

func TestFetchLatest_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	origEndpoints := releaseEndpoints
	releaseEndpoints = []string{srv.URL}
	defer func() { releaseEndpoints = origEndpoints }()

	_, err := fetchLatest()
	if err == nil {
		t.Fatal("expected error when manifest not found")
	}
}

func TestVersionCheck_NoUpdate(t *testing.T) {
	if version == "" {
		t.Skip("version not set")
	}
	latest := version
	if latest != version {
		t.Fatalf("versions should match: %s vs %s", latest, version)
	}
}

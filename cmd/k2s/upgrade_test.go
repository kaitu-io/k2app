package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManifestPath(t *testing.T) {
	tests := []struct {
		channel string
		want    string
	}{
		{"stable", "/k2s-cloudfront.latest.json"},
		{"beta", "/beta/k2s-cloudfront.latest.json"},
	}
	for _, tt := range tests {
		got := manifestPath(tt.channel)
		if got != tt.want {
			t.Errorf("manifestPath(%q) = %q, want %q", tt.channel, got, tt.want)
		}
	}
}

func TestReadWriteChannel(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "update-channel")

	ch := readChannelFromPath(path)
	if ch != "stable" {
		t.Errorf("readChannelFromPath (missing) = %q, want %q", ch, "stable")
	}

	if err := saveChannelToPath(path, "beta"); err != nil {
		t.Fatalf("saveChannelToPath: %v", err)
	}
	ch = readChannelFromPath(path)
	if ch != "beta" {
		t.Errorf("readChannelFromPath (beta) = %q, want %q", ch, "beta")
	}

	if err := saveChannelToPath(path, "stable"); err != nil {
		t.Fatalf("saveChannelToPath: %v", err)
	}
	ch = readChannelFromPath(path)
	if ch != "stable" {
		t.Errorf("readChannelFromPath (stable) = %q, want %q", ch, "stable")
	}

	os.WriteFile(path, []byte("invalid\n"), 0644)
	ch = readChannelFromPath(path)
	if ch != "stable" {
		t.Errorf("readChannelFromPath (invalid) = %q, want %q", ch, "stable")
	}

	os.WriteFile(path, []byte("  beta  \n"), 0644)
	ch = readChannelFromPath(path)
	if ch != "beta" {
		t.Errorf("readChannelFromPath (whitespace beta) = %q, want %q", ch, "beta")
	}
}

func readChannelFromPath(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return "stable"
	}
	ch := strings.TrimSpace(string(data))
	if ch == "beta" {
		return "beta"
	}
	return "stable"
}

func saveChannelToPath(path, ch string) error {
	if dir := filepath.Dir(path); dir != "" {
		os.MkdirAll(dir, 0755)
	}
	return os.WriteFile(path, []byte(ch), 0644)
}

func TestFetchLatest(t *testing.T) {
	stableInfo := latestInfo{
		Version: "1.2.3",
		Binaries: map[string]string{
			"linux-amd64": "https://cdn.example.com/1.2.3/k2s-linux-amd64",
		},
		Checksums: map[string]string{
			"linux-amd64": "sha256:abc123def456",
		},
	}
	betaInfo := latestInfo{
		Version: "1.3.0-beta.1",
		Binaries: map[string]string{
			"linux-amd64": "https://cdn.example.com/1.3.0-beta.1/k2s-linux-amd64",
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/k2s-cloudfront.latest.json":
			json.NewEncoder(w).Encode(stableInfo)
		case "/beta/k2s-cloudfront.latest.json":
			json.NewEncoder(w).Encode(betaInfo)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	origEndpoints := releaseEndpoints
	releaseEndpoints = []string{srv.URL}
	defer func() { releaseEndpoints = origEndpoints }()

	got, err := fetchLatest("stable")
	if err != nil {
		t.Fatalf("fetchLatest(stable): %v", err)
	}
	if got.Version != "1.2.3" {
		t.Errorf("stable version = %q, want %q", got.Version, "1.2.3")
	}
	if got.Checksums["linux-amd64"] != "sha256:abc123def456" {
		t.Errorf("checksum = %q, want %q", got.Checksums["linux-amd64"], "sha256:abc123def456")
	}

	got, err = fetchLatest("beta")
	if err != nil {
		t.Fatalf("fetchLatest(beta): %v", err)
	}
	if got.Version != "1.3.0-beta.1" {
		t.Errorf("beta version = %q, want %q", got.Version, "1.3.0-beta.1")
	}

	// Fallback when first endpoint fails
	releaseEndpoints = []string{"http://localhost:1", srv.URL}
	got, err = fetchLatest("stable")
	if err != nil {
		t.Fatalf("fetchLatest with fallback: %v", err)
	}
	if got.Version != "1.2.3" {
		t.Errorf("fallback version = %q, want %q", got.Version, "1.2.3")
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

	_, err := fetchLatest("stable")
	if err == nil {
		t.Fatal("expected error when manifest not found")
	}
}

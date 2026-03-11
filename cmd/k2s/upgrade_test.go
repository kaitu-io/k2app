package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLatestPath(t *testing.T) {
	tests := []struct {
		channel string
		want    string
	}{
		{"stable", "/LATEST"},
		{"beta", "/beta/LATEST"},
	}
	for _, tt := range tests {
		got := latestPath(tt.channel)
		if got != tt.want {
			t.Errorf("latestPath(%q) = %q, want %q", tt.channel, got, tt.want)
		}
	}
}

func TestReadWriteChannel(t *testing.T) {
	tmpDir := t.TempDir()

	path := filepath.Join(tmpDir, "update-channel")

	// Missing file defaults to "stable"
	ch := readChannelFromPath(path)
	if ch != "stable" {
		t.Errorf("readChannelFromPath (missing) = %q, want %q", ch, "stable")
	}

	// Write "beta", read back "beta"
	if err := saveChannelToPath(path, "beta"); err != nil {
		t.Fatalf("saveChannelToPath: %v", err)
	}
	ch = readChannelFromPath(path)
	if ch != "beta" {
		t.Errorf("readChannelFromPath (beta) = %q, want %q", ch, "beta")
	}

	// Write "stable", read back "stable"
	if err := saveChannelToPath(path, "stable"); err != nil {
		t.Fatalf("saveChannelToPath: %v", err)
	}
	ch = readChannelFromPath(path)
	if ch != "stable" {
		t.Errorf("readChannelFromPath (stable) = %q, want %q", ch, "stable")
	}

	// Invalid value defaults to "stable"
	os.WriteFile(path, []byte("invalid\n"), 0644)
	ch = readChannelFromPath(path)
	if ch != "stable" {
		t.Errorf("readChannelFromPath (invalid) = %q, want %q", ch, "stable")
	}

	// Whitespace trimmed
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

func TestFetchLatestVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/LATEST":
			fmt.Fprint(w, "1.2.3\n")
		case "/beta/LATEST":
			fmt.Fprint(w, "1.3.0-beta.1\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	origEndpoints := releaseEndpoints
	releaseEndpoints = []string{srv.URL}
	defer func() { releaseEndpoints = origEndpoints }()

	// Stable channel
	v, err := fetchLatestVersion("stable")
	if err != nil {
		t.Fatalf("fetchLatestVersion(stable): %v", err)
	}
	if v != "1.2.3" {
		t.Errorf("stable version = %q, want %q", v, "1.2.3")
	}

	// Beta channel
	v, err = fetchLatestVersion("beta")
	if err != nil {
		t.Fatalf("fetchLatestVersion(beta): %v", err)
	}
	if v != "1.3.0-beta.1" {
		t.Errorf("beta version = %q, want %q", v, "1.3.0-beta.1")
	}

	// Fallback when first endpoint fails
	releaseEndpoints = []string{"http://localhost:1", srv.URL}
	v, err = fetchLatestVersion("stable")
	if err != nil {
		t.Fatalf("fetchLatestVersion with fallback: %v", err)
	}
	if v != "1.2.3" {
		t.Errorf("fallback version = %q, want %q", v, "1.2.3")
	}
}

func TestParseChecksum(t *testing.T) {
	checksums := "abc123def456  k2s-linux-amd64\nfed654cba321  k2s-darwin-arm64\n"

	hash, err := parseChecksum(strings.NewReader(checksums), "k2s-linux-amd64")
	if err != nil {
		t.Fatalf("parseChecksum: %v", err)
	}
	if hash != "abc123def456" {
		t.Errorf("hash = %q, want %q", hash, "abc123def456")
	}

	hash, err = parseChecksum(strings.NewReader(checksums), "k2s-darwin-arm64")
	if err != nil {
		t.Fatalf("parseChecksum: %v", err)
	}
	if hash != "fed654cba321" {
		t.Errorf("hash = %q, want %q", hash, "fed654cba321")
	}

	_, err = parseChecksum(strings.NewReader(checksums), "k2s-windows-amd64")
	if err == nil {
		t.Error("expected error for missing binary")
	}
}

func TestFetchChecksum(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/1.2.3/checksums.txt" {
			fmt.Fprint(w, "abc123  k2s-linux-amd64\ndef456  k2s-darwin-arm64\n")
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	origEndpoints := releaseEndpoints
	releaseEndpoints = []string{srv.URL}
	defer func() { releaseEndpoints = origEndpoints }()

	hash, err := fetchChecksum("1.2.3", "k2s-linux-amd64")
	if err != nil {
		t.Fatalf("fetchChecksum: %v", err)
	}
	if hash != "abc123" {
		t.Errorf("hash = %q, want %q", hash, "abc123")
	}
}

func TestFetchLatestVersion_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	origEndpoints := releaseEndpoints
	releaseEndpoints = []string{srv.URL}
	defer func() { releaseEndpoints = origEndpoints }()

	_, err := fetchLatestVersion("stable")
	if err == nil {
		t.Fatal("expected error when LATEST not found")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error should mention 404, got: %v", err)
	}
}

func TestDownloadURL(t *testing.T) {
	url := downloadURL("1.2.3", "k2s-linux-amd64")
	want := releaseEndpoints[0] + "/1.2.3/k2s-linux-amd64"
	if url != want {
		t.Errorf("downloadURL = %q, want %q", url, want)
	}
}

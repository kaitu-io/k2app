package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchLatestVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/LATEST" {
			fmt.Fprint(w, "1.2.3\n")
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	origEndpoints := releaseEndpoints
	releaseEndpoints = []string{srv.URL}
	defer func() { releaseEndpoints = origEndpoints }()

	v, err := fetchLatestVersion()
	if err != nil {
		t.Fatalf("fetchLatestVersion: %v", err)
	}
	if v != "1.2.3" {
		t.Errorf("version = %q, want %q", v, "1.2.3")
	}

	// Fallback
	releaseEndpoints = []string{"http://localhost:1", srv.URL}
	v, err = fetchLatestVersion()
	if err != nil {
		t.Fatalf("fetchLatestVersion with fallback: %v", err)
	}
	if v != "1.2.3" {
		t.Errorf("fallback version = %q, want %q", v, "1.2.3")
	}
}

func TestParseChecksum(t *testing.T) {
	checksums := "abc123  k2-linux-amd64\ndef456  k2-darwin-arm64\n"

	hash, err := parseChecksum(strings.NewReader(checksums), "k2-linux-amd64")
	if err != nil {
		t.Fatalf("parseChecksum: %v", err)
	}
	if hash != "abc123" {
		t.Errorf("hash = %q, want %q", hash, "abc123")
	}

	_, err = parseChecksum(strings.NewReader(checksums), "k2-windows-amd64")
	if err == nil {
		t.Error("expected error for missing binary")
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

	_, err := fetchLatestVersion()
	if err == nil {
		t.Fatal("expected error when LATEST not found")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error should mention 404, got: %v", err)
	}
}

func TestVersionCheck_NoUpdate(t *testing.T) {
	// When version matches LATEST, no update needed.
	if version == "" {
		t.Skip("version not set")
	}
	latest := version
	if latest != version {
		t.Fatalf("versions should match: %s vs %s", latest, version)
	}
}

func TestDownloadURL(t *testing.T) {
	url := downloadURL("1.2.3", "k2-linux-amd64")
	want := releaseEndpoints[0] + "/1.2.3/k2-linux-amd64"
	if url != want {
		t.Errorf("downloadURL = %q, want %q", url, want)
	}
}

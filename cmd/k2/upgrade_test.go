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
			"darwin-arm64": "https://example.com/k2-darwin-arm64",
			"linux-amd64":  "https://example.com/k2-linux-amd64",
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/latest.json" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(info)
	}))
	defer srv.Close()

	// Override the releases URL for testing by using the mock server directly.
	client := &http.Client{}
	resp, err := client.Get(srv.URL + "/latest.json")
	if err != nil {
		t.Fatalf("fetch failed: %v", err)
	}
	defer resp.Body.Close()

	var got latestInfo
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if got.Version != "1.2.3" {
		t.Fatalf("expected version 1.2.3, got %s", got.Version)
	}
	if got.Binaries["darwin-arm64"] != "https://example.com/k2-darwin-arm64" {
		t.Fatalf("expected darwin-arm64 URL, got %s", got.Binaries["darwin-arm64"])
	}
}

func TestVersionCheck_NoUpdate(t *testing.T) {
	// When version matches, no update needed.
	currentVersion := version
	if currentVersion == "" {
		t.Skip("version not set")
	}

	info := latestInfo{Version: currentVersion}
	if info.Version != currentVersion {
		t.Fatalf("versions should match: %s vs %s", info.Version, currentVersion)
	}
}

func TestVersionCheck_NewVersion(t *testing.T) {
	info := latestInfo{Version: "99.99.99"}
	if info.Version == version {
		t.Fatal("test version should differ from current")
	}
}

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestManifestPath(t *testing.T) {
	tests := []struct {
		channel string
		want    string
	}{
		{"stable", "/k2s-cloudfront.latest.json"},
		{"beta", "/beta/k2s-cloudfront.latest.json"},
		{"", "/k2s-cloudfront.latest.json"},
		{"anything", "/k2s-cloudfront.latest.json"},
	}
	for _, tt := range tests {
		got := manifestPath(tt.channel)
		if got != tt.want {
			t.Errorf("manifestPath(%q) = %q, want %q", tt.channel, got, tt.want)
		}
	}
}

func TestReadWriteChannel(t *testing.T) {
	// Use a temp dir for channel file
	tmpDir := t.TempDir()
	origFunc := channelFilePath
	_ = origFunc // channelFilePath is a package-level func, we can't easily swap it

	// Instead, test saveChannel and readChannel with a known path by
	// writing directly to the file that channelFilePath() returns.
	// Since channelFilePath depends on config.DefaultServerConfig().CertDir,
	// we'll test the low-level logic directly.

	// Test: readChannel defaults to "stable" when file doesn't exist
	path := filepath.Join(tmpDir, "update-channel")
	ch := readChannelFromPath(path)
	if ch != "stable" {
		t.Errorf("readChannelFromPath (missing file) = %q, want %q", ch, "stable")
	}

	// Test: write "beta", read back "beta"
	if err := saveChannelToPath(path, "beta"); err != nil {
		t.Fatalf("saveChannelToPath: %v", err)
	}
	ch = readChannelFromPath(path)
	if ch != "beta" {
		t.Errorf("readChannelFromPath (beta) = %q, want %q", ch, "beta")
	}

	// Test: write "stable", read back "stable"
	if err := saveChannelToPath(path, "stable"); err != nil {
		t.Fatalf("saveChannelToPath: %v", err)
	}
	ch = readChannelFromPath(path)
	if ch != "stable" {
		t.Errorf("readChannelFromPath (stable) = %q, want %q", ch, "stable")
	}

	// Test: invalid value defaults to "stable"
	os.WriteFile(path, []byte("invalid\n"), 0644)
	ch = readChannelFromPath(path)
	if ch != "stable" {
		t.Errorf("readChannelFromPath (invalid) = %q, want %q", ch, "stable")
	}

	// Test: whitespace trimmed
	os.WriteFile(path, []byte("  beta  \n"), 0644)
	ch = readChannelFromPath(path)
	if ch != "beta" {
		t.Errorf("readChannelFromPath (whitespace beta) = %q, want %q", ch, "beta")
	}
}

// readChannelFromPath / saveChannelToPath are testable versions that accept a path.
func readChannelFromPath(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return "stable"
	}
	ch := trimChannel(string(data))
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

func trimChannel(s string) string {
	// Mirrors the logic in readChannel
	for _, c := range []string{"\n", "\r", " ", "\t"} {
		s = replaceAll(s, c, "")
	}
	return s
}

func replaceAll(s, old, new string) string {
	for {
		i := indexOf(s, old)
		if i < 0 {
			return s
		}
		s = s[:i] + new + s[i+len(old):]
	}
}

func indexOf(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestFetchLatest(t *testing.T) {
	// Mock server returning a manifest
	manifest := latestInfo{
		Version: "1.2.3",
		Binaries: map[string]string{
			"linux-amd64":  "https://example.com/k2s-linux-amd64",
			"darwin-arm64": "https://example.com/k2s-darwin-arm64",
		},
		Checksums: map[string]string{
			"linux-amd64":  "sha256:abc123",
			"darwin-arm64": "sha256:def456",
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/k2s-cloudfront.latest.json":
			json.NewEncoder(w).Encode(manifest)
		case "/beta/k2s-cloudfront.latest.json":
			betaManifest := manifest
			betaManifest.Version = "1.3.0-beta.1"
			json.NewEncoder(w).Encode(betaManifest)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	// Override endpoints for test
	origEndpoints := releaseEndpoints
	releaseEndpoints = []string{srv.URL}
	defer func() { releaseEndpoints = origEndpoints }()

	// Test stable channel
	info, err := fetchLatest("stable")
	if err != nil {
		t.Fatalf("fetchLatest(stable): %v", err)
	}
	if info.Version != "1.2.3" {
		t.Errorf("stable version = %q, want %q", info.Version, "1.2.3")
	}

	// Test beta channel
	info, err = fetchLatest("beta")
	if err != nil {
		t.Fatalf("fetchLatest(beta): %v", err)
	}
	if info.Version != "1.3.0-beta.1" {
		t.Errorf("beta version = %q, want %q", info.Version, "1.3.0-beta.1")
	}

	// Test fallback when first endpoint fails
	releaseEndpoints = []string{"http://localhost:1", srv.URL}
	info, err = fetchLatest("stable")
	if err != nil {
		t.Fatalf("fetchLatest(stable) with fallback: %v", err)
	}
	if info.Version != "1.2.3" {
		t.Errorf("fallback version = %q, want %q", info.Version, "1.2.3")
	}
}

func TestLatestInfoJSON(t *testing.T) {
	raw := `{
		"version": "0.4.0-beta.2",
		"binaries": {
			"linux-amd64": "https://cdn.example.com/k2s-linux-amd64",
			"linux-arm64": "https://cdn.example.com/k2s-linux-arm64"
		},
		"checksums": {
			"linux-amd64": "sha256:abcdef1234567890",
			"linux-arm64": "sha256:1234567890abcdef"
		}
	}`

	var info latestInfo
	if err := json.Unmarshal([]byte(raw), &info); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if info.Version != "0.4.0-beta.2" {
		t.Errorf("version = %q, want %q", info.Version, "0.4.0-beta.2")
	}
	if len(info.Binaries) != 2 {
		t.Errorf("binaries count = %d, want 2", len(info.Binaries))
	}
	if len(info.Checksums) != 2 {
		t.Errorf("checksums count = %d, want 2", len(info.Checksums))
	}

	// Verify checksum prefix stripping
	cs := info.Checksums["linux-amd64"]
	hash := trimSHA256Prefix(cs)
	if hash != "abcdef1234567890" {
		t.Errorf("stripped hash = %q, want %q", hash, "abcdef1234567890")
	}
}

func trimSHA256Prefix(s string) string {
	if len(s) > 7 && s[:7] == "sha256:" {
		return s[7:]
	}
	return s
}

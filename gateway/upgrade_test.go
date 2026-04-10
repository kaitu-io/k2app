//go:build linux

package gateway

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestFetchLatestK2r(t *testing.T) {
	const wantVersion = "0.4.2"
	checksumsBody := "abc123def456  k2r-linux-arm64\ndeadbeef1234  k2r-linux-amd64\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/LATEST":
			fmt.Fprintln(w, wantVersion)
		case "/" + wantVersion + "/checksums.txt":
			fmt.Fprint(w, checksumsBody)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	// Override endpoints.
	orig := K2rReleaseEndpoints
	K2rReleaseEndpoints = []string{srv.URL}
	defer func() { K2rReleaseEndpoints = orig }()

	info, err := FetchLatestK2r()
	if err != nil {
		t.Fatalf("FetchLatestK2r: %v", err)
	}

	if info.Version != wantVersion {
		t.Errorf("version: got %q, want %q", info.Version, wantVersion)
	}
	if h, ok := info.Checksums["k2r-linux-arm64"]; !ok || h != "abc123def456" {
		t.Errorf("checksum arm64: got %q, want %q", h, "abc123def456")
	}
	if h, ok := info.Checksums["k2r-linux-amd64"]; !ok || h != "deadbeef1234" {
		t.Errorf("checksum amd64: got %q, want %q", h, "deadbeef1234")
	}
}

func TestFetchLatestK2rFallback(t *testing.T) {
	const wantVersion = "0.4.3"
	checksumsBody := "aabbccdd1122  k2r-linux-amd64\n"

	// First endpoint always fails; second succeeds.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/LATEST":
			fmt.Fprintln(w, wantVersion)
		case "/" + wantVersion + "/checksums.txt":
			fmt.Fprint(w, checksumsBody)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	orig := K2rReleaseEndpoints
	K2rReleaseEndpoints = []string{"http://127.0.0.1:1", srv.URL}
	defer func() { K2rReleaseEndpoints = orig }()

	info, err := FetchLatestK2r()
	if err != nil {
		t.Fatalf("FetchLatestK2r fallback: %v", err)
	}
	if info.Version != wantVersion {
		t.Errorf("version: got %q, want %q", info.Version, wantVersion)
	}
}

func TestDownloadAndReplace(t *testing.T) {
	content := []byte("fake k2r binary content")
	sum := sha256.Sum256(content)
	expectedHash := hex.EncodeToString(sum[:])

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(content)
	}))
	defer srv.Close()

	dir := t.TempDir()
	targetPath := filepath.Join(dir, "k2r")

	// Create a placeholder "existing" binary.
	if err := os.WriteFile(targetPath, []byte("old binary"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := DownloadAndReplace(srv.URL+"/k2r-linux-amd64", expectedHash, targetPath); err != nil {
		t.Fatalf("DownloadAndReplace: %v", err)
	}

	got, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("file content mismatch: got %q, want %q", got, content)
	}

	fi, err := os.Stat(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&0111 == 0 {
		t.Error("replaced binary lost execute permission")
	}
}

func TestDownloadAndReplaceChecksumMismatch(t *testing.T) {
	content := []byte("fake binary")
	original := []byte("original binary")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(content)
	}))
	defer srv.Close()

	dir := t.TempDir()
	targetPath := filepath.Join(dir, "k2r")
	if err := os.WriteFile(targetPath, original, 0755); err != nil {
		t.Fatal(err)
	}

	err := DownloadAndReplace(srv.URL+"/k2r-linux-amd64", "wronghash000000", targetPath)
	if err == nil {
		t.Fatal("expected checksum mismatch error, got nil")
	}

	// Original file must be preserved.
	got, readErr := os.ReadFile(targetPath)
	if readErr != nil {
		t.Fatalf("read target after failure: %v", readErr)
	}
	if string(got) != string(original) {
		t.Errorf("original file was overwritten on checksum mismatch: got %q", got)
	}
}

func TestParseChecksums(t *testing.T) {
	body := `# comment line
abc123  k2r-linux-amd64
deadbeef  k2r-linux-arm64
`
	m, err := parseChecksums(body)
	if err != nil {
		t.Fatalf("parseChecksums: %v", err)
	}
	if m["k2r-linux-amd64"] != "abc123" {
		t.Errorf("amd64 hash: got %q, want %q", m["k2r-linux-amd64"], "abc123")
	}
	if m["k2r-linux-arm64"] != "deadbeef" {
		t.Errorf("arm64 hash: got %q, want %q", m["k2r-linux-arm64"], "deadbeef")
	}
}

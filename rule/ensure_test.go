package rule

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// makeTarballHandler returns an http.HandlerFunc that serves a tar.gz
// containing the given files. Used by EnsureBundles tests where extraction
// mechanics matter but bundle content does not.
func makeTarballHandler(t *testing.T, files map[string][]byte) http.HandlerFunc {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	for name, data := range files {
		hdr := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(data))}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	payload := buf.Bytes()
	return func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "k2-rules.tar.gz") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/gzip")
		_, _ = w.Write(payload)
	}
}

func TestEnsureBundles_ColdDownload(t *testing.T) {
	dir := t.TempDir()

	// A fake .k2b (magic-only). EnsureBundles doesn't parse bundles —
	// that's the loader's job — so as long as the file lands on disk
	// with the right name, this test is satisfied.
	srv := httptest.NewServer(makeTarballHandler(t, map[string][]byte{
		"dummy.k2b": {0x4b, 0x32, 0x42, 0x00}, // K2B\0
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := EnsureBundles(ctx, dir, []string{srv.URL + "/"}); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "dummy.k2b")); err != nil {
		t.Errorf("dummy.k2b not extracted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "bundles.version")); err != nil {
		t.Errorf("bundles.version not written: %v", err)
	}
	// .tmp/ must be cleaned up after success.
	if _, err := os.Stat(filepath.Join(dir, ".tmp")); !os.IsNotExist(err) {
		t.Errorf(".tmp/ should be removed after success, stat err=%v", err)
	}
}

func TestEnsureBundles_FreshSkip(t *testing.T) {
	dir := t.TempDir()

	// Write a fresh bundles.version — EnsureBundles should see it and skip
	// any network work. We give it an unreachable source to prove no HTTP
	// request was attempted.
	versionPath := filepath.Join(dir, "bundles.version")
	if err := os.WriteFile(versionPath, []byte("2026-04-20T00:00:00Z sha256:abc\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := EnsureBundles(ctx, dir, []string{"http://127.0.0.1:1/"})
	if err != nil {
		t.Errorf("expected nil (fresh skip), got %v", err)
	}
}

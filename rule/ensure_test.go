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
	"sync"
	"sync/atomic"
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

func TestEnsureBundles_ConcurrentSingleflight(t *testing.T) {
	dir := t.TempDir()
	var reqCount atomic.Int32

	// Slow server: sleep 200ms per request so multiple callers collide
	// inside the singleflight window.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqCount.Add(1)
		time.Sleep(200 * time.Millisecond)
		makeTarballHandler(t, map[string][]byte{
			"dummy.k2b": {0x4b, 0x32, 0x42, 0x00},
		})(w, r)
	}))
	defer srv.Close()

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := EnsureBundles(ctx, dir, []string{srv.URL + "/"}); err != nil {
				t.Errorf("unexpected err: %v", err)
			}
		}()
	}
	wg.Wait()

	// 5 callers × 1 source × singleflight => ideally 1 HTTP hit.
	// Allow up to 3 to tolerate race-window reopening if the first caller
	// finishes before a late caller reads bundles.version. Anything above
	// 3 indicates singleflight isn't working.
	if got := reqCount.Load(); got > 3 {
		t.Errorf("expected ≤3 HTTP requests via singleflight, got %d", got)
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

func TestEnsureBundles_AllSourcesFail(t *testing.T) {
	dir := t.TempDir()
	srv500 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv500.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := EnsureBundles(ctx, dir, []string{srv500.URL + "/"})
	if err == nil {
		t.Fatal("expected error when all sources 500, got nil")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "bundles.version")); statErr == nil {
		t.Error("bundles.version should not exist on download failure")
	}
}

func TestEnsureBundles_CorruptTarball(t *testing.T) {
	dir := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		_, _ = w.Write([]byte("this is not a gzip file"))
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := EnsureBundles(ctx, dir, []string{srv.URL + "/"})
	if err == nil {
		t.Fatal("expected extract error, got nil")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "bundles.version")); statErr == nil {
		t.Error("bundles.version should not exist on extract failure")
	}
}

func TestEnsureBundles_MaxSize(t *testing.T) {
	dir := t.TempDir()
	payload := make([]byte, 6<<20) // 6MB — over 5MB limit
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := EnsureBundles(ctx, dir, []string{srv.URL + "/"})
	if err == nil {
		t.Fatal("expected size-limit error, got nil")
	}
}

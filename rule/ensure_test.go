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

	"golang.org/x/sync/singleflight"
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

func TestEnsureBundles_CleansStaleTmp(t *testing.T) {
	dir := t.TempDir()

	// Pre-populate a leftover .tmp/ from a simulated prior crash.
	stale := filepath.Join(dir, ".tmp", "leftover.bin")
	if err := os.MkdirAll(filepath.Dir(stale), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(makeTarballHandler(t, map[string][]byte{
		"dummy.k2b": {0x4b, 0x32, 0x42, 0x00},
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := EnsureBundles(ctx, dir, []string{srv.URL + "/"}); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(dir, ".tmp")); !os.IsNotExist(err) {
		t.Errorf(".tmp/ should be removed after successful EnsureBundles (err=%v)", err)
	}
}

// TestAcquireLock_CrossProcessSerialization simulates the iOS
// Plugin-vs-NetworkExtension race: two processes call EnsureBundles on the
// same cacheDir. Within this single test binary we model distinct processes
// by opening the lock file twice; flock(2) is per-open-file-description, so
// goroutines in one process hold independent lock states. Verifies:
//   - second acquirer blocks while first holds the lock
//   - ctx cancellation releases the waiter without deadlock
//   - after release, second acquirer can proceed
func TestAcquireLock_CrossProcessSerialization(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, ".ensure.lock")

	release1, err := acquireLock(context.Background(), lockPath)
	if err != nil {
		t.Fatal(err)
	}

	// Second attempt should block; bound it with a short ctx.
	start := time.Now()
	ctx2, cancel2 := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel2()
	if _, err := acquireLock(ctx2, lockPath); err == nil {
		t.Fatal("expected timeout while first lock held, got nil")
	}
	if elapsed := time.Since(start); elapsed < 250*time.Millisecond {
		t.Errorf("second acquirer returned too early: %v (expected ≥250ms)", elapsed)
	}

	release1()

	// After release, next acquirer should succeed quickly.
	ctx3, cancel3 := context.WithTimeout(context.Background(), time.Second)
	defer cancel3()
	release2, err := acquireLock(ctx3, lockPath)
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	release2()
}

// TestEnsureBundles_CrossProcessFreshnessRecheck models the iOS real-world
// flow: process A (Plugin) downloads and writes bundles.version; process B
// (NE) was queued behind the flock. When B gets the lock, it must re-check
// freshness and skip — NOT re-download. Exercises the second freshness check
// at the top of ensureOnce (inside the lock). Proves A's write is visible to
// B across the lock boundary.
func TestEnsureBundles_CrossProcessFreshnessRecheck(t *testing.T) {
	dir := t.TempDir()
	var reqCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqCount.Add(1)
		// Deliberately slow so the second caller queues behind the flock.
		time.Sleep(150 * time.Millisecond)
		makeTarballHandler(t, map[string][]byte{
			"dummy.k2b": {0x4b, 0x32, 0x42, 0x00},
		})(w, r)
	}))
	defer srv.Close()

	// First caller: performs the download.
	if err := EnsureBundles(context.Background(), dir, []string{srv.URL + "/"}); err != nil {
		t.Fatal(err)
	}

	// Reset the in-process singleflight so the second call can't hit the
	// cached result — force it through the flock path like a separate process.
	ensureSF = singleflight.Group{}

	// Second caller: must see A's fresh bundles.version and skip.
	if err := EnsureBundles(context.Background(), dir, []string{srv.URL + "/"}); err != nil {
		t.Fatal(err)
	}

	if got := reqCount.Load(); got != 1 {
		t.Errorf("expected exactly 1 download (second caller should skip via freshness), got %d", got)
	}
}

func TestDefaultSources_ContainsExpectedMirrors(t *testing.T) {
	expected := []string{
		"github.com/kaitu-io/k2-rules/releases/latest/download",
		"ghfast.top",
		"gh-proxy.com",
		"cdn.jsdelivr.net/gh/kaitu-io/k2-rules@release",
		"fastly.jsdelivr.net/gh/kaitu-io/k2-rules@release",
		"testingcf.jsdelivr.net/gh/kaitu-io/k2-rules@release",
		"gcore.jsdelivr.net/gh/kaitu-io/k2-rules@release",
		"cdn.jsdmirror.com/gh/kaitu-io/k2-rules@release",
		"jsd.onmicrosoft.cn/gh/kaitu-io/k2-rules@release",
	}
	joined := strings.Join(DefaultSources, " ")
	for _, want := range expected {
		if !strings.Contains(joined, want) {
			t.Errorf("DefaultSources missing %q", want)
		}
	}
	if len(DefaultSources) < 9 {
		t.Errorf("expected ≥9 sources, got %d", len(DefaultSources))
	}
}

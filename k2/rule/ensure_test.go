package rule

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
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
		if !strings.HasSuffix(r.URL.Path, "krs.tar.gz") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/gzip")
		_, _ = w.Write(payload)
	}
}

func TestEnsureBundles_ColdDownload(t *testing.T) {
	dir := t.TempDir()

	// A fake .krs (magic-only). EnsureBundles doesn't parse bundles —
	// that's the loader's job — so as long as the file lands on disk
	// with the right name, this test is satisfied.
	srv := httptest.NewServer(makeTarballHandler(t, map[string][]byte{
		"dummy.krs": {0x4b, 0x32, 0x42, 0x00}, // K2B\0
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := EnsureBundles(ctx, dir, []string{srv.URL + "/"}); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "dummy.krs")); err != nil {
		t.Errorf("dummy.krs not extracted: %v", err)
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
			"dummy.krs": {0x4b, 0x32, 0x42, 0x00},
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

	// A genuinely warm cache = fresh bundles.version AND at least one .krs
	// bundle present. EnsureBundles should see it and skip any network work;
	// the unreachable source proves no HTTP request was attempted.
	versionPath := filepath.Join(dir, "bundles.version")
	if err := os.WriteFile(versionPath, []byte("2026-04-20T00:00:00Z sha256:abc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "geoip.krs"), []byte{0x4b, 0x32, 0x42, 0x00}, 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := EnsureBundles(ctx, dir, []string{"http://127.0.0.1:1/"})
	if err != nil {
		t.Errorf("expected nil (fresh skip), got %v", err)
	}
}

// TestEnsureBundles_StaleWhenVersionFreshButNoKRS guards the self-heal half of
// the cache-poisoning fix. A fresh bundles.version with zero .krs on disk (iOS
// App-Group eviction, or a cache poisoned before the Fix-1 guard shipped) must
// NOT count as fresh. EnsureBundles must re-download instead of short-circuiting
// so the cache self-heals on the next connect rather than waiting out the TTL.
func TestEnsureBundles_StaleWhenVersionFreshButNoKRS(t *testing.T) {
	dir := t.TempDir()
	// Fresh marker by mtime, but no .krs bundles — the poisoned state.
	if err := os.WriteFile(filepath.Join(dir, "bundles.version"),
		[]byte("2026-04-20T00:00:00Z sha256:abc\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Unreachable source: if EnsureBundles attempts a download it errors —
	// which is exactly what proves it did NOT treat the empty cache as fresh.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := EnsureBundles(ctx, dir, []string{"http://127.0.0.1:1/"})
	if err == nil {
		t.Fatal("expected a download attempt (error) when version fresh but zero .krs present — poisoned cache treated as fresh")
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

// TestEnsureBundles_RejectsTarballWithZeroKRS reproduces the cache-poisoning
// root cause: a stale CDN mirror still serving the pre-Plan-A tarball (only
// .k2b + .yaml, zero .krs). The current extractor pulls no .krs yet ensureOnce
// still writes a fresh bundles.version — marking the cache "ready" with zero
// usable bundles, so region routes silently fall through to all-proxy for the
// whole TTL. A download that yields zero .krs must be treated as a failure:
// no atomic swap, no bundles.version.
func TestEnsureBundles_RejectsTarballWithZeroKRS(t *testing.T) {
	dir := t.TempDir()
	srv := httptest.NewServer(makeTarballHandler(t, map[string][]byte{
		"cn-direct.k2b": {0x4b, 0x32, 0x42, 0x00}, // legacy format, ignored by extractor
		"meta.yaml":     []byte("schema: 1\n"),
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := EnsureBundles(ctx, dir, []string{srv.URL + "/"})
	if err == nil {
		t.Fatal("expected error when tarball has zero .krs bundles, got nil")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "bundles.version")); statErr == nil {
		t.Error("bundles.version must NOT be written when extraction yields zero .krs (cache poisoning)")
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
		"dummy.krs": {0x4b, 0x32, 0x42, 0x00},
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
	var tarballHits atomic.Int32

	// This server has no manifest route, so EnsureBundles takes the
	// manifest-unreachable legacy path. We count only krs.tar.gz fetches — the
	// "download" the test is asserting on — so the manifest probe (a 404 here)
	// doesn't pollute the count. The cross-process freshness contract is: the
	// second caller must perform ZERO tarball downloads (fast-path skip on the
	// fresh bundles.version the first caller wrote).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "krs.tar.gz") {
			tarballHits.Add(1)
		}
		// Deliberately slow so the second caller queues behind the flock.
		time.Sleep(150 * time.Millisecond)
		makeTarballHandler(t, map[string][]byte{
			"dummy.krs": {0x4b, 0x32, 0x42, 0x00},
		})(w, r)
	}))
	defer srv.Close()

	// First caller: performs the download.
	if err := EnsureBundles(context.Background(), dir, []string{srv.URL + "/"}); err != nil {
		t.Fatal(err)
	}

	// Second caller: must see A's fresh bundles.version and skip.
	if err := EnsureBundles(context.Background(), dir, []string{srv.URL + "/"}); err != nil {
		t.Fatal(err)
	}

	if got := tarballHits.Load(); got != 1 {
		t.Errorf("expected exactly 1 tarball download (second caller should skip via freshness), got %d", got)
	}
}

func TestExtractTarball_AcceptsYAMLFiles(t *testing.T) {
	// Build a fake tarball with one .krs and one .yaml entry.
	tmp := t.TempDir()
	tgz := filepath.Join(tmp, "test.tar.gz")

	f, _ := os.Create(tgz)
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)

	writeEntry := func(name, content string) {
		hdr := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	writeEntry("cn-direct.krs", "K2RBfake-binary-data")
	writeEntry("app-bypass-cn.yaml", "version: 1\nregion: cn\n")
	tw.Close()
	gz.Close()
	f.Close()

	out := t.TempDir()
	if err := extractTarball(tgz, out); err != nil {
		t.Fatalf("extract: %v", err)
	}

	if _, err := os.Stat(filepath.Join(out, "cn-direct.krs")); err != nil {
		t.Errorf(".krs missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "app-bypass-cn.yaml")); err != nil {
		t.Errorf(".yaml missing: %v", err)
	}
}

func TestAtomicSwap_HandlesYAML(t *testing.T) {
	cache := t.TempDir()
	newDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(newDir, "app-bypass-cn.yaml"), []byte("yaml content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newDir, "cn-direct.krs"), []byte("binary"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := atomicSwap(cache, newDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cache, "app-bypass-cn.yaml")); err != nil {
		t.Errorf(".yaml not swapped: %v", err)
	}
}

func TestAtomicSwap_PrunesOrphanedYAML(t *testing.T) {
	cache := t.TempDir()
	// Pre-existing .yaml in cache that's NOT in newDir → should be pruned.
	if err := os.WriteFile(filepath.Join(cache, "app-bypass-old.yaml"), []byte("orphan"), 0o644); err != nil {
		t.Fatal(err)
	}
	newDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(newDir, "app-bypass-cn.yaml"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := atomicSwap(cache, newDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cache, "app-bypass-old.yaml")); !os.IsNotExist(err) {
		t.Errorf("orphan .yaml should have been pruned")
	}
}

// TestAtomicSwap_PrunesLeftoverK2B covers the Plan A upgrade-path rollover:
// a pre-Plan-A client wrote <cc>-direct.k2b into the cache, the Plan A binary
// now extracts only .krs from the tarball, and atomicSwap's prune loop must
// delete the leftover .k2b so the cache reflects the new format.
//
// Without this guard, cn-direct.k2b would linger forever (never written by
// the new extractor, never deleted by the swap), bloating disk and confusing
// support telemetry about which format is active.
func TestAtomicSwap_PrunesLeftoverK2B(t *testing.T) {
	cache := t.TempDir()
	// Simulate previous binary's cache: two leftover .k2b files.
	if err := os.WriteFile(filepath.Join(cache, "cn-direct.k2b"), []byte("old cn"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "overseas.k2b"), []byte("old overseas"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Fresh extraction has only .krs.
	newDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(newDir, "cn.krs"), []byte("new cn"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newDir, "overseas.krs"), []byte("new overseas"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := atomicSwap(cache, newDir); err != nil {
		t.Fatal(err)
	}

	// Leftover .k2b must be gone.
	for _, name := range []string{"cn-direct.k2b", "overseas.k2b"} {
		if _, err := os.Stat(filepath.Join(cache, name)); !os.IsNotExist(err) {
			t.Errorf("%s should have been pruned, stat err=%v", name, err)
		}
	}
	// New .krs must be in place.
	for _, name := range []string{"cn.krs", "overseas.krs"} {
		if _, err := os.Stat(filepath.Join(cache, name)); err != nil {
			t.Errorf("%s should exist after swap, stat err=%v", name, err)
		}
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
	}
	joined := strings.Join(DefaultSources, " ")
	for _, want := range expected {
		if !strings.Contains(joined, want) {
			t.Errorf("DefaultSources missing %q", want)
		}
	}
	if len(DefaultSources) < len(expected) {
		t.Errorf("expected ≥%d sources, got %d", len(expected), len(DefaultSources))
	}
	// Removed 2026-05-24: third-party mirrors with ~25-day cache staleness.
	for _, banned := range []string{"cdn.jsdmirror.com", "jsd.onmicrosoft.cn"} {
		if strings.Contains(joined, banned) {
			t.Errorf("DefaultSources must not contain stale mirror %q", banned)
		}
	}
}

// makeManifestAndTarballHandler serves manifest.json (with the given version
// and computed shas) AND krs.tar.gz of the given files from one server.
func makeManifestAndTarballHandler(t *testing.T, version string, files map[string][]byte) http.HandlerFunc {
	t.Helper()
	// Build tarball.
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	bundles := map[string]string{}
	for name, data := range files {
		hdr := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(data))}
		_ = tw.WriteHeader(hdr)
		_, _ = tw.Write(data)
		if strings.HasSuffix(name, ".krs") {
			sum := sha256.Sum256(data)
			region := strings.TrimSuffix(name, ".krs")
			bundles[region] = hex.EncodeToString(sum[:])
		}
	}
	_ = tw.Close()
	_ = gw.Close()
	tarball := buf.Bytes()

	var mb strings.Builder
	mb.WriteString(`{"schemaVersion":2,"version":"` + version + `","bundles":{`)
	first := true
	for region, sha := range bundles {
		if !first {
			mb.WriteString(",")
		}
		first = false
		mb.WriteString(`"` + region + `":{"sha256":"` + sha + `","size":1,"ruleCount":9}`)
	}
	mb.WriteString(`}}`)
	manifestJSON := mb.String()

	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "manifest.json"):
			_, _ = w.Write([]byte(manifestJSON))
		case strings.HasSuffix(r.URL.Path, "krs.tar.gz"):
			_, _ = w.Write(tarball)
		default:
			http.NotFound(w, r)
		}
	}
}

func sha256Hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func TestEnsureBundles_VersionIdentity_DownloadsWhenNewer(t *testing.T) {
	dir := t.TempDir()
	srv := httptest.NewServer(makeManifestAndTarballHandler(t, "2026-06-02T00:00:00Z", map[string][]byte{
		"cn.krs": {0x4b, 0x32, 0x52, 0x4c}, // K2RL magic-ish; content only needs to round-trip sha
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := EnsureBundles(ctx, dir, []string{srv.URL + "/"}); err != nil {
		t.Fatalf("EnsureBundles: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "cn.krs")); err != nil {
		t.Errorf("cn.krs not extracted: %v", err)
	}
	if v := readLocalManifestVersion(dir); v != "2026-06-02T00:00:00Z" {
		t.Errorf("recorded version %q, want the manifest version", v)
	}
}

func TestEnsureBundles_VersionIdentity_SkipsWhenSameVersion(t *testing.T) {
	dir := t.TempDir()
	files := map[string][]byte{"cn.krs": {1, 2, 3, 4}}
	srv := httptest.NewServer(makeManifestAndTarballHandler(t, "2026-06-02T00:00:00Z", files))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// First call downloads.
	if err := EnsureBundles(ctx, dir, []string{srv.URL + "/"}); err != nil {
		t.Fatal(err)
	}
	// Make local cache appear older than shortTTL so the manifest path runs again.
	old := time.Now().Add(-2 * time.Hour)
	_ = os.Chtimes(filepath.Join(dir, "bundles.version"), old, old)

	// Second call: same manifest version + matching sha → must NOT re-extract.
	// Detect by counting tarball requests.
	var tarballHits atomic.Int32
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "manifest.json") {
			_, _ = w.Write([]byte(`{"schemaVersion":2,"version":"2026-06-02T00:00:00Z","bundles":{"cn":{"sha256":"` +
				sha256Hex(files["cn.krs"]) + `","size":1,"ruleCount":9}}}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "krs.tar.gz") {
			tarballHits.Add(1)
		}
		http.NotFound(w, r)
	}))
	defer srv2.Close()

	if err := EnsureBundles(ctx, dir, []string{srv2.URL + "/"}); err != nil {
		t.Fatalf("second EnsureBundles: %v", err)
	}
	if tarballHits.Load() != 0 {
		t.Errorf("tarball fetched %d times, want 0 (version unchanged)", tarballHits.Load())
	}
}

func TestEnsureBundles_ManifestUnreachable_FallsBackToLegacy(t *testing.T) {
	dir := t.TempDir()
	// Server serves the tarball but 500s the manifest → client must fall back
	// to the legacy blind download (no manifest verify) rather than failing.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "manifest.json") {
			http.Error(w, "no manifest", http.StatusInternalServerError)
			return
		}
		makeTarballHandler(t, map[string][]byte{"cn.krs": {1, 2, 3, 4}})(w, r)
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := EnsureBundles(ctx, dir, []string{srv.URL + "/"}); err != nil {
		t.Fatalf("fallback path should still populate cache: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "cn.krs")); err != nil {
		t.Errorf("legacy fallback did not extract cn.krs: %v", err)
	}
}

// We no longer verify the downloaded bundle bytes against the manifest sha
// (HTTPS is trusted; the sha-vs-manifest check only ever detected CDN per-file
// staleness and then BRICKED by rejecting a valid-but-stale bundle and sitting
// on an older/empty cache — the 2026-06 all-proxy outage). A non-empty download
// is now accepted: a 1-day-old rule bundle is fine, far better than degrading to
// all-proxy. Only structural guards remain (countKRS==0, gzip/tar decode).
func TestEnsureBundles_ShaMismatch_AcceptsDownload(t *testing.T) {
	dir := t.TempDir()
	// Pre-populate an older cn.krs + recorded older version.
	priorCN := []byte("prior-cn-bytes")
	if err := os.WriteFile(filepath.Join(dir, "cn.krs"), priorCN, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeLocalVersion(dir, "2026-06-01T00:00:00Z", false); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	_ = os.Chtimes(filepath.Join(dir, "bundles.version"), old, old)

	// Server: manifest advertises a NEW version whose declared sha does NOT match
	// the tarball it serves. Under the no-verify model the download is accepted.
	downloadedCN := []byte("downloaded-cn-bytes")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "manifest.json") {
			_, _ = w.Write([]byte(`{"schemaVersion":2,"version":"2026-06-02T00:00:00Z","bundles":{"cn":{"sha256":"deadbeefmismatch","size":1,"ruleCount":9}}}`))
			return
		}
		makeTarballHandler(t, map[string][]byte{"cn.krs": downloadedCN})(w, r)
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := EnsureBundles(ctx, dir, []string{srv.URL + "/"}); err != nil {
		t.Fatalf("download must be accepted without sha verification: %v", err)
	}
	// The downloaded (sha-mismatched) bundle replaced the prior cache.
	got, _ := os.ReadFile(filepath.Join(dir, "cn.krs"))
	if !bytes.Equal(got, downloadedCN) {
		t.Errorf("cn.krs = %q, want the downloaded bytes %q (download not swapped in)", got, downloadedCN)
	}
	// Version advanced to the manifest's new version.
	if v := readLocalManifestVersion(dir); v != "2026-06-02T00:00:00Z" {
		t.Errorf("recorded version = %q, want the new manifest version", v)
	}
}

func TestEnsureBundles_PinsManifestSource(t *testing.T) {
	dir := t.TempDir()
	files := map[string][]byte{"cn.krs": {9, 9, 9, 9}}
	cnSHA := sha256Hex(files["cn.krs"])

	// "stale" mirror: serves an OLDER manifest, and counts tarball hits.
	var staleTarballHits atomic.Int32
	stale := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "manifest.json") {
			_, _ = w.Write([]byte(`{"schemaVersion":2,"version":"2026-06-01T00:00:00Z","bundles":{"cn":{"sha256":"` + cnSHA + `","size":1,"ruleCount":9}}}`))
			return
		}
		staleTarballHits.Add(1)
		makeTarballHandler(t, files)(w, r)
	}))
	defer stale.Close()

	// "fresh" mirror: serves the NEWER manifest (wins newest-wins) + the tarball.
	fresh := httptest.NewServer(makeManifestAndTarballHandler(t, "2026-06-02T00:00:00Z", files))
	defer fresh.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Pass stale first to prove the download pins to fresh (the manifest winner),
	// not to whoever is first in the source list.
	if err := EnsureBundles(ctx, dir, []string{stale.URL + "/", fresh.URL + "/"}); err != nil {
		t.Fatalf("EnsureBundles: %v", err)
	}
	if staleTarballHits.Load() != 0 {
		t.Errorf("tarball was fetched from the stale mirror %d times — pin-source failed",
			staleTarballHits.Load())
	}
}

func TestEnsureBundles_ManifestUnreachable_BackstopSkipsDownload(t *testing.T) {
	dir := t.TempDir()
	// Pre-populate a valid cache: one good cn.krs + a recorded version, aged to
	// ~2h (past shortTTL=1h, within freshTTL=24h).
	if err := os.WriteFile(filepath.Join(dir, "cn.krs"), []byte("good-cn"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeLocalVersion(dir, "2026-06-01T00:00:00Z", false); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	_ = os.Chtimes(filepath.Join(dir, "bundles.version"), old, old)

	// Server 500s the manifest AND would count any tarball fetch.
	var tarballHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "manifest.json") {
			http.Error(w, "no manifest", http.StatusInternalServerError)
			return
		}
		if strings.HasSuffix(r.URL.Path, "krs.tar.gz") {
			tarballHits.Add(1)
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Manifest unreachable + cache within 24h backstop → return nil, NO download.
	if err := EnsureBundles(ctx, dir, []string{srv.URL + "/"}); err != nil {
		t.Fatalf("backstop should return nil: %v", err)
	}
	if tarballHits.Load() != 0 {
		t.Errorf("tarball fetched %d times, want 0 (24h backstop should skip download)", tarballHits.Load())
	}
	// Prior cache intact.
	got, _ := os.ReadFile(filepath.Join(dir, "cn.krs"))
	if string(got) != "good-cn" {
		t.Errorf("prior cn.krs changed: %q", got)
	}
}

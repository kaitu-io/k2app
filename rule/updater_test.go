package rule

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kaitu-io/k2-rules/krs"
)

// TestUpdater_TriggersReload verifies that StartUpdater downloads updated
// bundles and reloads the engine within the configured check interval.
func TestUpdater_TriggersReload(t *testing.T) {
	// Build updated k2b: google set now includes youtube.com.
	var updatedBuf bytes.Buffer
	if err := krs.WriteBundle(&updatedBuf, &krs.Bundle{Sets: []krs.NamedSet{
		{Name: "google", DomainSuffixes: []string{"google.com", "youtube.com"}},
	}}); err != nil {
		t.Fatalf("krs.WriteBundle: %v", err)
	}
	updatedData := updatedBuf.Bytes()

	// httptest server that serves a tarball containing the updated bundle.
	srv := httptest.NewServer(makeTarballHandler(t, map[string][]byte{
		"overseas.krs": updatedData,
	}))
	defer srv.Close()

	dir := t.TempDir()

	// Write the initial bundle: google set with only google.com (no youtube.com).
	// Do NOT write bundles.version — EnsureBundles must see the cache as stale
	// and download the tarball on the first tick.
	var initialBuf bytes.Buffer
	if err := krs.WriteBundle(&initialBuf, &krs.Bundle{Sets: []krs.NamedSet{
		{Name: "google", DomainSuffixes: []string{"google.com"}},
	}}); err != nil {
		t.Fatalf("krs.WriteBundle initial: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "overseas.krs"), initialBuf.Bytes(), 0o644); err != nil {
		t.Fatalf("WriteFile initial: %v", err)
	}

	// Build initial engine: google→proxy but youtube.com is NOT in the set yet.
	initialBundle := mustBundle(t, []krs.NamedSet{
		{Name: "google", DomainSuffixes: []string{"google.com"}},
	})
	initialIdx := krs.Index([]*krs.Bundle{initialBundle})
	initialRoutes := []RouteEntry{
		{Target: Target(2), Sets: bindSets(initialIdx, "google")},
	}
	eng := NewEngine(initialRoutes, Target(0), []*krs.Bundle{initialBundle}, false)
	defer eng.Close()

	// Confirm initial state: youtube.com does NOT match proxy.
	if got := eng.Match("youtube.com"); got != Target(0) {
		t.Fatalf("before reload: Match(youtube.com) = %v, want %v", got, Target(0))
	}

	// RebuildFunc: loads bundles from cacheDir, builds routes with google→proxy.
	rebuild := func(cacheDir string) ([]RouteEntry, []*krs.Bundle, error) {
		bundles, err := krs.Load(cacheDir)
		if err != nil {
			return nil, nil, err
		}
		idx := krs.Index(bundles)
		routes := []RouteEntry{
			{Target: Target(2), Sets: bindSets(idx, "google")},
		}
		return routes, bundles, nil
	}

	ctx := t.Context()

	StartUpdater(
		ctx,
		eng,
		dir,
		[]string{srv.URL + "/"},
		rebuild,
		WithCheckInterval(500*time.Millisecond),
	)

	// Wait up to 2s for the reload to happen.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if eng.Match("youtube.com") == Target(2) {
			return // success
		}
		time.Sleep(50 * time.Millisecond)
	}

	t.Fatalf("after 2s: Match(youtube.com) = %v, want %v", eng.Match("youtube.com"), Target(2))
}

// TestDownloadAndReload_SkipsFresh verifies that DownloadAndReload skips
// HTTP entirely when bundles.version is fresh.
func TestDownloadAndReload_SkipsFresh(t *testing.T) {
	dir := t.TempDir()
	// Warm cache = fresh bundles.version AND a bundle present. The .krs is
	// required: a fresh marker over an empty cache is no longer treated as
	// fresh (see isVersionFresh / cache-poisoning fix).
	if err := os.WriteFile(filepath.Join(dir, "bundles.version"),
		[]byte("2026-04-20T00:00:00Z sha256:abc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "overseas.krs"), makeTestK2B(t), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("unexpected HTTP call")
	}))
	defer srv.Close()

	ctx := context.Background()
	if err := DownloadAndReload(ctx, nil, dir, []string{srv.URL + "/"}, nil); err != nil {
		t.Fatal(err)
	}
}

// TestDownloadAndReload_DownloadsWhenStale verifies that stale (no bundles.version)
// triggers a real tarball download.
func TestDownloadAndReload_DownloadsWhenStale(t *testing.T) {
	dir := t.TempDir()
	data := makeTestK2B(t)
	srv := httptest.NewServer(makeTarballHandler(t, map[string][]byte{
		"overseas.krs": data,
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := DownloadAndReload(ctx, nil, dir, []string{srv.URL + "/"}, nil); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "overseas.krs"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("file should have been downloaded")
	}
}

// TestDownloadAndReload_UpgradesEmbedFloor is the Finding-A guard: a cache
// seeded from the embedded archive reads as "fresh" (mtime now + .krs present),
// so the plain 24h gate would skip the CDN download and the device would serve
// the build-time baseline for a whole TTL. DownloadAndReload must detect the
// embed provenance and FORCE a refresh that replaces the floor and clears the
// embedded marker — then later ticks fall back to the normal gated cadence.
func TestDownloadAndReload_UpgradesEmbedFloor(t *testing.T) {
	dir := t.TempDir()

	// Cold-start floor: seed from the embedded archive. Cache is now fresh.
	if err := SeedFromEmbedIfEmpty(dir); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if !isVersionFresh(dir, bundlesFreshTTL) {
		t.Fatal("precondition: seeded cache should read fresh")
	}

	var reqCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqCount.Add(1)
		makeTarballHandler(t, map[string][]byte{
			"cn.krs":       {0x4b, 0x32, 0x42, 0x00},
			"overseas.krs": {0x4b, 0x32, 0x42, 0x00},
		})(w, r)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := DownloadAndReload(ctx, nil, dir, []string{srv.URL + "/"}, nil); err != nil {
		t.Fatalf("DownloadAndReload: %v", err)
	}

	if reqCount.Load() == 0 {
		t.Fatal("embed-seeded floor must force a CDN download (got 0 HTTP requests — 24h gate not bypassed)")
	}
	if _, err := os.Stat(filepath.Join(dir, "overseas.krs")); err != nil {
		t.Errorf("CDN bundle not written over the floor: %v", err)
	}
	if wasEmbedSeeded(dir) {
		t.Error("embedded marker must be cleared after a successful CDN upgrade")
	}

	// Second pass on the now-CDN-stamped fresh cache must NOT re-download —
	// proving the force is embed-only, not always-on (normal 24h cadence intact).
	before := reqCount.Load()
	if err := DownloadAndReload(ctx, nil, dir, []string{srv.URL + "/"}, nil); err != nil {
		t.Fatalf("second DownloadAndReload: %v", err)
	}
	if reqCount.Load() != before {
		t.Errorf("CDN-stamped fresh cache must be gated (no extra download); requests %d → %d", before, reqCount.Load())
	}
}

// TestDownloadAndReloadNamed_UpgradesEmbedFloor mirrors the above for the
// NamedBundle variant the engine actually wires (StartUpdaterNamed).
func TestDownloadAndReloadNamed_UpgradesEmbedFloor(t *testing.T) {
	dir := t.TempDir()
	if err := SeedFromEmbedIfEmpty(dir); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var reqCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqCount.Add(1)
		makeTarballHandler(t, map[string][]byte{
			"cn.krs":       {0x4b, 0x32, 0x42, 0x00},
			"overseas.krs": {0x4b, 0x32, 0x42, 0x00},
		})(w, r)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := DownloadAndReloadNamed(ctx, nil, dir, []string{srv.URL + "/"}, nil); err != nil {
		t.Fatalf("DownloadAndReloadNamed: %v", err)
	}
	if reqCount.Load() == 0 {
		t.Fatal("Named variant must also force a CDN download for an embed-seeded floor")
	}
	if wasEmbedSeeded(dir) {
		t.Error("embedded marker must be cleared after a successful CDN upgrade (Named)")
	}
}

// TestDownloadAndReload_NilEngine verifies download-only mode (no reload).
func TestDownloadAndReload_NilEngine(t *testing.T) {
	dir := t.TempDir()
	srv := httptest.NewServer(makeTarballHandler(t, map[string][]byte{
		"overseas.krs": makeTestK2B(t),
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := DownloadAndReload(ctx, nil, dir, []string{srv.URL + "/"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "overseas.krs")); err != nil {
		t.Fatal(err)
	}
}

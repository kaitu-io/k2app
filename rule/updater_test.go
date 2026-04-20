package rule

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestUpdater_TriggersReload verifies that StartUpdater downloads updated
// bundles and reloads the engine within the configured check interval.
func TestUpdater_TriggersReload(t *testing.T) {
	// Build updated k2b: google set now includes youtube.com.
	var updatedBuf bytes.Buffer
	if err := WriteBundle(&updatedBuf, []BundleSet{
		{Name: "google", DomainSuffixes: []string{"google.com", "youtube.com"}},
	}); err != nil {
		t.Fatalf("WriteBundle: %v", err)
	}
	updatedData := updatedBuf.Bytes()

	// httptest server that serves a tarball containing the updated k2b.
	srv := httptest.NewServer(makeTarballHandler(t, map[string][]byte{
		"overseas.k2b": updatedData,
	}))
	defer srv.Close()

	dir := t.TempDir()

	// Write the initial k2b: google set with only google.com (no youtube.com).
	// Do NOT write bundles.version — EnsureBundles must see the cache as stale
	// and download the tarball on the first tick.
	var initialBuf bytes.Buffer
	if err := WriteBundle(&initialBuf, []BundleSet{
		{Name: "google", DomainSuffixes: []string{"google.com"}},
	}); err != nil {
		t.Fatalf("WriteBundle initial: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "overseas.k2b"), initialBuf.Bytes(), 0o644); err != nil {
		t.Fatalf("WriteFile initial: %v", err)
	}

	// Build initial engine: google→proxy but youtube.com is NOT in the set yet.
	initialBundle := mustBundle(t, []BundleSet{
		{Name: "google", DomainSuffixes: []string{"google.com"}},
	})
	initialIdx := Index([]*Bundle{initialBundle})
	initialRoutes := []RouteEntry{
		{Target: Target(2), Sets: bindSets(initialIdx, "google")},
	}
	eng := NewEngine(initialRoutes, Target(0), []*Bundle{initialBundle}, false)
	defer eng.Close()

	// Confirm initial state: youtube.com does NOT match proxy.
	if got := eng.Match("youtube.com"); got != Target(0) {
		t.Fatalf("before reload: Match(youtube.com) = %v, want %v", got, Target(0))
	}

	// RebuildFunc: loads bundles from cacheDir, builds routes with google→proxy.
	rebuild := func(cacheDir string) ([]RouteEntry, []*Bundle, error) {
		bundles, err := Load(cacheDir)
		if err != nil {
			return nil, nil, err
		}
		idx := Index(bundles)
		routes := []RouteEntry{
			{Target: Target(2), Sets: bindSets(idx, "google")},
		}
		return routes, bundles, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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
	if err := os.WriteFile(filepath.Join(dir, "bundles.version"),
		[]byte("2026-04-20T00:00:00Z sha256:abc\n"), 0o644); err != nil {
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
		"overseas.k2b": data,
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := DownloadAndReload(ctx, nil, dir, []string{srv.URL + "/"}, nil); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "overseas.k2b"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("file should have been downloaded")
	}
}

// TestDownloadAndReload_NilEngine verifies download-only mode (no reload).
func TestDownloadAndReload_NilEngine(t *testing.T) {
	dir := t.TempDir()
	srv := httptest.NewServer(makeTarballHandler(t, map[string][]byte{
		"overseas.k2b": makeTestK2B(t),
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := DownloadAndReload(ctx, nil, dir, []string{srv.URL + "/"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "overseas.k2b")); err != nil {
		t.Fatal(err)
	}
}

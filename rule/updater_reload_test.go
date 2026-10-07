package rule

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// freshCache writes a cache that ensureBundles treats as fresh (no network):
// one .krs plus a just-written bundles.version from a CDN download.
func freshCache(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cn.krs"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeLocalVersion(dir, "2026-10-01T00:00:00Z", false); err != nil {
		t.Fatal(err)
	}
	return dir
}

// countingRebuild returns a rebuild func that counts calls.
func countingRebuild(n *atomic.Int32) RebuildFuncNamed {
	return func(string) ([]RouteEntry, []NamedBundle, error) {
		n.Add(1)
		return nil, nil, nil
	}
}

func noNetwork(t *testing.T) []string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected rule-source request %s (cache is fresh)", r.URL.Path)
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return []string{srv.URL + "/"}
}

// TestRefreshAndReload_SkipsWhenDiskUnchanged: the engine already serves the
// on-disk set, so a check that changes nothing must not reload. Every reload
// is a full heap rebuild, and before this guard the updater's immediate first
// check reloaded on every connect (field log #3889: `rule-tmp-reload` ~100ms
// after each `connected`, dropping up to 60 DNS-learned pins).
func TestRefreshAndReload_SkipsWhenDiskUnchanged(t *testing.T) {
	dir := freshCache(t)
	loaded := BundleSignature(dir)
	eng := NewEngine(nil, Target(0), nil, false)
	defer eng.Close()

	var rebuilds atomic.Int32
	got, err := refreshAndReload(context.Background(), eng, dir, noNetwork(t), countingRebuild(&rebuilds), loaded)
	if err != nil {
		t.Fatalf("refreshAndReload: %v", err)
	}
	if n := rebuilds.Load(); n != 0 {
		t.Errorf("rebuilt %d times with an unchanged disk set, want 0", n)
	}
	if got != loaded {
		t.Errorf("returned signature %q, want unchanged %q", got, loaded)
	}
}

// TestRefreshAndReload_ReloadsWhenDiskChangedBehindEngine: the connect path no
// longer downloads, so the app-start prefetch may swap new bundles onto disk
// AFTER the engine loaded the old ones — and by the time the updater checks,
// the cache reads fresh and ensureBundles downloads nothing. The updater must
// still notice the engine is behind the disk and reload.
func TestRefreshAndReload_ReloadsWhenDiskChangedBehindEngine(t *testing.T) {
	dir := freshCache(t)
	loaded := BundleSignature(dir) // engine loaded v1

	// Background prefetch lands v2 (new content, new manifest version).
	time.Sleep(10 * time.Millisecond) // distinct mtime even on coarse clocks
	if err := os.WriteFile(filepath.Join(dir, "cn.krs"), []byte("v2-longer"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeLocalVersion(dir, "2026-10-02T00:00:00Z", false); err != nil {
		t.Fatal(err)
	}

	eng := NewEngine(nil, Target(0), nil, false)
	defer eng.Close()
	var rebuilds atomic.Int32
	got, err := refreshAndReload(context.Background(), eng, dir, noNetwork(t), countingRebuild(&rebuilds), loaded)
	if err != nil {
		t.Fatalf("refreshAndReload: %v", err)
	}
	if n := rebuilds.Load(); n != 1 {
		t.Errorf("rebuilt %d times after the disk set changed, want 1", n)
	}
	if got == loaded || got != BundleSignature(dir) {
		t.Errorf("returned signature %q, want the new on-disk signature %q", got, BundleSignature(dir))
	}
}

// TestRefreshAndReload_ReloadsAfterOwnDownload: the updater's own successful
// download changes the disk set and must reload.
func TestRefreshAndReload_ReloadsAfterOwnDownload(t *testing.T) {
	dir := t.TempDir()
	if err := SeedFromEmbedIfEmpty(dir); err != nil { // embed floor → forced refresh
		t.Fatal(err)
	}
	loaded := BundleSignature(dir)
	srv := httptest.NewServer(makeTarballHandler(t, map[string][]byte{
		"cn.krs":       {0x4b, 0x32, 0x42, 0x00},
		"overseas.krs": {0x4b, 0x32, 0x42, 0x00},
	}))
	defer srv.Close()

	eng := NewEngine(nil, Target(0), nil, false)
	defer eng.Close()
	var rebuilds atomic.Int32
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := refreshAndReload(ctx, eng, dir, []string{srv.URL + "/"}, countingRebuild(&rebuilds), loaded); err != nil {
		t.Fatalf("refreshAndReload: %v", err)
	}
	if n := rebuilds.Load(); n != 1 {
		t.Errorf("rebuilt %d times after a successful download, want 1", n)
	}
}

// TestRefreshAndReload_DownloadFailureStillReloadsChangedDisk: a failed fetch
// must not hide a disk change made by someone else.
func TestRefreshAndReload_DownloadFailureStillReloadsChangedDisk(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cn.krs"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded := BundleSignature(dir) // no bundles.version → never fresh → will fetch
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(dir, "overseas.krs"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()

	eng := NewEngine(nil, Target(0), nil, false)
	defer eng.Close()
	var rebuilds atomic.Int32
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := refreshAndReload(ctx, eng, dir, []string{srv.URL + "/"}, countingRebuild(&rebuilds), loaded); err == nil {
		t.Error("want the download error surfaced for logging")
	}
	if n := rebuilds.Load(); n != 1 {
		t.Errorf("rebuilt %d times, want 1 (disk changed even though our fetch failed)", n)
	}
}

// TestBundleSignature tracks the facts that define "a different set": file
// added/removed, content rewritten, manifest version changed — and nothing
// else (a touchVersionFile freshness bump is not a change).
func TestBundleSignature(t *testing.T) {
	dir := freshCache(t)
	base := BundleSignature(dir)
	if base == "" {
		t.Fatal("empty signature for a populated cache")
	}
	if BundleSignature(dir) != base {
		t.Fatal("signature not stable across calls")
	}
	if err := touchVersionFile(dir); err != nil {
		t.Fatal(err)
	}
	if BundleSignature(dir) != base {
		t.Error("touchVersionFile (freshness bump only) changed the signature")
	}
	if err := writeLocalVersion(dir, "2026-10-09T00:00:00Z", false); err != nil {
		t.Fatal(err)
	}
	v := BundleSignature(dir)
	if v == base {
		t.Error("manifest version change not reflected")
	}
	if err := os.WriteFile(filepath.Join(dir, "overseas.krs"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if BundleSignature(dir) == v {
		t.Error("added bundle not reflected")
	}
	if got := BundleSignature(filepath.Join(dir, "missing")); got != "" {
		t.Errorf("missing dir signature = %q, want empty", got)
	}
}

// TestRefreshAndReload_StoppedEngineNeverReloads: once the engine's ctx is
// cancelled (Stop), a check still in flight must not rebuild/reload — the rule
// engine may already be closed, and a reload would resurrect its tables.
func TestRefreshAndReload_StoppedEngineNeverReloads(t *testing.T) {
	dir := freshCache(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	eng := NewEngine(nil, Target(0), nil, false)
	defer eng.Close()
	var rebuilds atomic.Int32
	// loaded="" → the disk set differs, so only the ctx guard can stop the reload.
	if _, err := refreshAndReload(ctx, eng, dir, noNetwork(t), countingRebuild(&rebuilds), ""); err == nil {
		t.Error("want ctx error from a stopped engine's check")
	}
	if n := rebuilds.Load(); n != 0 {
		t.Errorf("rebuilt %d times after the engine stopped, want 0", n)
	}
}

// TestStartUpdaterNamed_LoadedSignatureGatesFirstCheck drives the real updater
// goroutine: with WithLoadedSignature matching the disk, the immediate first
// check (the one that used to reload ~100ms after every connect) does not
// rebuild; without the option it does (pre-signature behavior).
func TestStartUpdaterNamed_LoadedSignatureGatesFirstCheck(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts func(dir string) []UpdaterOption
		want int32
	}{
		{"signature matches disk", func(dir string) []UpdaterOption {
			return []UpdaterOption{WithLoadedSignature(BundleSignature(dir))}
		}, 0},
		{"no signature", func(string) []UpdaterOption { return nil }, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := freshCache(t)
			eng := NewEngine(nil, Target(0), nil, false)
			defer eng.Close()
			var rebuilds atomic.Int32
			checked := make(chan struct{}, 4)
			rebuild := func(string) ([]RouteEntry, []NamedBundle, error) {
				rebuilds.Add(1)
				return nil, nil, nil
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			// Long interval: only the immediate first check runs in this test.
			opts := append(tc.opts(dir), WithCheckInterval(time.Hour))
			StartUpdaterNamed(ctx, eng, dir, noNetwork(t), func(d string) ([]RouteEntry, []NamedBundle, error) {
				defer func() { checked <- struct{}{} }()
				return rebuild(d)
			}, opts...)

			// The first check is synchronous inside the goroutine; give it a
			// bounded window, then assert. A rebuild signals `checked`.
			select {
			case <-checked:
			case <-time.After(500 * time.Millisecond):
			}
			if n := rebuilds.Load(); n != tc.want {
				t.Errorf("first check rebuilt %d times, want %d", n, tc.want)
			}
		})
	}
}

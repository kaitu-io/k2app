package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/rule"
)

// Ticket #3889 (Android 0.4.11, mainland China): "很难连接上". The app-start
// rule prefetch (appext.PrefetchRules, 30s budget) was mid-download over a slow
// direct path when the user tapped connect. engine.Start's prefetchRulesForStart
// called EnsureBundles with its own 10s budget, but singleflight made it join
// the in-flight 30s download — Start blocked 11–28s on a CDN fetch while a
// usable cache sat on disk, and every stop/start the user tapped queued behind
// it on the platform's single-thread engine executor.
//
// The connect path must never touch the network: it seeds the embedded floor
// (local) and loads whatever is on disk. Refresh belongs to the app-start
// prefetch and the post-connect background updater.

// staleRuleCache writes a non-empty cache whose bundles.version is past
// shortTTL, i.e. the state in which the old connect path went to the network.
func staleRuleCache(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cn.krs"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bundles.version"), []byte("2026-06-01T00:00:00Z\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "bundles.version"), old, old); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestPrefetchRulesForStart_NeverTouchesNetwork: a stale-but-populated cache
// (the state in which the old path went to the network) is used as-is. One
// path for every platform — the NE-only exemption this replaces is now the rule.
func TestPrefetchRulesForStart_NeverTouchesNetwork(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := &config.ClientConfig{CacheDir: staleRuleCache(t), RuleSources: []string{srv.URL + "/"}}
	if ee := New().prefetchRulesForStart(client); ee != nil {
		t.Fatalf("prefetchRulesForStart returned %v, want nil (cache is populated)", ee)
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("connect path made %d rule-source requests, want 0", n)
	}
}

// TestPrefetchRulesForStart_NotBlockedByInflightDownload reproduces the exact
// #3889 shape: a background EnsureBundles (the app-start prefetch) is stuck on
// a slow mirror, and the connect path must still return immediately.
func TestPrefetchRulesForStart_NotBlockedByInflightDownload(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select { // hang like a throttled CDN until the test ends
		case <-release:
		case <-r.Context().Done():
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	defer close(release)

	dir := staleRuleCache(t)
	sources := []string{srv.URL + "/"}

	bgCtx, bgCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer bgCancel()
	bgDone := make(chan struct{})
	go func() {
		defer close(bgDone)
		_ = rule.EnsureBundles(bgCtx, dir, sources)
	}()
	time.Sleep(200 * time.Millisecond) // let the background fetch take the singleflight slot

	client := &config.ClientConfig{CacheDir: dir, RuleSources: sources}
	start := time.Now()
	if ee := New().prefetchRulesForStart(client); ee != nil {
		t.Fatalf("prefetchRulesForStart returned %v, want nil", ee)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("connect path blocked %v behind an in-flight rule download, want < 1s", d)
	}
	bgCancel()
	<-bgDone
}

// TestPrefetchRulesForStart_ColdStartSeedsEmbedded: an empty cache is seeded
// from the binary-embedded archive synchronously (no network) and passes.
func TestPrefetchRulesForStart_ColdStartSeedsEmbedded(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.NotFound(w, r)
	}))
	defer srv.Close()

	dir := filepath.Join(t.TempDir(), "rules")
	client := &config.ClientConfig{CacheDir: dir, RuleSources: []string{srv.URL + "/"}}
	if ee := New().prefetchRulesForStart(client); ee != nil {
		t.Fatalf("cold start returned %v, want nil (embedded floor must satisfy it)", ee)
	}
	if len(rule.ListRegions(dir)) == 0 {
		t.Fatal("cold start left no .krs on disk; embedded seed did not run")
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("cold start made %d rule-source requests, want 0", n)
	}
}

// TestPrefetchRulesForStart_NoBundlesIsFatal keeps the routes=0 guard: when
// even the embedded seed cannot produce a bundle (here: cacheDir is a regular
// file, so nothing can be written under it), Start fails loud with 504 instead
// of silently routing everything through the fallback.
func TestPrefetchRulesForStart_NoBundlesIsFatal(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	client := &config.ClientConfig{CacheDir: blocker}
	ee := New().prefetchRulesForStart(client)
	if ee == nil || ee.Code != ErrCodeRuleBundlesUnavailable {
		t.Fatalf("got %v, want EngineError code %d", ee, ErrCodeRuleBundlesUnavailable)
	}
}

// TestPrefetchRulesForStart_NotBlockedByForcedEmbedUpgrade: once the cache holds
// bundles, a background download holding .ensure.lock (here the updater's
// forced upgrade of the embedded floor, stuck on a source that never answers)
// must not delay Start. SeedFromEmbedIfEmpty answers a warm cache before it
// touches the lock, so the connect path returns at once.
func TestPrefetchRulesForStart_NotBlockedByForcedEmbedUpgrade(t *testing.T) {
	dir := t.TempDir()
	if err := rule.SeedFromEmbedIfEmpty(dir); err != nil {
		t.Fatal(err)
	}

	hit := make(chan struct{}, 1)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case hit <- struct{}{}:
		default:
		}
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() { close(release); srv.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	dlDone := make(chan struct{})
	go func() {
		defer close(dlDone)
		_ = rule.DownloadAndReloadNamed(ctx, nil, dir, []string{srv.URL + "/"}, nil)
	}()
	t.Cleanup(func() { cancel(); <-dlDone })

	select {
	case <-hit: // forced download is in flight and holds .ensure.lock
	case <-time.After(5 * time.Second):
		t.Fatal("forced upgrade of the embedded floor never reached the rule source")
	}

	start := time.Now()
	client := &config.ClientConfig{CacheDir: dir, RuleSources: []string{srv.URL + "/"}}
	if ee := New().prefetchRulesForStart(client); ee != nil {
		t.Fatalf("prefetchRulesForStart = %v during an in-flight download, want nil", ee)
	}
	if took := time.Since(start); took > time.Second {
		t.Fatalf("prefetchRulesForStart took %v during an in-flight download, want < 1s", took)
	}
}

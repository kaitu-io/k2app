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

	// httptest server that always serves the updated k2b.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		w.Write(updatedData)
	}))
	defer srv.Close()

	dir := t.TempDir()

	// Write the initial k2b: google set with only google.com (no youtube.com).
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
		{Target: TargetProxy, Sets: bindSets(initialIdx, "google")},
	}
	eng := NewEngine(initialRoutes, TargetDirect, []*Bundle{initialBundle}, false)
	defer eng.Close()

	// Confirm initial state: youtube.com does NOT match proxy.
	if got := eng.Match("youtube.com"); got != TargetDirect {
		t.Fatalf("before reload: Match(youtube.com) = %v, want %v", got, TargetDirect)
	}

	// RebuildFunc: loads bundles from cacheDir, builds routes with google→proxy.
	rebuild := func(cacheDir string) ([]RouteEntry, []*Bundle, error) {
		bundles, err := Load(cacheDir)
		if err != nil {
			return nil, nil, err
		}
		idx := Index(bundles)
		routes := []RouteEntry{
			{Target: TargetProxy, Sets: bindSets(idx, "google")},
		}
		return routes, bundles, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	StartUpdater(
		ctx,
		eng,
		dir,
		[]string{"overseas.k2b"},
		[]string{srv.URL + "/"},
		rebuild,
		WithCheckInterval(500*time.Millisecond),
	)

	// Wait up to 2s for the reload to happen.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if eng.Match("youtube.com") == TargetProxy {
			return // success
		}
		time.Sleep(50 * time.Millisecond)
	}

	t.Fatalf("after 2s: Match(youtube.com) = %v, want %v", eng.Match("youtube.com"), TargetProxy)
}

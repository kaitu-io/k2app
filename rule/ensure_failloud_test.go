package rule

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestEnsureBundles_BadRemoteVersionFailLoud pins the fail-loud guard for a
// generator-side version-format regression (k2-rules v0.1.4 emitted bare
// dates): a remote manifest whose version is not RFC3339 must (1) NOT trigger
// a download — it parses to zero time and never wins remoteNewer, so the
// local bundles keep serving (fail-safe) — and (2) emit
// DIAG: rule-manifest-bad-version, so the resulting OTA freeze is visible in
// field logs instead of silently aging the rules for months.
func TestEnsureBundles_BadRemoteVersionFailLoud(t *testing.T) {
	dir := t.TempDir()

	// Warm-but-expired cache: valid local version + one bundle, mtime pushed
	// past the TTLs so ensureOnce actually consults the manifest.
	versionPath := filepath.Join(dir, "bundles.version")
	if err := os.WriteFile(versionPath, []byte("2026-04-20T00:00:00Z\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "geoip.krs"), []byte{0x4b, 0x32, 0x42, 0x00}, 0o644); err != nil {
		t.Fatal(err)
	}
	stale := time.Now().Add(-25 * time.Hour)
	if err := os.Chtimes(versionPath, stale, stale); err != nil {
		t.Fatal(err)
	}

	var tarballHits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, manifestName) {
			_, _ = w.Write([]byte(`{"schemaVersion":2,"version":"2026-01-02","bundles":{"cn":{"sha256":"x","size":1,"ruleCount":1}}}`))
			return
		}
		tarballHits.Add(1)
		http.NotFound(w, r)
	}))
	defer srv.Close()

	var logBuf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, nil)))
	defer slog.SetDefault(prev)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := EnsureBundles(ctx, dir, []string{srv.URL + "/"}); err != nil {
		t.Fatalf("expected nil (fail-safe skip), got %v", err)
	}

	if n := tarballHits.Load(); n != 0 {
		t.Errorf("tarball fetched %d times, want 0 — a bad version must never win remoteNewer", n)
	}
	if !strings.Contains(logBuf.String(), "rule-manifest-bad-version") {
		t.Errorf("missing DIAG: rule-manifest-bad-version in logs:\n%s", logBuf.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "geoip.krs")); err != nil {
		t.Errorf("local bundle disturbed: %v", err)
	}
}

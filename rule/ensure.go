package rule

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sync/singleflight"
)

// bundlesFreshTTL is how long bundles stay valid before re-download.
// Matches typical upstream rebuild cadence (daily) with generous slack.
const bundlesFreshTTL = 24 * time.Hour

// ensureSF dedupes concurrent EnsureBundles calls by cacheDir. Two callers
// targeting the same directory share one download; see TestEnsureBundles_ConcurrentSingleflight.
var ensureSF singleflight.Group

// EnsureBundles downloads the rule bundle tarball (when not fresh) and
// atomically extracts *.k2b entries into cacheDir. When bundles.version is
// less than bundlesFreshTTL old, returns nil without any network activity.
//
// Concurrent calls with the same cacheDir are deduplicated via singleflight —
// only one download runs; others wait for its result.
//
// See docs/superpowers/specs/2026-04-20-rule-bundle-coldstart-fix-design.md
// for the full contract.
func EnsureBundles(ctx context.Context, cacheDir string, sources []string) error {
	if cacheDir == "" {
		return nil
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return fmt.Errorf("rule: create cache dir: %w", err)
	}

	if isVersionFresh(cacheDir, bundlesFreshTTL) {
		slog.Debug("rule: bundles fresh, skipping download", "dir", cacheDir)
		return nil
	}

	_, err, _ := ensureSF.Do(cacheDir, func() (any, error) {
		return nil, ensureOnce(ctx, cacheDir, sources)
	})
	return err
}

// ensureOnce performs the actual download + extract. Guarded by singleflight
// so only one runs per cacheDir at a time. B3 will implement the real body;
// today this returns a not-implemented stub after a re-check of freshness
// (a concurrent caller may have finished between our freshness check above
// and our turn inside the singleflight).
func ensureOnce(ctx context.Context, cacheDir string, sources []string) error {
	if isVersionFresh(cacheDir, bundlesFreshTTL) {
		return nil
	}
	return fmt.Errorf("rule: EnsureBundles not fully implemented (B3 pending)")
}

// isVersionFresh reports whether <cacheDir>/bundles.version exists with
// mtime within maxAge. Returns false on any error (missing, I/O, stat).
func isVersionFresh(cacheDir string, maxAge time.Duration) bool {
	info, err := os.Stat(filepath.Join(cacheDir, "bundles.version"))
	if err != nil {
		return false
	}
	return time.Since(info.ModTime()) <= maxAge
}

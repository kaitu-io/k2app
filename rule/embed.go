package rule

import (
	"bytes"
	"context"
	_ "embed"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// embeddedRules is the gzipped tarball of all .krs rule bundles, baked into
// the binary at build time. A release build is expected to overwrite
// rule/embed/krs.tar.gz with the full production archive first; the committed
// copy is a small cn-baseline placeholder so the package always compiles and
// dev/test builds have real bundles.
//
// That overwrite is currently out-of-band: no Makefile target, script, or
// workflow in this repo refreshes the archive. (This comment used to name a
// `make fetch-rules-embed` target — it does not exist, and citing it sent
// readers looking for a build step that was never there.)
//
// seedFromEmbed (ensure.go) extracts this into the cache on true cold start —
// instant, offline, censorship-proof. The CDN remains the source of newer
// rules via the background updater.
//
//go:embed embed/krs.tar.gz
var embeddedRules []byte

// SeedFromEmbedIfEmpty seeds cacheDir from the embedded archive when it holds
// zero .krs (true cold start); no-op otherwise. Offline and instant. Safe
// across processes (cache flock) and goroutines. Call BEFORE EnsureBundles so
// a cold start gets a working rule floor without waiting on the network — the
// subsequent EnsureBundles then sees a fresh cache and skips the download.
func SeedFromEmbedIfEmpty(cacheDir string) error {
	if cacheDir == "" {
		return nil
	}
	if countKRS(cacheDir) > 0 {
		return nil // warm — fast path, no lock
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	release, err := acquireLock(ctx, filepath.Join(cacheDir, ".ensure.lock"))
	if err != nil {
		return err
	}
	defer release()
	if countKRS(cacheDir) > 0 {
		return nil // another process/goroutine seeded while we waited
	}
	if err := seedFromEmbed(cacheDir); err != nil {
		return err
	}
	slog.Info("rule: seeded cache from embedded archive", "dir", cacheDir)
	return nil
}

// embedMarker is the token writeLocalVersion (manifest.go) appends to
// bundles.version to record that the cache was populated from the
// binary-embedded archive rather than a CDN download. An embed seed writes
// "<version> [embedded]"; a CDN download writes "<version>" (no marker).
var embedMarker = []byte("[embedded]")

// wasEmbedSeeded reports whether cacheDir's bundles.version was written by the
// embedded-archive seed (seedFromEmbed) rather than a CDN download.
//
// The background updater uses this to FORCE a CDN refresh that upgrades the
// build-time floor instead of honoring the 24h freshness gate. Without it a
// freshly-seeded device reads as "fresh" and serves the embedded baseline
// untouched for a whole TTL even on a healthy network — the floor never lifts
// to the CDN ceiling. A successful CDN download rewrites bundles.version
// without the marker, so the force is self-clearing: it applies only until the
// floor is upgraded, then the normal gated cadence resumes.
func wasEmbedSeeded(cacheDir string) bool {
	if cacheDir == "" {
		return false
	}
	data, err := os.ReadFile(filepath.Join(cacheDir, "bundles.version"))
	if err != nil {
		return false
	}
	return bytes.Contains(data, embedMarker)
}

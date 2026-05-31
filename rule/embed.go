package rule

import (
	"context"
	_ "embed"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// embeddedRules is the gzipped tarball of all .krs rule bundles, baked into
// the binary at build time. The build pipeline (`make fetch-rules-embed`)
// overwrites rule/embed/all.krs.tar.gz with the full production archive before
// a release build; the committed copy is a small cn-baseline placeholder so the
// package always compiles and dev/test builds have real bundles.
//
// seedFromEmbed (ensure.go) extracts this into the cache on true cold start —
// instant, offline, censorship-proof. The CDN remains the source of newer
// rules via the background updater.
//
//go:embed embed/all.krs.tar.gz
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

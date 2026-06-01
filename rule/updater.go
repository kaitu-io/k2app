package rule

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/kaitu-io/k2-rules/krs"
	"github.com/kaitu-io/k2/safego"
)

// defaultCheckInterval is how often a connected engine re-checks for new
// rule bundles. Each tick calls DownloadAndReload, which is short-circuited
// by the 24h bundlesFreshTTL gate plus singleflight — so a 1h tick is mostly
// noop os.Stat calls with one real download per day when the upstream daily
// build emits a new tarball. The aggressive tick rate ensures the engine
// hot-reloads within an hour of a new bundle landing on disk. Exception: a
// cache still on the binary-embedded floor (wasEmbedSeeded) is force-refreshed
// on the first tick so the floor lifts to the CDN ceiling promptly rather than
// after the full TTL.
const defaultCheckInterval = 1 * time.Hour

// UpdaterOption configures the updater.
type UpdaterOption func(*updaterConfig)

type updaterConfig struct {
	interval time.Duration
}

// WithCheckInterval overrides the default 1h check interval.
func WithCheckInterval(d time.Duration) UpdaterOption {
	return func(c *updaterConfig) {
		c.interval = d
	}
}

// RebuildFunc loads bundles from cacheDir and rebuilds the engine's routes.
// Called by the updater after downloading new bundles.
// Returns the new routes and bundles for Engine.Reload().
type RebuildFunc func(cacheDir string) ([]RouteEntry, []*krs.Bundle, error)

// RebuildFuncNamed is the NamedBundle variant of RebuildFunc. The engine's
// background refresh builds NamedBundles (region + bundle) so the rebuild
// closure can recompute match.region routes from the refreshed set.
type RebuildFuncNamed func(cacheDir string) ([]RouteEntry, []NamedBundle, error)

// DownloadAndReload fetches the rule-bundle tarball when the cache is older
// than bundlesFreshTTL, then optionally rebuilds and hot-swaps the engine's
// routes. Safe for concurrent callers — singleflight inside EnsureBundles
// dedups. If engine or rebuild is nil, only the download side runs.
//
// When the cache was populated from the binary-embedded floor
// (wasEmbedSeeded), the download is FORCED past the freshness gate so the
// build-time baseline lifts to the CDN ceiling on the first background tick
// instead of staying frozen for a whole TTL. A successful download clears the
// embed marker, so subsequent ticks fall back to the gated 24h cadence.
func DownloadAndReload(
	ctx context.Context,
	engine *Engine,
	cacheDir string,
	sources []string,
	rebuild RebuildFunc,
) error {
	if err := ensureBundles(ctx, cacheDir, sources, wasEmbedSeeded(cacheDir)); err != nil {
		return err
	}
	if engine == nil || rebuild == nil {
		return nil
	}
	routes, newBundles, err := rebuild(cacheDir)
	if err != nil {
		return fmt.Errorf("rule: rebuild after download: %w", err)
	}
	return engine.Reload(routes, newBundles)
}

// StartUpdater launches a background goroutine that periodically refreshes
// rule bundles (via EnsureBundles tarball fetch) and hot-swaps the engine's
// routes. An immediate first check runs; subsequent checks honor the
// configured interval (default 1h). Stops when ctx is cancelled.
func StartUpdater(
	ctx context.Context,
	engine *Engine,
	cacheDir string,
	sources []string,
	rebuild RebuildFunc,
	opts ...UpdaterOption,
) {
	cfg := updaterConfig{interval: defaultCheckInterval}
	for _, o := range opts {
		o(&cfg)
	}

	safego.Go(func() {
		if err := DownloadAndReload(ctx, engine, cacheDir, sources, rebuild); err != nil {
			slog.Warn("rule: updater initial check failed", "err", err)
		}

		ticker := time.NewTicker(cfg.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := DownloadAndReload(ctx, engine, cacheDir, sources, rebuild); err != nil {
					slog.Warn("rule: updater cycle failed", "err", err)
				}
			}
		}
	})
}

// DownloadAndReloadNamed is the NamedBundle variant of DownloadAndReload.
// Runs the download, the rebuild, then Engine.ReloadNamed so the engine's
// region→bundle map stays in sync with disk. Like DownloadAndReload it FORCES
// the download past the freshness gate when the cache is an embed-seeded floor
// (wasEmbedSeeded), so a cold-started device upgrades to the CDN ruleset on the
// first background tick rather than after the 24h TTL.
func DownloadAndReloadNamed(
	ctx context.Context,
	engine *Engine,
	cacheDir string,
	sources []string,
	rebuild RebuildFuncNamed,
) error {
	if err := ensureBundles(ctx, cacheDir, sources, wasEmbedSeeded(cacheDir)); err != nil {
		return err
	}
	if engine == nil || rebuild == nil {
		return nil
	}
	routes, named, err := rebuild(cacheDir)
	if err != nil {
		return fmt.Errorf("rule: rebuild after download: %w", err)
	}
	return engine.ReloadNamed(routes, named)
}

// StartUpdaterNamed is the NamedBundle variant of StartUpdater.
func StartUpdaterNamed(
	ctx context.Context,
	engine *Engine,
	cacheDir string,
	sources []string,
	rebuild RebuildFuncNamed,
	opts ...UpdaterOption,
) {
	cfg := updaterConfig{interval: defaultCheckInterval}
	for _, o := range opts {
		o(&cfg)
	}

	safego.Go(func() {
		if err := DownloadAndReloadNamed(ctx, engine, cacheDir, sources, rebuild); err != nil {
			slog.Warn("rule: updater initial check failed", "err", err)
		}

		ticker := time.NewTicker(cfg.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := DownloadAndReloadNamed(ctx, engine, cacheDir, sources, rebuild); err != nil {
					slog.Warn("rule: updater cycle failed", "err", err)
				}
			}
		}
	})
}

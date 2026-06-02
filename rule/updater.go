package rule

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/kaitu-io/k2/safego"
)

// defaultCheckInterval is how often a connected engine re-checks for new
// rule bundles. Each tick calls DownloadAndReloadNamed, which is short-circuited
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

// RebuildFuncNamed loads bundles from cacheDir and rebuilds the engine's
// routes, returning NamedBundles (region + bundle) so the rebuild closure can
// recompute match.region routes from the refreshed set. Called by the updater
// after downloading new bundles; returns the new routes and named bundles for
// Engine.ReloadNamed().
type RebuildFuncNamed func(cacheDir string) ([]RouteEntry, []NamedBundle, error)

// DownloadAndReloadNamed fetches the rule-bundle tarball when the cache is
// older than bundlesFreshTTL, then optionally rebuilds and hot-swaps the
// engine's routes via Engine.ReloadNamed so the engine's region→bundle map
// stays in sync with disk. Safe for concurrent callers — singleflight inside
// ensureBundles dedups. If engine or rebuild is nil, only the download side
// runs. It FORCES
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

// StartUpdaterNamed launches a background goroutine that periodically refreshes
// rule bundles (via ensureBundles tarball fetch) and hot-swaps the engine's
// routes through DownloadAndReloadNamed. An immediate first check runs;
// subsequent checks honor the configured interval (default 1h). Stops when ctx
// is cancelled.
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

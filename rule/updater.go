package rule

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/kaitu-io/k2/safego"
)

const defaultCheckInterval = 24 * time.Hour

// UpdaterOption configures the updater.
type UpdaterOption func(*updaterConfig)

type updaterConfig struct {
	interval time.Duration
}

// WithCheckInterval overrides the default 24h check interval (for testing).
func WithCheckInterval(d time.Duration) UpdaterOption {
	return func(c *updaterConfig) {
		c.interval = d
	}
}

// RebuildFunc loads bundles from cacheDir and rebuilds the engine's routes.
// Called by the updater after downloading new bundles.
// Returns the new routes and bundles for Engine.Reload().
type RebuildFunc func(cacheDir string) ([]RouteEntry, []*Bundle, error)

// DownloadAndReload downloads bundles (skipping if fresh), then optionally
// rebuilds and hot-swaps the engine's rules.
// If engine or rebuild is nil, only downloads (no reload).
// skipIfFresh > 0 skips the download entirely when all files have mtime within that duration.
func DownloadAndReload(
	ctx context.Context,
	engine *Engine,
	cacheDir string,
	bundles []string,
	sources []string,
	rebuild RebuildFunc,
	skipIfFresh time.Duration,
) error {
	if len(bundles) == 0 || cacheDir == "" {
		return nil
	}
	if skipIfFresh > 0 && IsFresh(cacheDir, bundles, skipIfFresh) {
		slog.Debug("rule: bundles fresh, skipping download")
		return nil
	}
	if err := Download(ctx, cacheDir, bundles, sources); err != nil {
		return fmt.Errorf("rule: download: %w", err)
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

// StartUpdater launches a background goroutine that periodically checks for
// bundle updates. When new bundles are downloaded, it calls rebuild to get
// new routes and bundles, then swaps them into the engine via Reload().
// An immediate first check runs on start (skipped if bundles are fresh).
// Stops when ctx is cancelled.
func StartUpdater(
	ctx context.Context,
	engine *Engine,
	cacheDir string,
	bundles []string,
	sources []string,
	rebuild RebuildFunc,
	opts ...UpdaterOption,
) {
	cfg := updaterConfig{interval: defaultCheckInterval}
	for _, o := range opts {
		o(&cfg)
	}

	if len(bundles) == 0 {
		return
	}

	safego.Go(func() {
		// Immediate first check — skip if bundles are already fresh.
		// Handles the case where user switches country: old cache may lack
		// the new bundle, so this downloads it right after connection and
		// hot-reloads within seconds.
		if err := DownloadAndReload(ctx, engine, cacheDir, bundles, sources, rebuild, defaultCheckInterval); err != nil {
			slog.Warn("rule: updater initial check failed", "err", err)
		}

		ticker := time.NewTicker(cfg.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := DownloadAndReload(ctx, engine, cacheDir, bundles, sources, rebuild, 0); err != nil {
					slog.Warn("rule: updater cycle failed", "err", err)
				}
			}
		}
	})
}

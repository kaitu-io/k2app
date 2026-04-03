package rule

import (
	"context"
	"log/slog"
	"time"
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

// StartUpdater launches a background goroutine that periodically checks for
// bundle updates. When new bundles are downloaded, it calls rebuild to get
// new routes and bundles, then swaps them into the engine via Reload().
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

	go func() {
		ticker := time.NewTicker(cfg.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := Download(ctx, cacheDir, bundles, sources); err != nil {
					slog.Warn("rule: updater download failed", "err", err)
					continue
				}
				routes, newBundles, err := rebuild(cacheDir)
				if err != nil {
					slog.Warn("rule: updater rebuild failed", "err", err)
					continue
				}
				if err := engine.Reload(routes, newBundles); err != nil {
					slog.Warn("rule: updater reload failed", "err", err)
					continue
				}
				slog.Info("rule: updater reload complete", "routes", len(routes))
			}
		}
	}()
}

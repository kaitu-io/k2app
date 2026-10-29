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
	interval  time.Duration
	loadedSig string
}

// WithCheckInterval overrides the default 1h check interval.
func WithCheckInterval(d time.Duration) UpdaterOption {
	return func(c *updaterConfig) {
		c.interval = d
	}
}

// WithLoadedSignature tells the updater which on-disk bundle set the engine
// was built from (BundleSignature taken just before the engine loaded it). The
// updater then reloads only when the disk set differs. Without it the first
// check always reloads (the pre-signature behavior).
func WithLoadedSignature(sig string) UpdaterOption {
	return func(c *updaterConfig) {
		c.loadedSig = sig
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
		loaded := cfg.loadedSig
		var err error
		if loaded, err = refreshAndReload(ctx, engine, cacheDir, sources, rebuild, loaded); err != nil {
			slog.Warn("rule: updater initial check failed", "err", err)
		}

		ticker := time.NewTicker(cfg.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if loaded, err = refreshAndReload(ctx, engine, cacheDir, sources, rebuild, loaded); err != nil {
					slog.Warn("rule: updater cycle failed", "err", err)
				}
			}
		}
	})
}

// refreshAndReload is one updater check: refresh the cache from the rule
// sources (ensureBundles; forced past the freshness gate while the cache is
// the embedded floor), then hot-reload the engine iff the on-disk bundle set
// differs from loaded — the signature of the set the engine currently serves.
// Returns the signature the engine serves afterwards.
//
// The reload decision is "disk differs from engine", not "this call
// downloaded": the connect path never downloads, so the app-start prefetch can
// swap a new set onto disk after the engine loaded the old one, and by the
// time this runs the cache reads fresh and ensureBundles fetches nothing. For
// the same reason a failed fetch still reloads a disk set someone else changed.
// An unchanged set never reloads — each reload is a full heap rebuild.
//
// The signature is taken BEFORE rebuild reads the files, so a swap racing the
// rebuild leaves the returned signature behind the disk and the next check
// reloads again; it can never record a set the engine does not serve.
func refreshAndReload(
	ctx context.Context,
	engine *Engine,
	cacheDir string,
	sources []string,
	rebuild RebuildFuncNamed,
	loaded string,
) (string, error) {
	ensureErr := ensureBundles(ctx, cacheDir, sources, wasEmbedSeeded(cacheDir))
	if engine == nil || rebuild == nil {
		return loaded, ensureErr
	}
	if ctx.Err() != nil {
		return loaded, ctx.Err() // engine stopping — never reload a closed engine
	}
	sig := BundleSignature(cacheDir)
	if sig == "" || sig == loaded {
		// "" = no bundle on disk right now (mid-swap or wiped): never swap the
		// engine's working set for an empty one.
		return loaded, ensureErr
	}
	routes, named, err := rebuild(cacheDir)
	if err != nil {
		return loaded, fmt.Errorf("rule: rebuild after download: %w", err)
	}
	if err := engine.ReloadNamed(routes, named); err != nil {
		return loaded, err
	}
	return sig, ensureErr
}

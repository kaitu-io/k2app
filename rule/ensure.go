package rule

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sync/singleflight"
)

// bundlesFreshTTL is how long bundles stay valid before re-download.
// Matches typical upstream rebuild cadence (daily) with generous slack.
// It remains the hard backstop for when the manifest is unreachable.
const bundlesFreshTTL = 24 * time.Hour

// bundlesShortTTL is the window during which the client trusts the local cache
// without even fetching the manifest. Matches the updater tick — below it, a
// connect is zero-network (fast path); above it, the cheap manifest check runs
// to pick up a newer version. bundlesFreshTTL remains the hard backstop for
// when the manifest is unreachable.
const bundlesShortTTL = 1 * time.Hour

const (
	tarballName    = "krs.tar.gz"
	maxTarballSize = 5 << 20 // 5 MB
)

// ensureSF dedupes concurrent EnsureBundles calls by cacheDir. Two callers
// targeting the same directory share one download; see TestEnsureBundles_ConcurrentSingleflight.
var ensureSF singleflight.Group

// EnsureBundles downloads the rule bundle tarball (when not fresh) and
// atomically extracts *.krs entries into cacheDir. When bundles.version is
// less than bundlesFreshTTL old, returns nil without any network activity.
//
// Concurrent calls with the same cacheDir are deduplicated via singleflight —
// only one download runs; others wait for its result.
//
// See docs/superpowers/specs/2026-04-20-rule-bundle-coldstart-fix-design.md
// for the full contract.
func EnsureBundles(ctx context.Context, cacheDir string, sources []string) error {
	return ensureBundles(ctx, cacheDir, sources, false)
}

// ensureBundles is EnsureBundles with an explicit force flag. force=true
// bypasses the freshness gate (both the pre-lock and post-lock checks) so the
// background updater can upgrade an embed-seeded floor that would otherwise
// read as fresh for the full TTL. The connect-path caller (engine.Start) uses
// force=false so a warm cache stays instant; only DownloadAndReload(Named)
// forces, and only while wasEmbedSeeded is still true.
func ensureBundles(ctx context.Context, cacheDir string, sources []string, force bool) error {
	if cacheDir == "" {
		return nil
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return fmt.Errorf("rule: create cache dir: %w", err)
	}

	// Fast path: within the short TTL, trust local — zero network. (H4)
	if !force && isVersionFresh(cacheDir, bundlesShortTTL) {
		slog.Debug("rule: bundles within shortTTL, skipping manifest check", "dir", cacheDir)
		return nil
	}

	_, err, _ := ensureSF.Do(cacheDir, func() (any, error) {
		return nil, ensureOnce(ctx, cacheDir, sources, force)
	})
	return err
}

// ensureOnce performs the actual download + extract. Guarded by singleflight
// (in-process dedup) plus flock (cross-process dedup — iOS Plugin and NE run
// in separate processes and both call EnsureBundles on the same cacheDir).
// Re-checks freshness after acquiring the lock in case another process just
// finished writing bundles.version.
func ensureOnce(ctx context.Context, cacheDir string, sources []string, force bool) error {
	if !force && isVersionFresh(cacheDir, bundlesShortTTL) {
		return nil
	}

	release, err := acquireLock(ctx, filepath.Join(cacheDir, ".ensure.lock"))
	if err != nil {
		return fmt.Errorf("rule: acquire lock: %w", err)
	}
	defer release()

	if !force && isVersionFresh(cacheDir, bundlesShortTTL) {
		return nil
	}

	// Version identity: fetch the manifest, pick newest, pin its mirror.
	remote, srcURL, mErr := raceManifest(ctx, sources)
	if mErr != nil {
		// Manifest unreachable → legacy behavior: honor the 24h backstop, else
		// blind-download (no manifest verify) so offline/CDN-down never regresses.
		if !force && isVersionFresh(cacheDir, bundlesFreshTTL) {
			return nil
		}
		slog.Warn("DIAG: rule-manifest-unreachable", "err", mErr.Error(), "dir", cacheDir)
		// No manifest winner → no pin. Race all sources (Happy Eyeballs).
		return downloadExtractSwap(ctx, cacheDir, sources, "", "", nil)
	}

	localVer := readLocalManifestVersion(cacheDir)
	if !force && !remoteNewer(remote.Version, localVer) && localMatchesManifest(cacheDir, remote) {
		// Same-or-older remote and local shas already match → fresh. Touch the
		// version file so shortTTL resets and we don't re-fetch the manifest
		// every connect for the next hour.
		_ = touchVersionFile(cacheDir)
		return nil
	}

	// Pin the tarball download to the manifest's winning source: that mirror's
	// shas are what we'll verify against, so racing other (possibly stale)
	// mirrors only risks handing us a mismatched tarball. downloadExtractSwap →
	// downloadRaceTarball fetches the pinned mirror alone first, then races the
	// remaining sources as fallbacks (order-independent), so no pre-ordering of
	// sources is needed here. (H3)
	return downloadExtractSwap(ctx, cacheDir, sources, srcURL, remote.Version, remote)
}

// downloadExtractSwap downloads the tarball, extracts to a temp dir, rejects an
// empty extraction, verifies against the manifest (when non-nil), atomic-swaps,
// and records manifestVersion. A verify failure keeps the prior cache.
//
// pinnedSrc selects the download strategy: when non-empty (manifest path), the
// pinned mirror is fetched in isolation first and the rest of sources are only
// raced as fallbacks if the pin fails — so the happy path contacts exactly the
// mirror whose manifest we'll verify against. When empty (manifest-unreachable
// legacy path), all sources race via Happy Eyeballs.
func downloadExtractSwap(ctx context.Context, cacheDir string, sources []string, pinnedSrc, manifestVersion string, m *Manifest) error {
	tmpDir := filepath.Join(cacheDir, ".tmp")
	_ = os.RemoveAll(tmpDir) // clear stale crash leftover
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	tarballPath := filepath.Join(tmpDir, tarballName)
	if _, err := downloadTarball(ctx, tarballPath, sources, pinnedSrc); err != nil {
		return fmt.Errorf("rule: download tarball: %w", err)
	}
	newDir := filepath.Join(tmpDir, "new")
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		return err
	}
	if err := extractTarball(tarballPath, newDir); err != nil {
		return fmt.Errorf("rule: extract tarball: %w", err)
	}

	// Reject a download that produced zero .krs bundles before touching the
	// live cache. A stale CDN mirror serving the pre-Plan-A tarball (only
	// .k2b/.yaml) extracts no .krs; committing it would write a fresh
	// bundles.version over an empty set, poisoning the cache and silently
	// degrading region routes to all-proxy. Treat as a failed download so the
	// engine falls back to the prior cache (or fails loud).
	if countKRS(newDir) == 0 {
		slog.Warn("DIAG: rule-bundle-empty", "cacheDir", cacheDir,
			"reason", "downloaded tarball extracted 0 .krs (stale mirror or format skew)")
		return errors.New("rule: tarball contained 0 .krs bundles (stale mirror or format skew)")
	}

	if m != nil {
		if err := verifyAgainstManifest(newDir, m); err != nil {
			// Mirror mid-propagation / corruption: keep the prior cache.
			return fmt.Errorf("rule: manifest verify failed, keeping prior cache: %w", err)
		}
	}

	if err := atomicSwap(cacheDir, newDir); err != nil {
		return fmt.Errorf("rule: atomic swap: %w", err)
	}
	return writeLocalVersion(cacheDir, manifestVersion, false)
}

// localMatchesManifest reports whether every region the manifest covers AND
// that exists locally has a matching sha. Missing-local regions don't block
// (they'd be picked up at connect) — see engine.Start's 504 missing-region gate
// (DIAG: rule-bundle-missing-region). Used to decide "no download needed".
func localMatchesManifest(cacheDir string, m *Manifest) bool {
	for region, mb := range m.Bundles {
		p := filepath.Join(cacheDir, region+".krs")
		data, err := os.ReadFile(p)
		if err != nil {
			continue // not present locally — not a mismatch here
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != mb.SHA256 {
			return false
		}
	}
	return true
}

// touchVersionFile resets bundles.version mtime so the shortTTL fast path holds
// for another window without re-fetching the manifest.
func touchVersionFile(cacheDir string) error {
	now := time.Now()
	return os.Chtimes(filepath.Join(cacheDir, "bundles.version"), now, now)
}

// downloadRaceTarball performs the actual fetch, honoring the pin. With a
// non-empty pinnedSrc it tries that one mirror alone first; on failure it races
// the remaining sources. With an empty pinnedSrc it races every source.
func downloadRaceTarball(ctx context.Context, dest string, sources []string, pinnedSrc string) (downloadResult, error) {
	if pinnedSrc == "" {
		return raceDownload(ctx, dest, tarballName, sources)
	}
	// Pinned: attempt the winning mirror in isolation first.
	res, err := raceDownload(ctx, dest, tarballName, []string{pinnedSrc})
	if err == nil && res != downloadResultFailed {
		return res, nil
	}
	// Pinned mirror failed — fall back to the rest (excluding the pin we tried).
	rest := make([]string, 0, len(sources))
	for _, s := range sources {
		if s != pinnedSrc {
			rest = append(rest, s)
		}
	}
	if len(rest) == 0 {
		return res, err
	}
	slog.Warn("DIAG: rule-pin-fallback", "pinned", pinnedSrc, "fallbacks", len(rest))
	return raceDownload(ctx, dest, tarballName, rest)
}

// downloadTarball fetches the rule bundle tarball, enforces the size cap, and
// returns the hex-encoded SHA-256 of the file on success.
//
// When pinnedSrc is non-empty it is fetched in isolation first; only if that
// fetch fails do the remaining sources race as fallbacks (Happy Eyeballs). This
// keeps the manifest path from contacting — and possibly accepting bytes from —
// a stale mirror other than the one whose manifest we verify against. When
// pinnedSrc is empty, all sources race.
func downloadTarball(ctx context.Context, dest string, sources []string, pinnedSrc string) (string, error) {
	res, err := downloadRaceTarball(ctx, dest, sources, pinnedSrc)
	if err != nil {
		return "", err
	}
	if res == downloadResultFailed {
		return "", errors.New("all sources failed")
	}

	info, err := os.Stat(dest)
	if err != nil {
		return "", err
	}
	if info.Size() > maxTarballSize {
		_ = os.Remove(dest)
		return "", fmt.Errorf("tarball too large: %d bytes (max %d)", info.Size(), maxTarballSize)
	}

	f, err := os.Open(dest)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// extractTarball unpacks .krs and .yaml entries from the gzipped tarball at
// tarballPath into newDir, stripping any directory components (defense against
// path traversal). Other entry types and non-regular files are silently ignored.
func extractTarball(tarballPath, newDir string) error {
	f, err := os.Open(tarballPath)
	if err != nil {
		return err
	}
	defer f.Close()
	return extractTarballReader(f, newDir)
}

// extractTarballReader unpacks .krs and .yaml entries from a gzipped tar stream
// into newDir, stripping directory components (path-traversal defense). Other
// entry types and non-regular files are ignored. Shared by CDN downloads
// (extractTarball) and the binary-embedded seed (seedFromEmbed).
func extractTarballReader(r io.Reader, newDir string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		name := filepath.Base(hdr.Name)
		if !strings.HasSuffix(name, ".krs") && !strings.HasSuffix(name, ".yaml") {
			continue
		}
		dst := filepath.Join(newDir, name)
		out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, io.LimitReader(tr, maxTarballSize)); err != nil {
			out.Close()
			return err
		}
		if err := out.Close(); err != nil {
			return err
		}
	}
	return nil
}

// seedFromEmbed extracts the binary-embedded krs.tar.gz into cacheDir.
// Extracts to a temp dir then atomicSwap so a concurrent reader never sees a
// half-written cache. Writes bundles.version stamped "embedded" so the cache
// reads fresh. Lock-agnostic: the public SeedFromEmbedIfEmpty wraps it in the
// cache flock.
func seedFromEmbed(cacheDir string) error {
	tmpDir := filepath.Join(cacheDir, ".seed-tmp")
	_ = os.RemoveAll(tmpDir)
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	if err := extractTarballReader(bytes.NewReader(embeddedRules), tmpDir); err != nil {
		return fmt.Errorf("rule: extract embedded: %w", err)
	}
	if countKRS(tmpDir) == 0 {
		return errors.New("rule: embedded archive contained 0 .krs (broken placeholder or build-fetch)")
	}
	if err := atomicSwap(cacheDir, tmpDir); err != nil {
		return fmt.Errorf("rule: seed atomic swap: %w", err)
	}

	// Stamp the "embedded" sentinel version with the embed marker so wasEmbedSeeded
	// (embed.go) can tell this floor apart from a CDN download and force an upgrade.
	// "embedded" parses to the zero time, so any real CDN version always out-ranks
	// it and the updater lifts the floor to the CDN ceiling.
	return writeLocalVersion(cacheDir, "embedded", true)
}

// atomicSwap renames each .krs and .yaml from newDir into cacheDir, then
// removes any .krs/.yaml/.k2b files in cacheDir that were NOT part of the
// new set (orphan pruning). .k2b is included in the prune filter to drop
// leftover bundles from pre-Plan-A binaries — the new extractor no longer
// produces .k2b files, so any encountered are stale.
func atomicSwap(cacheDir, newDir string) error {
	entries, err := os.ReadDir(newDir)
	if err != nil {
		return err
	}
	newNames := map[string]struct{}{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".krs") && !strings.HasSuffix(name, ".yaml") {
			continue
		}
		newNames[name] = struct{}{}
		if err := os.Rename(
			filepath.Join(newDir, name),
			filepath.Join(cacheDir, name),
		); err != nil {
			return err
		}
	}

	existing, err := os.ReadDir(cacheDir)
	if err != nil {
		return nil // best effort — a stat failure here doesn't invalidate the rename
	}
	for _, e := range existing {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".krs") &&
			!strings.HasSuffix(name, ".yaml") &&
			!strings.HasSuffix(name, ".k2b") {
			continue
		}
		if _, keep := newNames[name]; !keep {
			_ = os.Remove(filepath.Join(cacheDir, name))
		}
	}
	return nil
}

// countKRS returns the number of regular *.krs files directly under dir.
// Used to validate an extraction (or a cache) actually holds usable bundles —
// a "fresh" bundles.version marker is meaningless without them.
func countKRS(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".krs") {
			n++
		}
	}
	return n
}

// isVersionFresh reports whether the cache is genuinely warm: <cacheDir>/
// bundles.version exists with mtime within maxAge AND at least one .krs bundle
// is actually present. The .krs check is what prevents a poisoned cache (fresh
// marker, zero bundles — from iOS App-Group eviction or a pre-guard empty
// extraction) from short-circuiting the download forever; without it such a
// cache silently degrades region routes to all-proxy for the whole TTL.
// Returns false on any error (missing, I/O, stat).
func isVersionFresh(cacheDir string, maxAge time.Duration) bool {
	info, err := os.Stat(filepath.Join(cacheDir, "bundles.version"))
	if err != nil {
		return false
	}
	if time.Since(info.ModTime()) > maxAge {
		return false
	}
	return countKRS(cacheDir) > 0
}

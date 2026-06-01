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
const bundlesFreshTTL = 24 * time.Hour

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

	if !force && isVersionFresh(cacheDir, bundlesFreshTTL) {
		slog.Debug("rule: bundles fresh, skipping download", "dir", cacheDir)
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
	if !force && isVersionFresh(cacheDir, bundlesFreshTTL) {
		return nil
	}

	release, err := acquireLock(ctx, filepath.Join(cacheDir, ".ensure.lock"))
	if err != nil {
		return fmt.Errorf("rule: acquire lock: %w", err)
	}
	defer release()

	if !force && isVersionFresh(cacheDir, bundlesFreshTTL) {
		return nil
	}

	tmpDir := filepath.Join(cacheDir, ".tmp")
	_ = os.RemoveAll(tmpDir) // clear stale crash leftover
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	tarballPath := filepath.Join(tmpDir, tarballName)
	sha, err := downloadTarball(ctx, tarballPath, sources)
	if err != nil {
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
	// bundles.version over an empty set, poisoning the cache for the whole TTL
	// and silently degrading region routes to all-proxy. Treat as a failed
	// download so the engine falls back to the prior cache (or fails loud).
	if countKRS(newDir) == 0 {
		slog.Warn("DIAG: rule-bundle-empty", "cacheDir", cacheDir,
			"reason", "downloaded tarball extracted 0 .krs (stale mirror or format skew)")
		return errors.New("rule: tarball contained 0 .krs bundles (stale mirror or format skew)")
	}

	if err := atomicSwap(cacheDir, newDir); err != nil {
		return fmt.Errorf("rule: atomic swap: %w", err)
	}

	stamp := time.Now().UTC().Format(time.RFC3339)
	line := fmt.Sprintf("%s sha256:%s\n", stamp, sha)
	return os.WriteFile(filepath.Join(cacheDir, "bundles.version"), []byte(line), 0o644)
}

// downloadTarball fetches the rule bundle tarball via raceDownload, enforces
// the size cap, and returns the hex-encoded SHA-256 of the file on success.
func downloadTarball(ctx context.Context, dest string, sources []string) (string, error) {
	res, err := raceDownload(ctx, dest, tarballName, sources)
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

	// Stamp with the embed marker (embedMarker, embed.go) so wasEmbedSeeded can
	// later tell this floor apart from a CDN download and force an upgrade. Both
	// sides share embedMarker so the write/read can't drift.
	sum := sha256.Sum256(embeddedRules)
	stamp := time.Now().UTC().Format(time.RFC3339)
	line := fmt.Sprintf("%s %s%s\n", stamp, embedMarker, hex.EncodeToString(sum[:]))
	return os.WriteFile(filepath.Join(cacheDir, "bundles.version"), []byte(line), 0o644)
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

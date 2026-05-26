package rule

import (
	"archive/tar"
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
	tarballName    = "k2-rules.tar.gz"
	maxTarballSize = 5 << 20 // 5 MB
)

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
// (in-process dedup) plus flock (cross-process dedup — iOS Plugin and NE run
// in separate processes and both call EnsureBundles on the same cacheDir).
// Re-checks freshness after acquiring the lock in case another process just
// finished writing bundles.version.
func ensureOnce(ctx context.Context, cacheDir string, sources []string) error {
	if isVersionFresh(cacheDir, bundlesFreshTTL) {
		return nil
	}

	release, err := acquireLock(ctx, filepath.Join(cacheDir, ".ensure.lock"))
	if err != nil {
		return fmt.Errorf("rule: acquire lock: %w", err)
	}
	defer release()

	if isVersionFresh(cacheDir, bundlesFreshTTL) {
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

// extractTarball unpacks .k2b and .yaml entries from the gzipped tarball at
// tarballPath into newDir, stripping any directory components (defense against
// path traversal). Other entry types and non-regular files are silently ignored.
func extractTarball(tarballPath, newDir string) error {
	f, err := os.Open(tarballPath)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
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
		if !strings.HasSuffix(name, ".k2b") && !strings.HasSuffix(name, ".yaml") {
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

// atomicSwap renames each .k2b and .yaml from newDir into cacheDir, then
// removes any .k2b/.yaml files in cacheDir that were NOT part of the new set
// (orphan pruning).
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
		if !strings.HasSuffix(name, ".k2b") && !strings.HasSuffix(name, ".yaml") {
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
		if !strings.HasSuffix(name, ".k2b") && !strings.HasSuffix(name, ".yaml") {
			continue
		}
		if _, keep := newNames[name]; !keep {
			_ = os.Remove(filepath.Join(cacheDir, name))
		}
	}
	return nil
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

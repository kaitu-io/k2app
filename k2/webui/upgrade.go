//go:build linux

package webui

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// UpgradeConfig describes how to fetch, download, replace, and restart a
// Kaitu Linux binary (k2r or k2). Each binary chooses its own CDN path,
// state directory, and restart mechanism via this config.
type UpgradeConfig struct {
	// BinaryName is the logical binary name used in the CDN URL and
	// checksums.txt entry, e.g. "k2r" for files like "k2r-linux-amd64".
	// The full filename is "<BinaryName>-linux-<GOARCH>".
	BinaryName string

	// BinaryPath is the absolute path to the running executable. If empty,
	// the Upgrader resolves it via os.Executable() + filepath.EvalSymlinks.
	BinaryPath string

	// StateDir is the directory where update-state.json is persisted for
	// crash recovery. Created with mode 0700 as needed.
	StateDir string

	// ReleaseEndpoints is the ordered list of CDN base URLs tried by
	// FetchLatest and ReplaceBinary; the first success wins.
	ReleaseEndpoints []string

	// RestartFunc is invoked after a successful Apply() replace. Typical
	// implementations run `systemctl restart <unit>` or OpenWRT procd.
	// If nil, Apply() leaves the replaced binary in place without restart.
	RestartFunc func()
}

// UpgradeInfo holds the latest release version and per-binary SHA256 checksums.
type UpgradeInfo struct {
	// Version is the latest release version string (e.g. "0.4.2").
	Version string
	// Checksums maps binary filename (e.g. "k2r-linux-arm64") to its
	// SHA256 hex hash.
	Checksums map[string]string
}

// FetchLatest queries each CDN endpoint in order, returning the first
// successful LATEST + checksums.txt pair. The returned UpgradeInfo is
// safe to share across goroutines (immutable after construction).
func (u *Upgrader) FetchLatest() (*UpgradeInfo, error) {
	client := &http.Client{Timeout: 10 * time.Second}

	var lastErr error
	for _, endpoint := range u.cfg.ReleaseEndpoints {
		version, err := fetchText(client, endpoint+"/LATEST")
		if err != nil {
			lastErr = err
			continue
		}
		version = strings.TrimSpace(version)
		if version == "" {
			lastErr = fmt.Errorf("empty LATEST from %s", endpoint)
			continue
		}

		checksumURL := fmt.Sprintf("%s/%s/checksums.txt", endpoint, version)
		body, err := fetchText(client, checksumURL)
		if err != nil {
			lastErr = err
			continue
		}

		checksums, err := parseChecksums(body)
		if err != nil {
			lastErr = fmt.Errorf("parse checksums from %s: %w", endpoint, err)
			continue
		}

		return &UpgradeInfo{
			Version:   version,
			Checksums: checksums,
		}, nil
	}

	return nil, fmt.Errorf("all endpoints failed: %w", lastErr)
}

// BinaryFilename returns the platform-specific filename for cfg.BinaryName,
// e.g. "k2r-linux-amd64" or "k2-linux-arm64".
func (u *Upgrader) BinaryFilename() string {
	return fmt.Sprintf("%s-linux-%s", u.cfg.BinaryName, runtimeGOARCH())
}

// BinaryURL returns the full CDN download URL for the given endpoint and
// version, combining with cfg.BinaryName and the current architecture.
func (u *Upgrader) BinaryURL(endpoint, version string) string {
	return fmt.Sprintf("%s/%s/%s", endpoint, version, u.BinaryFilename())
}

// ReplaceBinary downloads info.Version from cfg.ReleaseEndpoints, verifies
// the SHA-256 hash against info.Checksums, and atomically replaces the
// current binary. Returns nil on success or the last error encountered.
//
// This is a one-shot helper with no persistent state; for the background
// HTTP-triggered upgrade path with crash recovery, use Apply().
func (u *Upgrader) ReplaceBinary(info *UpgradeInfo) error {
	binaryPath, err := u.resolveBinaryPath()
	if err != nil {
		return err
	}
	expectedHash, ok := info.Checksums[u.BinaryFilename()]
	if !ok {
		return fmt.Errorf("no binary available for linux/%s in release", runtimeGOARCH())
	}

	var lastErr error
	for _, endpoint := range u.cfg.ReleaseEndpoints {
		url := u.BinaryURL(endpoint, info.Version)
		if lastErr = DownloadAndReplace(url, expectedHash, binaryPath); lastErr == nil {
			return nil
		}
	}
	return lastErr
}

// Restart invokes cfg.RestartFunc if set.
func (u *Upgrader) Restart() {
	if u.cfg.RestartFunc != nil {
		u.cfg.RestartFunc()
	}
}

// DownloadAndReplace downloads a binary from url, verifies its SHA256 hash
// against expectedHash (if non-empty), then atomically replaces targetPath.
// The replacement file inherits the permissions of the existing targetPath.
//
// Exported as a package-level function because the download+verify+replace
// sequence is useful to callers that already know the URL and target.
func DownloadAndReplace(url, expectedHash, targetPath string) error {
	client := &http.Client{Timeout: 5 * time.Minute}

	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download: HTTP %d", resp.StatusCode)
	}

	dir := filepath.Dir(targetPath)
	tmp, err := os.CreateTemp(dir, "webui-upgrade-*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()

	h := sha256.New()
	reader := io.TeeReader(resp.Body, h)

	if _, err := io.Copy(tmp, reader); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write: %w", err)
	}
	tmp.Close()

	if expectedHash != "" {
		actualHash := hex.EncodeToString(h.Sum(nil))
		if actualHash != expectedHash {
			os.Remove(tmpPath)
			return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedHash, actualHash)
		}
	}

	fi, err := os.Stat(targetPath)
	if err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("stat target: %w", err)
	}
	if err := os.Chmod(tmpPath, fi.Mode()); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("chmod: %w", err)
	}

	if err := os.Rename(tmpPath, targetPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("replace: %w", err)
	}

	return nil
}

// IsOpenWRT reports whether the current host is running OpenWRT. Callers
// typically use this to decide between systemctl and /etc/init.d/<unit>.
func IsOpenWRT() bool {
	_, err := os.Stat("/etc/openwrt_release")
	return err == nil
}

// fetchText performs a GET and returns the response body as a string.
func fetchText(client *http.Client, url string) (string, error) {
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// parseChecksums parses sha256sum-format output into a map of filename→hash.
// Each line is: "<hash>  <filename>" (two spaces between hash and name).
func parseChecksums(body string) (map[string]string, error) {
	result := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			return nil, fmt.Errorf("invalid checksum line: %q", line)
		}
		hash := parts[0]
		name := parts[1]
		result[name] = hash
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// resolveBinaryPath returns cfg.BinaryPath if non-empty, otherwise falls
// back to os.Executable() with symlink resolution.
func (u *Upgrader) resolveBinaryPath() (string, error) {
	if u.cfg.BinaryPath != "" {
		return u.cfg.BinaryPath, nil
	}
	self, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve executable: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(self)
	if err != nil {
		return "", fmt.Errorf("resolve symlinks: %w", err)
	}
	return resolved, nil
}

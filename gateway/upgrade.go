//go:build linux

package gateway

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// K2rReleaseEndpoints is the list of CDN endpoints for k2r releases.
// Tried in order; first successful response wins.
var K2rReleaseEndpoints = []string{
	"https://d13jc1jqzlg4yt.cloudfront.net/kaitu/k2r",
	"https://dl.kaitu.io/kaitu/k2r",
}

// UpgradeInfo holds the latest release version and per-binary SHA256 checksums.
type UpgradeInfo struct {
	// Version is the latest release version string (e.g. "0.4.2").
	Version string
	// Checksums maps binary filename (e.g. "k2r-linux-arm64") to its SHA256 hex hash.
	Checksums map[string]string
}

// FetchLatestK2r fetches the latest k2r version and checksums from the CDN.
// It tries each endpoint in K2rReleaseEndpoints and returns the first success.
func FetchLatestK2r() (*UpgradeInfo, error) {
	client := &http.Client{Timeout: 10 * time.Second}

	var lastErr error
	for _, endpoint := range K2rReleaseEndpoints {
		// Fetch LATEST file (contains just the version string).
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

		// Fetch checksums.txt for that version.
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

// DownloadAndReplace downloads a binary from url, verifies its SHA256 hash
// against expectedHash (if non-empty), then atomically replaces targetPath.
// The replacement file inherits the permissions of the existing targetPath.
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

	// Write to a temp file in the same directory for atomic rename.
	dir := filepath.Dir(targetPath)
	tmp, err := os.CreateTemp(dir, "k2r-upgrade-*")
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

	// Preserve existing file permissions.
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

// RestartService restarts the k2r system service. It detects whether the
// host is running OpenWrt (procd) or a systemd-based Linux distribution.
func RestartService() {
	if isOpenWRTSystem() {
		exec.Command("/etc/init.d/k2r", "restart").Run()
	} else {
		exec.Command("systemctl", "restart", "k2r").Run()
	}
}

// BinaryURL returns the CDN download URL for the current architecture's k2r
// binary given a version string and a base endpoint.
func BinaryURL(endpoint, version string) string {
	arch := runtime.GOARCH
	return fmt.Sprintf("%s/%s/k2r-linux-%s", endpoint, version, arch)
}

// isOpenWRTSystem reports whether the current host is running OpenWrt.
func isOpenWRTSystem() bool {
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

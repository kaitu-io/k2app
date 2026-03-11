package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var releaseEndpoints = []string{
	"https://d13jc1jqzlg4yt.cloudfront.net/kaitu/k2",
	"https://d0.all7.cc/kaitu/k2",
}

func cmdUpgrade(args []string) {
	fs := flag.NewFlagSet("upgrade", flag.ExitOnError)
	check := fs.Bool("check", false, "check for updates without installing")
	fs.Parse(args)

	latest, err := fetchLatestVersion()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to check for updates: %v\n", err)
		os.Exit(1)
	}

	if latest == version {
		fmt.Printf("Already up to date: %s\n", version)
		return
	}

	fmt.Printf("New version available: %s (current: %s)\n", latest, version)

	if *check {
		return
	}

	binary := fmt.Sprintf("k2-%s-%s", runtime.GOOS, runtime.GOARCH)
	expectedHash, err := fetchChecksum(latest, binary)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to fetch checksum: %v\n", err)
		os.Exit(1)
	}

	url := downloadURL(latest, binary)
	if err := downloadAndReplace(url, expectedHash); err != nil {
		fmt.Fprintf(os.Stderr, "Upgrade failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Upgraded to %s\n", latest)
}

// fetchLatestVersion fetches the LATEST file from CDN endpoints.
func fetchLatestVersion() (string, error) {
	client := &http.Client{Timeout: 10 * time.Second}

	var lastErr error
	for _, endpoint := range releaseEndpoints {
		resp, err := client.Get(endpoint + "/LATEST")
		if err != nil {
			lastErr = err
			continue
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("HTTP %d from %s", resp.StatusCode, endpoint)
			continue
		}
		v := strings.TrimSpace(string(body))
		if v == "" {
			lastErr = fmt.Errorf("empty LATEST from %s", endpoint)
			continue
		}
		return v, nil
	}
	return "", fmt.Errorf("all endpoints failed: %w", lastErr)
}

// fetchChecksum fetches checksums.txt and returns the hash for the given binary.
func fetchChecksum(ver, binary string) (string, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	path := fmt.Sprintf("/%s/checksums.txt", ver)

	var lastErr error
	for _, endpoint := range releaseEndpoints {
		resp, err := client.Get(endpoint + path)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			lastErr = fmt.Errorf("HTTP %d from %s", resp.StatusCode, endpoint)
			continue
		}
		hash, err := parseChecksum(resp.Body, binary)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		return hash, nil
	}
	return "", fmt.Errorf("all endpoints failed: %w", lastErr)
}

// parseChecksum reads checksums.txt content and finds the hash for binary.
func parseChecksum(r io.Reader, binary string) (string, error) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) == 2 && parts[1] == binary {
			return parts[0], nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read checksums: %w", err)
	}
	return "", fmt.Errorf("no checksum for %s", binary)
}

// downloadURL returns the CDN URL for a versioned binary.
func downloadURL(ver, binary string) string {
	return releaseEndpoints[0] + "/" + ver + "/" + binary
}

func downloadAndReplace(url string, expectedHash string) error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find executable: %w", err)
	}
	self, err = filepath.EvalSymlinks(self)
	if err != nil {
		return fmt.Errorf("resolve symlinks: %w", err)
	}

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download: HTTP %d", resp.StatusCode)
	}

	dir := filepath.Dir(self)
	tmp, err := os.CreateTemp(dir, "k2-upgrade-*")
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
		fmt.Println("Checksum verified (sha256)")
	}

	fi, err := os.Stat(self)
	if err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("stat: %w", err)
	}
	if err := os.Chmod(tmpPath, fi.Mode()); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("chmod: %w", err)
	}

	if err := os.Rename(tmpPath, self); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("replace: %w", err)
	}

	return nil
}

// checkVersionAsync prints a message if a newer version is available.
// Non-blocking, meant to be called in a goroutine at daemon startup.
func checkVersionAsync() {
	latest, err := fetchLatestVersion()
	if err != nil {
		return
	}
	if latest != "" && latest != version {
		fmt.Printf("New version available: %s (run 'k2 upgrade' to update)\n", latest)
	}
}

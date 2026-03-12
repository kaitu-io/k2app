package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

type latestInfo struct {
	Version   string            `json:"version"`
	Binaries  map[string]string `json:"binaries"`            // "darwin-arm64" -> download URL
	Checksums map[string]string `json:"checksums,omitempty"` // "darwin-arm64" -> "sha256:abc..."
}

func cmdUpgrade(args []string) {
	fs := flag.NewFlagSet("upgrade", flag.ExitOnError)
	check := fs.Bool("check", false, "check for updates without installing")
	fs.Parse(args)

	info, err := fetchLatest()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to check for updates: %v\n", err)
		os.Exit(1)
	}

	if info.Version == version {
		fmt.Printf("Already up to date: %s\n", version)
		return
	}

	fmt.Printf("New version available: %s (current: %s)\n", info.Version, version)

	if *check {
		return
	}

	key := fmt.Sprintf("%s-%s", runtime.GOOS, runtime.GOARCH)
	downloadURL, ok := info.Binaries[key]
	if !ok {
		fmt.Fprintf(os.Stderr, "No binary available for %s\n", key)
		os.Exit(1)
	}

	expectedHash := ""
	if info.Checksums != nil {
		if cs, ok := info.Checksums[key]; ok {
			expectedHash = strings.TrimPrefix(cs, "sha256:")
		}
	}

	if err := downloadAndReplace(downloadURL, expectedHash); err != nil {
		fmt.Fprintf(os.Stderr, "Upgrade failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Upgraded to %s\n", info.Version)
}

func fetchLatest() (*latestInfo, error) {
	client := &http.Client{Timeout: 10 * time.Second}

	var lastErr error
	for _, endpoint := range releaseEndpoints {
		resp, err := client.Get(endpoint + "/cloudfront.latest.json")
		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			lastErr = fmt.Errorf("HTTP %d from %s", resp.StatusCode, endpoint)
			continue
		}

		var info latestInfo
		if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
			resp.Body.Close()
			lastErr = err
			continue
		}
		resp.Body.Close()
		return &info, nil
	}
	return nil, fmt.Errorf("all endpoints failed: %w", lastErr)
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

	// Write to temp file in same directory for atomic rename.
	dir := filepath.Dir(self)
	tmp, err := os.CreateTemp(dir, "k2-upgrade-*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()

	// Hash while downloading.
	h := sha256.New()
	reader := io.TeeReader(resp.Body, h)

	if _, err := io.Copy(tmp, reader); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write: %w", err)
	}
	tmp.Close()

	// Verify checksum if available.
	if expectedHash != "" {
		actualHash := hex.EncodeToString(h.Sum(nil))
		if actualHash != expectedHash {
			os.Remove(tmpPath)
			return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedHash, actualHash)
		}
		fmt.Println("Checksum verified (sha256)")
	}

	// Copy permissions from current binary.
	fi, err := os.Stat(self)
	if err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("stat: %w", err)
	}
	if err := os.Chmod(tmpPath, fi.Mode()); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("chmod: %w", err)
	}

	// Replace binary. On Windows, a running executable cannot be deleted
	// but can be renamed. Move the current binary aside first.
	if runtime.GOOS == "windows" {
		oldPath := self + ".old"
		os.Remove(oldPath) // clean up from previous upgrade
		if err := os.Rename(self, oldPath); err != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("rename current binary: %w", err)
		}
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
	client := &http.Client{Timeout: 5 * time.Second}

	for _, endpoint := range releaseEndpoints {
		resp, err := client.Get(endpoint + "/cloudfront.latest.json")
		if err != nil {
			continue
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			continue
		}

		var info latestInfo
		if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
			resp.Body.Close()
			continue
		}
		resp.Body.Close()

		if info.Version != "" && info.Version != version {
			fmt.Printf("New version available: %s (run 'k2 upgrade' to update)\n", info.Version)
		}
		return
	}
}

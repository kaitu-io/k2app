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

	"github.com/kaitu-io/k2/config"
)

var releaseEndpoints = []string{
	"https://d13jc1jqzlg4yt.cloudfront.net/kaitu/k2",
	"https://d0.all7.cc/kaitu/k2",
}

type latestInfo struct {
	Version   string            `json:"version"`
	Binaries  map[string]string `json:"binaries"`            // "linux-amd64" -> download URL
	Checksums map[string]string `json:"checksums,omitempty"` // "linux-amd64" -> "sha256:abc..."
}

// Channel preference persistence — mirrors desktop/src-tauri/src/channel.rs pattern.
// Stored in certDir (default ~/.k2s/) alongside certs and config.

func channelFilePath() string {
	return filepath.Join(config.DefaultServerConfig().CertDir, "update-channel")
}

func readChannel() string {
	data, err := os.ReadFile(channelFilePath())
	if err != nil {
		return "stable"
	}
	ch := strings.TrimSpace(string(data))
	if ch == "beta" {
		return "beta"
	}
	return "stable"
}

func saveChannel(ch string) error {
	path := channelFilePath()
	if dir := filepath.Dir(path); dir != "" {
		os.MkdirAll(dir, 0755)
	}
	return os.WriteFile(path, []byte(ch), 0644)
}

func manifestPath(channel string) string {
	if channel == "beta" {
		return "/beta/k2s-cloudfront.latest.json"
	}
	return "/k2s-cloudfront.latest.json"
}

func cmdUpgrade(args []string) {
	fs := flag.NewFlagSet("upgrade", flag.ExitOnError)
	check := fs.Bool("check", false, "check for updates without installing")
	channel := fs.String("channel", "", "set update channel: stable|beta")
	fs.Parse(args)

	if *channel != "" {
		if *channel != "stable" && *channel != "beta" {
			fmt.Fprintf(os.Stderr, "Invalid channel: %s (use stable or beta)\n", *channel)
			os.Exit(1)
		}
		if err := saveChannel(*channel); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to save channel: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Update channel set to: %s\n", *channel)
	}

	ch := readChannel()
	info, err := fetchLatest(ch)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to check for updates: %v\n", err)
		os.Exit(1)
	}

	if info.Version == version {
		fmt.Printf("Already up to date: %s (channel: %s)\n", version, ch)
		return
	}

	fmt.Printf("New version available: %s (current: %s, channel: %s)\n", info.Version, version, ch)

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

func fetchLatest(channel string) (*latestInfo, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	path := manifestPath(channel)

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

	dir := filepath.Dir(self)
	tmp, err := os.CreateTemp(dir, "k2s-upgrade-*")
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
// Non-blocking, meant to be called in a goroutine at server startup.
func checkVersionAsync() {
	ch := readChannel()
	client := &http.Client{Timeout: 5 * time.Second}
	path := manifestPath(ch)

	for _, endpoint := range releaseEndpoints {
		resp, err := client.Get(endpoint + path)
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
			fmt.Printf("New version available: %s (run 'k2s upgrade' to update)\n", info.Version)
		}
		return
	}
}

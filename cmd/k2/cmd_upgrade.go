package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

const releasesBaseURL = "https://releases.kaitu.io/k2"

type latestInfo struct {
	Version  string `json:"version"`
	Binaries map[string]string `json:"binaries"` // "darwin-arm64" -> download URL
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

	if err := downloadAndReplace(downloadURL); err != nil {
		fmt.Fprintf(os.Stderr, "Upgrade failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Upgraded to %s\n", info.Version)
}

func fetchLatest() (*latestInfo, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(releasesBaseURL + "/latest.json")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var info latestInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}
	return &info, nil
}

func downloadAndReplace(url string) error {
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

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write: %w", err)
	}
	tmp.Close()

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

	// Atomic replace.
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
	resp, err := client.Get(releasesBaseURL + "/latest.json")
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return
	}

	var info latestInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return
	}

	if info.Version != "" && info.Version != version {
		fmt.Printf("New version available: %s (run 'k2 upgrade' to update)\n", info.Version)
	}
}

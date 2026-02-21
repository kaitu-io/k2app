package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kaitu-io/k2/config"
)

// cmdSetup handles "k2s setup": ensures k2s is installed as a system service,
// starts it if needed, waits for the connect URL, and prints it.
//
// This is the explicit replacement for the old "k2s run" smart-install default.
func cmdSetup(args []string) {
	certDir := config.DefaultServerConfig().CertDir
	urlPath := filepath.Join(certDir, "connect-url.txt")

	// Already running? Just print the URL.
	if isServiceRunning() {
		if url := readConnectURL(urlPath); url != "" {
			fmt.Println(url)
			return
		}
		fmt.Println("k2s is running but connect URL not yet available. Waiting...")
		if url := waitForConnectURL(urlPath, 30*time.Second); url != "" {
			fmt.Println(url)
			return
		}
		fmt.Fprintln(os.Stderr, "Timed out waiting for connect URL.")
		fmt.Fprintf(os.Stderr, "Check logs or run: k2s\n")
		os.Exit(1)
	}

	// Not installed → install.
	if !isServiceInstalled() {
		fmt.Println("Installing k2s service...")
		if err := installService(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to install service: %v\n", err)
			fmt.Fprintf(os.Stderr, "Try running with sudo, or use: k2s\n")
			os.Exit(1)
		}
		fmt.Println("Service installed.")
	} else {
		// Installed but not running → start.
		fmt.Println("Starting k2s service...")
		if err := startService(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to start service: %v\n", err)
			fmt.Fprintf(os.Stderr, "Try running with sudo, or use: k2s\n")
			os.Exit(1)
		}
	}

	// Wait for connect URL to appear.
	fmt.Println("Waiting for server to start...")
	url := waitForConnectURL(urlPath, 30*time.Second)
	if url == "" {
		fmt.Fprintln(os.Stderr, "Timed out waiting for server to start.")
		fmt.Fprintf(os.Stderr, "Check logs or run: k2s\n")
		os.Exit(1)
	}

	fmt.Println()
	fmt.Println("k2s is running. Share this URL with clients:")
	fmt.Println()
	fmt.Println("  " + url)
	fmt.Println()
}

// readConnectURL reads the connect URL from the file, returning empty string if not found.
func readConnectURL(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// waitForConnectURL polls for the connect-url.txt file to appear.
func waitForConnectURL(path string, timeout time.Duration) string {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if url := readConnectURL(path); url != "" {
			return url
		}
		time.Sleep(500 * time.Millisecond)
	}
	return ""
}

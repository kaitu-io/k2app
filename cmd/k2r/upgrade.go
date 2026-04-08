//go:build linux

package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"

	"github.com/kaitu-io/k2/gateway"
)

// cmdUpgrade implements the "k2r upgrade" subcommand.
func cmdUpgrade(args []string) {
	fs := flag.NewFlagSet("upgrade", flag.ExitOnError)
	check := fs.Bool("check", false, "check for updates without installing")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `Check or install k2r updates.

Usage: k2r upgrade [--check]

  --check   Print available version without installing.

`)
		fs.PrintDefaults()
	}
	fs.Parse(args)

	info, err := gateway.FetchLatestK2r()
	if err != nil {
		out.fail("UPDATE_CHECK_FAILED", err.Error(), "check your internet connection")
		os.Exit(1)
	}

	if info.Version == version {
		fmt.Printf("Already up to date: %s\n", version)
		out.ok(map[string]string{"version": version, "status": "up-to-date"})
		return
	}

	fmt.Printf("New version available: %s (current: %s)\n", info.Version, version)

	if *check {
		out.ok(map[string]string{"current": version, "latest": info.Version, "status": "update-available"})
		return
	}

	// Determine the binary filename for the current arch.
	arch := runtime.GOARCH
	binaryName := fmt.Sprintf("k2r-linux-%s", arch)

	expectedHash, ok := info.Checksums[binaryName]
	if !ok {
		out.fail("NO_BINARY", fmt.Sprintf("no binary available for linux/%s", arch), "")
		os.Exit(1)
	}

	// Resolve current executable path.
	self, err := os.Executable()
	if err != nil {
		out.fail("UPGRADE_FAILED", "cannot resolve executable path: "+err.Error(), "")
		os.Exit(1)
	}

	// Find a working endpoint and build the download URL.
	var downloadURL string
	for _, ep := range gateway.K2rReleaseEndpoints {
		downloadURL = gateway.BinaryURL(ep, info.Version)
		break
	}

	fmt.Printf("Downloading %s...\n", downloadURL)
	if err := gateway.DownloadAndReplace(downloadURL, expectedHash, self); err != nil {
		out.fail("UPGRADE_FAILED", err.Error(), "")
		os.Exit(1)
	}

	fmt.Printf("Upgraded to %s — restarting service...\n", info.Version)
	gateway.RestartService()
	out.ok(map[string]string{"version": info.Version, "status": "upgraded"})
}

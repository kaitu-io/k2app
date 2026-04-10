//go:build linux

package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"

	"github.com/kaitu-io/k2/webui"
)

// k2rReleaseEndpoints is the CDN list for k2r OTA releases. Mirrors the
// list the gateway HTTP daemon uses in newK2rUpgrader().
var k2rReleaseEndpoints = []string{
	"https://d13jc1jqzlg4yt.cloudfront.net/kaitu/k2r",
	"https://dl.kaitu.io/kaitu/k2r",
}

// newCliUpgrader builds a webui.Upgrader configured for CLI use. It points
// at the running executable via webui's os.Executable() fallback and wires
// the restart command to match newK2rUpgrader in the gateway package.
func newCliUpgrader() *webui.Upgrader {
	return webui.NewUpgrader(version, webui.UpgradeConfig{
		BinaryName:       "k2r",
		StateDir:         "/etc/k2r",
		ReleaseEndpoints: k2rReleaseEndpoints,
		RestartFunc: func() {
			if webui.IsOpenWRT() {
				exec.Command("/etc/init.d/k2r", "restart").Run()
			} else {
				exec.Command("systemctl", "restart", "k2r").Run()
			}
		},
	})
}

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

	u := newCliUpgrader()

	info, err := u.FetchLatest()
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

	// webui.Upgrader.ReplaceBinary walks ReleaseEndpoints internally and
	// verifies the SHA-256 against info.Checksums[BinaryFilename()].
	fmt.Printf("Downloading %s...\n", u.BinaryFilename())
	if err := u.ReplaceBinary(info); err != nil {
		out.fail("UPGRADE_FAILED", err.Error(), "")
		os.Exit(1)
	}

	fmt.Printf("Upgraded to %s — restarting service...\n", info.Version)
	u.Restart()
	out.ok(map[string]string{"version": info.Version, "status": "upgraded"})
}

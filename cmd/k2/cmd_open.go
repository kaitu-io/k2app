package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/kaitu-io/k2/daemon"
)

func cmdOpen(args []string) {
	url := fmt.Sprintf("http://%s", daemon.DefaultAddr)
	if err := openBrowser(url); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open browser: %v\n", err)
		fmt.Fprintf(os.Stderr, "Open manually: %s\n", url)
		os.Exit(1)
	}
	fmt.Printf("Opening %s in browser...\n", url)
}

func openBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "linux":
		return exec.Command("xdg-open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

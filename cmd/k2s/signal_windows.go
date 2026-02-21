//go:build windows

package main

import (
	"fmt"
	"io"
	"os"
	"syscall"

	"github.com/kaitu-io/k2/config"
)

// runSignal sends a signal (stop) to the server identified by pidPath on Windows.
// Note: Windows does not support SIGHUP (reload) — only stop is supported.
func runSignal(sig string, pidPath string, w io.Writer) error {
	switch sig {
	case "stop":
		// supported
	case "reload":
		return fmt.Errorf("reload signal is not supported on Windows")
	default:
		return fmt.Errorf("unknown signal %q: use 'stop'", sig)
	}

	// Read PID file.
	pid, err := config.ReadPID(pidPath)
	if err != nil {
		return fmt.Errorf("read pid file: %w", err)
	}
	if pid == 0 {
		return fmt.Errorf("k2s is not running (pid file not found: %s)", pidPath)
	}

	// Verify process is alive.
	if !config.IsProcessAlive(pid) {
		config.RemovePID(pidPath)
		return fmt.Errorf("k2s is not running (stale pid %d, file removed)", pid)
	}

	// Send SIGTERM (Windows equivalent for graceful stop).
	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find process %d: %w", pid, err)
	}
	if err := process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("send signal to pid %d: %w", pid, err)
	}

	fmt.Fprintf(w, "Sent stop signal to k2s (PID %d)\n", pid)
	return nil
}

//go:build !windows

package main

import (
	"fmt"
	"io"
	"os"
	"syscall"

	"github.com/kaitu-io/k2/config"
)

// runSignal sends a signal (stop or reload) to the server identified by pidPath.
// It writes a confirmation message to w on success.
// Returns an error if the PID file is missing, the process is not alive, or signal fails.
func runSignal(sig string, pidPath string, w io.Writer) error {
	// Validate signal name.
	var sysSignal syscall.Signal
	switch sig {
	case "stop":
		sysSignal = syscall.SIGTERM
	case "reload":
		sysSignal = syscall.SIGHUP
	default:
		return fmt.Errorf("unknown signal %q: use 'stop' or 'reload'", sig)
	}

	// Read PID file.
	pid, err := config.ReadPID(pidPath)
	if err != nil {
		return fmt.Errorf("read pid file: %w", err)
	}
	if pid == 0 {
		return fmt.Errorf("k2s is not running (pid file not found: %s)", pidPath)
	}

	// Verify process is alive (removes stale file if not).
	if !config.IsProcessAlive(pid) {
		// Remove the stale PID file.
		config.RemovePID(pidPath)
		return fmt.Errorf("k2s is not running (stale pid %d, file removed)", pid)
	}

	// Send signal to the process.
	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find process %d: %w", pid, err)
	}
	if err := process.Signal(sysSignal); err != nil {
		return fmt.Errorf("send signal to pid %d: %w", pid, err)
	}

	switch sig {
	case "stop":
		fmt.Fprintf(w, "Sent stop signal to k2s (PID %d)\n", pid)
	case "reload":
		fmt.Fprintf(w, "Sent reload signal to k2s (PID %d)\n", pid)
	}
	return nil
}

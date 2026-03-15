//go:build !windows

package config

import (
	"os"
	"syscall"
)

// IsProcessAlive reports whether the process with the given PID is running.
// On Unix, it uses signal 0 to probe the process without sending a real signal.
func IsProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// os.FindProcess on Unix always succeeds — use signal 0 to probe.
	err = process.Signal(syscall.Signal(0))
	return err == nil
}

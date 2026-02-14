//go:build windows

package daemon

import (
	"os"
	"strings"
	"syscall"
)

// processExists checks if a process with the given PID is alive.
// On Windows, os.FindProcess always succeeds, so we probe with signal 0.
// If the error is nil or "Access is denied", the process exists.
func processExists(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = p.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	return strings.Contains(err.Error(), "Access is denied")
}

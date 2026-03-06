//go:build darwin || linux

package daemon

import "syscall"

// shouldMonitorPID reports whether PID monitoring is supported on this platform.
func shouldMonitorPID() bool { return true }

// processExists checks if a process with the given PID is alive.
func processExists(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

//go:build darwin || linux

package daemon

import "syscall"

// processExists checks if a process with the given PID is alive.
func processExists(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

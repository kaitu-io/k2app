//go:build windows

package daemon

// shouldMonitorPID reports whether PID monitoring is supported on this platform.
// Windows: disabled — daemon runs as service, Signal(0) unreliable across sessions.
func shouldMonitorPID() bool { return false }

// processExists is a no-op stub on Windows (PID monitoring is disabled).
func processExists(pid int) bool { return true }

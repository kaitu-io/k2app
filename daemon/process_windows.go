//go:build windows

package daemon

import "golang.org/x/sys/windows"

// shouldMonitorPID reports whether PID monitoring is supported on this platform.
// Windows: disabled — daemon runs as service, Signal(0) unreliable across sessions.
func shouldMonitorPID() bool { return false }

// processExists checks if a process with the given PID is alive.
// Signal(0) is not supported on Windows ("not supported by windows"),
// so we use OpenProcess with PROCESS_QUERY_LIMITED_INFORMATION instead.
func processExists(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	windows.CloseHandle(h)
	return true
}

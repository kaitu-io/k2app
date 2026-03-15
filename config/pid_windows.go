//go:build windows

package config

import "golang.org/x/sys/windows"

// IsProcessAlive reports whether the process with the given PID is running.
// On Windows, Signal(0) returns "not supported by windows", so we use
// OpenProcess with PROCESS_QUERY_LIMITED_INFORMATION instead.
// This mirrors the pattern in daemon/process_windows.go.
func IsProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	windows.CloseHandle(h)
	return true
}

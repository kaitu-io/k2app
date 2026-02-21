package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)

// WritePID writes the current process PID to the specified file.
// The file is created or overwritten with mode 0644.
func WritePID(path string) error {
	pid := os.Getpid()
	content := fmt.Sprintf("%d\n", pid)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("pid: write %s: %w", path, err)
	}
	return nil
}

// ReadPID reads the PID from a file.
// Returns 0, nil if the file does not exist.
// Returns an error if the file exists but cannot be read or parsed.
func ReadPID(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, fmt.Errorf("pid: read %s: %w", path, err)
	}

	s := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("pid: parse %s: invalid PID %q: %w", path, s, err)
	}
	return pid, nil
}

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

// RemovePID removes the PID file. Ignores any errors (best-effort cleanup).
func RemovePID(path string) {
	os.Remove(path) //nolint:errcheck
}

// CheckStale checks whether the PID file refers to a process that is no longer
// running (stale). If stale, it removes the file and returns the stale PID.
// Returns 0, nil if the file does not exist or the process is still alive.
func CheckStale(path string) (int, error) {
	pid, err := ReadPID(path)
	if err != nil {
		return 0, err
	}
	if pid == 0 {
		// File does not exist.
		return 0, nil
	}
	if IsProcessAlive(pid) {
		// Process is still running — not stale.
		return 0, nil
	}
	// Process is dead — remove the stale file.
	RemovePID(path)
	return pid, nil
}

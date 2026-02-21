package config

// WritePID writes the current process PID to the specified file.
func WritePID(path string) error {
	return nil // RED: not implemented
}

// ReadPID reads the PID from a file. Returns 0, nil if the file does not exist.
func ReadPID(path string) (int, error) {
	return 0, nil // RED: not implemented
}

// IsProcessAlive reports whether the process with the given PID is running.
func IsProcessAlive(pid int) bool {
	return false // RED: not implemented
}

// RemovePID removes the PID file. Ignores errors (best-effort cleanup).
func RemovePID(path string) {
	// RED: not implemented
}

// CheckStale checks whether the PID file is stale (process no longer running).
// If stale, removes the file and returns the stale PID.
// Returns 0 if the file doesn't exist or the process is still alive.
func CheckStale(path string) (int, error) {
	return 0, nil // RED: not implemented
}

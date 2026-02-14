//go:build windows

package daemon

// setMaxProcs is a no-op on Windows (no RLIMIT_NPROC equivalent).
func setMaxProcs(max uint64) error {
	return nil
}

// setMaxFiles is a no-op on Windows (no RLIMIT_NOFILE equivalent).
func setMaxFiles(max uint64) error {
	return nil
}

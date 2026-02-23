//go:build !windows

package wintun

// EnsureExtracted is a no-op on non-Windows platforms.
func EnsureExtracted() (string, error) {
	return "", nil
}

//go:build !windows

package config

import "os"

// isRoot reports whether the current process is running as root (uid 0).
func isRoot() bool {
	return os.Geteuid() == 0
}

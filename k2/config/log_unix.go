//go:build !windows

package config

import "os"

// IsRoot reports whether the current process is running as root (uid 0).
func IsRoot() bool {
	return os.Geteuid() == 0
}

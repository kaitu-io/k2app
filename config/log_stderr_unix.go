//go:build (darwin && !ios) || (linux && !android)

package config

import (
	"os"
	"syscall"
)

// redirectStderrFD redirects file descriptor 2 to the given file.
// Captures runtime panics and CGo crashes that bypass Go's os.Stderr.
func redirectStderrFD(f *os.File) error {
	return syscall.Dup2(int(f.Fd()), 2)
}

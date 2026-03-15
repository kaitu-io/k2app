//go:build (darwin && !ios) || (linux && !android)

package config

import (
	"os"
	"syscall"
)

// redirectStderrFD redirects file descriptor 2 to the given file.
// Returns a restore function that puts fd 2 back to its original target.
// Captures runtime panics and CGo crashes that bypass Go's os.Stderr.
func redirectStderrFD(f *os.File) (restore func(), err error) {
	origFd, err := syscall.Dup(2)
	if err != nil {
		return nil, err
	}
	if err := syscall.Dup2(int(f.Fd()), 2); err != nil {
		syscall.Close(origFd)
		return nil, err
	}
	return func() {
		syscall.Dup2(origFd, 2) //nolint:errcheck
		syscall.Close(origFd)
	}, nil
}

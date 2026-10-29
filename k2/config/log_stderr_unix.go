//go:build (darwin && !ios) || (linux && !android)

package config

import (
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// redirectStderrFD redirects file descriptor 2 to the given file.
// Returns a restore function that puts fd 2 back to its original target.
// Captures runtime panics and CGo crashes that bypass Go's os.Stderr.
//
// Uses unix.Dup2 instead of syscall.Dup2 because linux/arm64 does not have
// the dup2 syscall (only dup3). x/sys/unix.Dup2 handles this transparently.
func redirectStderrFD(f *os.File) (restore func(), err error) {
	origFd, err := syscall.Dup(2)
	if err != nil {
		return nil, err
	}
	if err := unix.Dup2(int(f.Fd()), 2); err != nil {
		syscall.Close(origFd)
		return nil, err
	}
	return func() {
		unix.Dup2(origFd, 2) //nolint:errcheck
		syscall.Close(origFd)
	}, nil
}

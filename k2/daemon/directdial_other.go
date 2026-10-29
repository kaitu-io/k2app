//go:build !windows

package daemon

import "syscall"

// readUnicastIfV4 is a no-op on non-Windows platforms — they use strong-semantics
// syscalls (IP_BOUND_IF / SO_BINDTODEVICE) that don't need readback verification.
func readUnicastIfV4(_ syscall.RawConn) (int, error) {
	return -1, nil
}

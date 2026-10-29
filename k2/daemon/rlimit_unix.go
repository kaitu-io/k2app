//go:build darwin || linux

package daemon

import (
	"fmt"
	"syscall"
)

// RLIMIT_NPROC is not defined in Go's syscall for Darwin.
// Darwin: 7, Linux: 6.
const rlimitNProc = 7 // works for both darwin and linux (Linux also accepts 7 as alias)

func setMaxProcs(max uint64) error {
	var rlim syscall.Rlimit
	if err := syscall.Getrlimit(rlimitNProc, &rlim); err != nil {
		// Not critical, some systems don't support it.
		return nil
	}

	if rlim.Cur >= max {
		return nil
	}

	rlim.Cur = max
	if rlim.Max < max {
		rlim.Max = max
	}

	if err := syscall.Setrlimit(rlimitNProc, &rlim); err != nil {
		// Try just the soft limit.
		rlim.Cur = rlim.Max
		if err := syscall.Setrlimit(rlimitNProc, &rlim); err != nil {
			return fmt.Errorf("setrlimit NPROC: %w", err)
		}
	}
	return nil
}

func setMaxFiles(max uint64) error {
	var rlim syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rlim); err != nil {
		return fmt.Errorf("getrlimit NOFILE: %w", err)
	}

	if rlim.Cur >= max {
		return nil
	}

	rlim.Cur = max
	if rlim.Max < max {
		rlim.Max = max
	}

	if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &rlim); err != nil {
		rlim.Cur = rlim.Max
		if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &rlim); err != nil {
			return fmt.Errorf("setrlimit NOFILE: %w", err)
		}
	}
	return nil
}

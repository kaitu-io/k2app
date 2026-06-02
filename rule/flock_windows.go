//go:build windows

package rule

import (
	"context"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

// acquireLock opens lockPath and takes an exclusive byte-range lock via
// LockFileEx, returning a release func. Blocks (polling) until granted or ctx
// is done. Windows releases the lock automatically when the handle closes —
// including on process crash — so this is crash-safe like the Unix flock path.
//
// This serializes EnsureBundles across processes, matching the Unix
// syscall.Flock path. Harmless today (single k2.exe desktop process) but
// removes a latent cross-process hazard.
func acquireLock(ctx context.Context, lockPath string) (func(), error) {
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	h := windows.Handle(f.Fd())
	ol := new(windows.Overlapped)
	for {
		err := windows.LockFileEx(h,
			windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
			0, 1, 0, ol)
		if err == nil {
			return func() {
				_ = windows.UnlockFileEx(h, 0, 1, 0, ol)
				_ = f.Close()
			}, nil
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

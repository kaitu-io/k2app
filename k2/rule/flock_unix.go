//go:build unix

package rule

import (
	"context"
	"errors"
	"os"
	"syscall"
	"time"
)

// acquireLock opens lockPath and acquires an exclusive flock, returning a
// release func. Blocks until the lock is granted or ctx is done.
//
// This serializes EnsureBundles across processes — iOS runs K2Plugin (UI
// process) and PacketTunnelProvider (Network Extension process) both calling
// EnsureBundles on the same cacheDir. singleflight only dedupes within a
// process; flock covers the cross-process case.
func acquireLock(ctx context.Context, lockPath string) (func(), error) {
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() {
				_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
				_ = f.Close()
			}, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			_ = f.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

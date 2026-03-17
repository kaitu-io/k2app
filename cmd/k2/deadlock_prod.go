//go:build !deadlock_disable

package main

import "github.com/sasha-s/go-deadlock"

func init() {
	// Desktop production: lock-ordering detection only.
	// DeadlockTimeout <= 0 disables per-Lock goroutine spawn (major overhead source).
	// Lock ordering graph still detects inconsistent acquisition order across goroutines.
	// MaxMapSize 4096 is sufficient for k2's ~30 mutexes (max pairs ~900).
	deadlock.Opts.DeadlockTimeout = -1
	deadlock.Opts.DisableLockOrderDetection = false
	deadlock.Opts.MaxMapSize = 4096
}

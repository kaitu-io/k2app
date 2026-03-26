//go:build !deadlock_disable

package main

import (
	"log/slog"
	"os"
	"time"

	"github.com/kaitu-io/k2/provider"
	"github.com/sasha-s/go-deadlock"
)

func init() {
	// Desktop production: full deadlock detection enabled.
	// DeadlockTimeout spawns a goroutine per Lock() call — acceptable overhead
	// on desktop (not mobile) for comprehensive deadlock safety.
	// On detection: clean up DNS, log, then exit. Service manager (launchd/systemd/
	// Windows Service) restarts the daemon automatically.
	deadlock.Opts.DeadlockTimeout = 60 * time.Second
	deadlock.Opts.DisableLockOrderDetection = false
	deadlock.Opts.MaxMapSize = 4096
	deadlock.Opts.OnPotentialDeadlock = func() {
		slog.Error("FATAL: potential deadlock detected, cleaning up before exit")
		// Best-effort DNS cleanup before exit. CleanupStaleDNSOverride is a
		// package-level function that doesn't acquire any deadlock.Mutex —
		// only file I/O + CGo (SCDynamicStore on macOS). Run in a goroutine
		// with timeout in case CGo itself is blocked (e.g. configd unresponsive).
		done := make(chan struct{})
		go func() {
			provider.CleanupStaleDNSOverride()
			close(done)
		}()
		select {
		case <-done:
			slog.Info("DNS cleanup completed before deadlock exit")
		case <-time.After(3 * time.Second):
			slog.Warn("DNS cleanup timed out, exiting anyway")
		}
		os.Exit(2)
	}
}

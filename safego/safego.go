// Package safego provides panic-safe goroutine launchers.
package safego

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"time"
)

// Go launches fn in a new goroutine with panic recovery.
// If fn panics, the panic is logged and the goroutine exits.
func Go(fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("goroutine panicked", "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
			}
		}()
		fn()
	}()
}

// Loop launches fn in a goroutine that restarts on panic.
// If fn returns normally (no panic), Loop exits without restarting.
// If fn panics, it is restarted after a 1-second backoff.
// Loop exits when ctx is cancelled.
func Loop(ctx context.Context, name string, fn func(ctx context.Context)) {
	go func() {
		for {
			if ctx.Err() != nil {
				return
			}
			if !runWithRecover(name, func() { fn(ctx) }) {
				// fn returned normally — do not restart.
				return
			}
			// fn panicked — backoff then restart.
			select {
			case <-ctx.Done():
				return
			case <-time.After(1 * time.Second):
			}
		}
	}()
}

// runWithRecover runs fn and returns true if fn panicked.
func runWithRecover(name string, fn func()) (panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("loop goroutine panicked, restarting", "name", name, "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
			panicked = true
		}
	}()
	fn()
	return false
}

package daemon

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"
)

// HandlePanic is a deferred function that catches panics, logs the stack trace,
// and attempts to restore network settings.
func HandlePanic() {
	if r := recover(); r != nil {
		stack := debug.Stack()
		log.Printf("PANIC: %v\n%s", r, stack)

		// Write panic log.
		dir := filepath.Join(os.TempDir(), "k2")
		os.MkdirAll(dir, 0700)
		name := fmt.Sprintf("panic-%s.log", time.Now().Format("20060102-150405"))
		path := filepath.Join(dir, name)

		content := fmt.Sprintf("Panic: %v\n\n%s", r, stack)
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			log.Printf("Failed to write panic log: %v", err)
		} else {
			log.Printf("Panic log written to %s", path)
		}

		// Attempt network recovery.
		restoreNetworkOnCrash()
	}
}

// restoreNetworkOnCrash attempts to restore network settings after a panic.
// On macOS/Linux, sing-tun's Close() should restore routes automatically,
// but if we crash without calling Close(), we need a fallback.
func restoreNetworkOnCrash() {
	// sing-tun auto-restores on Close(), but on panic Close() may not be called.
	// Platform-specific recovery is in recovery_unix.go.
	restoreNetworkPlatform()
}

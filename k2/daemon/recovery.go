package daemon

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"

	"github.com/kaitu-io/k2/config"
)

// HandlePanic is a deferred function that catches panics, logs the stack trace,
// and attempts to restore network settings.
func HandlePanic() {
	if r := recover(); r != nil {
		stack := debug.Stack()
		slog.Error("PANIC", "panic", r, "stack", string(stack))

		// Write panic log alongside service logs so Tauri can find it.
		dir := filepath.Dir(config.DefaultLogPath("k2"))
		os.MkdirAll(dir, 0755)
		name := fmt.Sprintf("panic-%s.log", time.Now().Format("20060102-150405"))
		path := filepath.Join(dir, name)

		content := fmt.Sprintf("Panic: %v\n\n%s", r, stack)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			slog.Error("failed to write panic log", "err", err)
		} else {
			slog.Info("panic log written", "path", path)
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

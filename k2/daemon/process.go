package daemon

import (
	"context"
	"log/slog"
	"time"
)

// monitorProcess watches a PID and disconnects when the process exits.
// The goroutine stops when ctx is cancelled (e.g., on doDown or new doUp).
func (d *Daemon) monitorProcess(ctx context.Context, pid int) {
	slog.Info("monitoring PID", "pid", pid)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Info("PID monitor stopped", "pid", pid)
			return
		case <-ticker.C:
			if !processExists(pid) {
				slog.Info("PID exited, disconnecting VPN", "pid", pid)
				d.doDown()
				return
			}
		}
	}
}

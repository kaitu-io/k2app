package daemon

import (
	"context"
	"log"
	"time"
)

// monitorProcess watches a PID and disconnects when the process exits.
// The goroutine stops when ctx is cancelled (e.g., on doDown or new doUp).
func (d *Daemon) monitorProcess(ctx context.Context, pid int) {
	log.Printf("Monitoring PID %d", pid)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Printf("PID %d monitor stopped", pid)
			return
		case <-ticker.C:
			if !processExists(pid) {
				log.Printf("PID %d exited, disconnecting VPN", pid)
				d.doDown()
				return
			}
		}
	}
}

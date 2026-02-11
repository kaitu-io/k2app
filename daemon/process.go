package daemon

import (
	"log"
	"time"
)

// monitorProcess watches a PID and disconnects when the process exits.
func (d *Daemon) monitorProcess(pid int) {
	log.Printf("Monitoring PID %d", pid)
	for {
		time.Sleep(5 * time.Second)
		if !processExists(pid) {
			log.Printf("PID %d exited, disconnecting VPN", pid)
			d.doDown()
			return
		}
	}
}

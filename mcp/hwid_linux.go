//go:build linux

package main

import (
	"fmt"
	"os"
	"strings"
)

// getHardwareID reads systemd-machine-id, matching Rust machine-uid 0.5.4:
// /var/lib/dbus/machine-id first, falling back to /etc/machine-id.
func getHardwareID() (string, error) {
	for _, path := range []string{"/var/lib/dbus/machine-id", "/etc/machine-id"} {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		id := strings.TrimSpace(string(data))
		if id != "" {
			return id, nil
		}
	}
	return "", fmt.Errorf("machine-id not found at /var/lib/dbus/machine-id or /etc/machine-id")
}

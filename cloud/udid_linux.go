package cloud

import (
	"os"
	"strings"
)

// platformID returns the machine ID from /etc/machine-id or
// /var/lib/dbus/machine-id on Linux.
func platformID() string {
	for _, path := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		id := strings.TrimSpace(string(data))
		if id != "" {
			return id
		}
	}
	return ""
}

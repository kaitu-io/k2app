package cloud

import (
	"os/exec"
	"strings"
)

// platformID returns the IOPlatformUUID from macOS's I/O Registry.
func platformID() string {
	out, err := exec.Command("ioreg", "-rd1", "-c", "IOPlatformExpertDevice").Output()
	if err != nil {
		return ""
	}
	// Parse IOPlatformUUID from output like:
	//   "IOPlatformUUID" = "XXXXXXXX-XXXX-XXXX-XXXX-XXXXXXXXXXXX"
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "IOPlatformUUID") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) != 2 {
				continue
			}
			uuid := strings.TrimSpace(parts[1])
			uuid = strings.Trim(uuid, "\"")
			if uuid != "" {
				return uuid
			}
		}
	}
	return ""
}

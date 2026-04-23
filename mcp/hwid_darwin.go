//go:build darwin

package main

import (
	"fmt"
	"os/exec"
	"strings"
)

// getHardwareID returns IOPlatformUUID from ioreg, matching Rust
// machine-uid 0.5.4's parsing: take the last '=' segment of the first
// line containing "IOPlatformUUID" and strip surrounding quotes/whitespace.
func getHardwareID() (string, error) {
	out, err := exec.Command("ioreg", "-rd1", "-c", "IOPlatformExpertDevice").Output()
	if err != nil {
		return "", fmt.Errorf("ioreg: %w", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, "IOPlatformUUID") {
			continue
		}
		idx := strings.LastIndex(line, "=")
		if idx < 0 {
			continue
		}
		id := strings.TrimFunc(line[idx+1:], func(r rune) bool {
			return r == '"' || r == ' ' || r == '\t'
		})
		if id != "" {
			return id, nil
		}
	}
	return "", fmt.Errorf("IOPlatformUUID not found in ioreg output")
}

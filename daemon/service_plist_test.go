//go:build darwin

package daemon

import (
	"strings"
	"testing"
)

// TestPlistTemplate verifies that the generated plist XML has the correct
// logging configuration: StandardErrorPath as a safety net for pre-logging
// panics and cgo crashes, but no StandardOutPath (Go handles log files directly).
func TestPlistTemplate(t *testing.T) {
	content := plistContent("kaitu", "/usr/local/bin/k2")

	if strings.Contains(content, "StandardOutPath") {
		t.Error("plist template must not contain StandardOutPath (Go handles log files directly)")
	}
	if !strings.Contains(content, "StandardErrorPath") {
		t.Error("plist template must contain StandardErrorPath as a safety net for pre-logging panics")
	}
	if !strings.Contains(content, "/var/log/kaitu/k2-stderr.log") {
		t.Error("StandardErrorPath must point to /var/log/kaitu/k2-stderr.log")
	}
}

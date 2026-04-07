//go:build darwin

package main

import (
	"strings"
	"testing"
)

// TestPlistTemplate verifies that the generated plist XML has the correct
// logging configuration: no StandardOutPath or StandardErrorPath (Go redirects
// stderr into k2.log via config.SetupLogging).
func TestPlistTemplate(t *testing.T) {
	content := plistContent("kaitu", "/usr/local/bin/k2")

	if strings.Contains(content, "StandardOutPath") {
		t.Error("plist template must not contain StandardOutPath (Go handles log files directly)")
	}
	if strings.Contains(content, "StandardErrorPath") {
		t.Error("plist template must not contain StandardErrorPath (stderr redirected into k2.log by Go)")
	}
}

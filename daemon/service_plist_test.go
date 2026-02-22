//go:build darwin

package daemon

import (
	"strings"
	"testing"
)

// TestPlistTemplate_NoLogPath verifies that the generated plist XML does not
// contain StandardOutPath or StandardErrorPath directives. Go-level logging
// (via lumberjack) handles file output directly; launchd log redirection is
// therefore redundant and should not be present.
func TestPlistTemplate_NoLogPath(t *testing.T) {
	content := plistContent("kaitu", "/usr/local/bin/k2")

	if strings.Contains(content, "StandardOutPath") {
		t.Error("plist template must not contain StandardOutPath (Go handles log files directly)")
	}
	if strings.Contains(content, "StandardErrorPath") {
		t.Error("plist template must not contain StandardErrorPath (Go handles log files directly)")
	}
}

//go:build (darwin && !ios) || (linux && !android) || windows

package provider

import (
	"runtime"
	"strings"
	"testing"
)

func TestDefaultTunName(t *testing.T) {
	name := defaultTunName()

	if name == "" {
		t.Fatal("defaultTunName() returned empty string, want non-empty")
	}

	switch runtime.GOOS {
	case "darwin":
		if !strings.HasPrefix(name, "utun") {
			t.Errorf("defaultTunName() on darwin = %q, want prefix utun", name)
		}
	case "linux":
		if name != "k2tun" {
			t.Errorf("defaultTunName() on linux = %q, want k2tun", name)
		}
	case "windows":
		if name != "k2tun" {
			t.Errorf("defaultTunName() on windows = %q, want k2tun", name)
		}
	default:
		t.Errorf("unexpected GOOS %q, defaultTunName() = %q", runtime.GOOS, name)
	}
}

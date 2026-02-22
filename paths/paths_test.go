package paths

import (
	"os"
	"runtime"
	"strings"
	"testing"
)

func TestCacheDir(t *testing.T) {
	dir := CacheDir()
	if dir == "" {
		t.Fatal("CacheDir returned empty string")
	}
	if !strings.HasSuffix(dir, "k2") {
		t.Fatalf("CacheDir = %q, want suffix 'k2'", dir)
	}
}

func TestCacheDir_ContainsK2(t *testing.T) {
	dir := CacheDir()
	switch runtime.GOOS {
	case "darwin":
		if !strings.Contains(dir, "Library/Caches/k2") {
			t.Fatalf("CacheDir = %q, want to contain Library/Caches/k2 on macOS", dir)
		}
	case "linux":
		if !strings.Contains(dir, ".cache/k2") && !strings.Contains(dir, "cache/k2") {
			t.Fatalf("CacheDir = %q, want to contain .cache/k2 or cache/k2 on Linux", dir)
		}
	}
}

func TestLogDir(t *testing.T) {
	dir := LogDir()
	if dir == "" {
		t.Fatal("LogDir returned empty string")
	}
	if !strings.HasSuffix(dir, "k2") {
		t.Fatalf("LogDir = %q, want suffix 'k2'", dir)
	}
}

func TestLogDir_Root(t *testing.T) {
	if os.Getuid() == 0 {
		dir := LogDir()
		if dir != "/var/log/k2" {
			t.Fatalf("LogDir as root = %q, want /var/log/k2", dir)
		}
	} else {
		dir := LogDir()
		switch runtime.GOOS {
		case "darwin":
			if !strings.Contains(dir, "Library/Logs/k2") {
				t.Fatalf("LogDir = %q, want to contain Library/Logs/k2 on macOS", dir)
			}
		case "linux":
			if !strings.Contains(dir, ".local/state/k2/log") {
				t.Fatalf("LogDir = %q, want to contain .local/state/k2/log on Linux", dir)
			}
		}
	}
}

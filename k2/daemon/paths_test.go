package daemon

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kaitu-io/k2/config"
)

func TestResolveCacheDir(t *testing.T) {
	tests := []struct {
		name          string
		goos          string
		isRoot        bool
		userCacheBase string
		programData   string
		tempDir       string
		want          string
	}{
		{
			name:          "macOS root (launchd, $HOME unset → empty userCacheBase)",
			goos:          "darwin",
			isRoot:        true,
			userCacheBase: "",
			tempDir:       "/tmp",
			want:          "/var/root/Library/Application Support/io.kaitu.desktop",
		},
		{
			name:          "macOS root (HOME stale-set to user home — must still pin to root)",
			goos:          "darwin",
			isRoot:        true,
			userCacheBase: "/Users/dave/Library/Caches",
			tempDir:       "/tmp",
			want:          "/var/root/Library/Application Support/io.kaitu.desktop",
		},
		{
			name:          "macOS user",
			goos:          "darwin",
			isRoot:        false,
			userCacheBase: "/Users/dave/Library/Caches",
			tempDir:       "/tmp",
			// filepath.Join wraps the literal so the comparison stays correct on
			// Windows runners — the implementation uses host-aware filepath.Join,
			// not goos-aware path construction. Matches the Windows subtests' pattern.
			want: filepath.Join("/Users/dave/Library/Caches", "k2"),
		},
		{
			name:          "Linux root (systemd injects HOME=/root)",
			goos:          "linux",
			isRoot:        true,
			userCacheBase: "/root/.cache",
			tempDir:       "/tmp",
			want:          filepath.Join("/root/.cache", "k2"),
		},
		{
			name:          "Linux user",
			goos:          "linux",
			isRoot:        false,
			userCacheBase: "/home/dave/.cache",
			tempDir:       "/tmp",
			want:          filepath.Join("/home/dave/.cache", "k2"),
		},
		{
			name:          "Linux root no HOME (degraded fallback to tempDir)",
			goos:          "linux",
			isRoot:        true,
			userCacheBase: "",
			tempDir:       "/tmp",
			want:          filepath.Join("/tmp", "k2"),
		},
		{
			name:        "Windows SYSTEM with ProgramData",
			goos:        "windows",
			isRoot:      true,
			programData: `C:\ProgramData`,
			tempDir:     `C:\Windows\Temp`,
			want:        filepath.Join(`C:\ProgramData`, "Kaitu", "cache"),
		},
		{
			name:        "Windows SYSTEM ProgramData env unset",
			goos:        "windows",
			isRoot:      true,
			programData: "",
			tempDir:     `C:\Windows\Temp`,
			want:        filepath.Join(`C:\ProgramData`, "Kaitu", "cache"),
		},
		{
			name:          "Windows user",
			goos:          "windows",
			isRoot:        false,
			userCacheBase: `C:\Users\dave\AppData\Local`,
			tempDir:       `C:\Windows\Temp`,
			want:          filepath.Join(`C:\Users\dave\AppData\Local`, "k2"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveCacheDir(tt.goos, tt.isRoot, tt.userCacheBase, tt.programData, tt.tempDir)
			if got != tt.want {
				t.Errorf("resolveCacheDir(%q, isRoot=%v, base=%q, pd=%q, tmp=%q) = %q, want %q",
					tt.goos, tt.isRoot, tt.userCacheBase, tt.programData, tt.tempDir, got, tt.want)
			}
		})
	}
}

// TestCacheDir is the integration smoke test — exercises the live wrapper
// against the current process's runtime.GOOS/euid/env. Only asserts that the
// result is non-empty and platform-shaped; full table coverage lives in
// TestResolveCacheDir above.
func TestCacheDir(t *testing.T) {
	dir := cacheDir()
	if dir == "" {
		t.Fatal("cacheDir returned empty string")
	}
	switch runtime.GOOS {
	case "darwin":
		if config.IsRoot() {
			if dir != "/var/root/Library/Application Support/io.kaitu.desktop" {
				t.Fatalf("cacheDir as root = %q, want /var/root/Library/Application Support/io.kaitu.desktop", dir)
			}
		} else {
			if !strings.Contains(dir, "Library/Caches/k2") {
				t.Fatalf("cacheDir = %q, want to contain Library/Caches/k2 on macOS user", dir)
			}
		}
	case "linux":
		if !strings.Contains(dir, "cache/k2") {
			t.Fatalf("cacheDir = %q, want to contain cache/k2 on Linux", dir)
		}
	case "windows":
		if config.IsRoot() {
			if !strings.Contains(dir, "Kaitu") {
				t.Fatalf("cacheDir = %q, want to contain Kaitu on Windows SYSTEM", dir)
			}
		} else {
			if !strings.HasSuffix(dir, "k2") {
				t.Fatalf("cacheDir = %q, want suffix k2 on Windows user", dir)
			}
		}
	}
}

func TestLogDir(t *testing.T) {
	dir := logDir()
	if dir == "" {
		t.Fatal("logDir returned empty string")
	}
	if !strings.Contains(dir, "k2") {
		t.Fatalf("logDir = %q, want to contain 'k2'", dir)
	}
}

func TestLogDir_Root(t *testing.T) {
	if os.Getuid() == 0 {
		dir := logDir()
		if dir != "/var/log/k2" {
			t.Fatalf("logDir as root = %q, want /var/log/k2", dir)
		}
	} else {
		dir := logDir()
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

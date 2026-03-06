package daemon

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/kaitu-io/k2/config"
)

// CacheDir returns the k2 cache directory for the current user.
// Based on os.UserCacheDir():
//
//	macOS root:   /var/root/Library/Caches/k2
//	macOS user:   ~/Library/Caches/k2
//	Linux root:   /root/.cache/k2
//	Linux user:   ~/.cache/k2
//	Windows:      %LocalAppData%\k2\cache
func cacheDir() string {
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, "k2")
}

// LogDir returns the k2 log directory for the current user.
// Root uses system standard log directories; regular users use user-level dirs:
//
//	macOS root:   /var/log/k2
//	macOS user:   ~/Library/Logs/k2
//	Linux root:   /var/log/k2
//	Linux user:   ~/.local/state/k2/log
//	Windows:      %ProgramData%\k2\log (SYSTEM) or %LocalAppData%\k2\log (user)
func logDir() string {
	switch runtime.GOOS {
	case "darwin":
		if config.IsRoot() {
			return "/var/log/k2"
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return filepath.Join(os.TempDir(), "k2")
		}
		return filepath.Join(home, "Library", "Logs", "k2")

	case "linux":
		if config.IsRoot() {
			return "/var/log/k2"
		}
		// XDG_STATE_HOME or ~/.local/state
		stateDir := os.Getenv("XDG_STATE_HOME")
		if stateDir == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return filepath.Join(os.TempDir(), "k2")
			}
			stateDir = filepath.Join(home, ".local", "state")
		}
		return filepath.Join(stateDir, "k2", "log")

	case "windows":
		// SYSTEM/admin: %ProgramData%\k2\log
		// User: %LocalAppData%\k2\log
		if config.IsRoot() {
			programData := os.Getenv("ProgramData")
			if programData == "" {
				programData = `C:\ProgramData`
			}
			return filepath.Join(programData, "k2", "log")
		}
		base, err := os.UserCacheDir()
		if err != nil {
			return filepath.Join(os.TempDir(), "k2")
		}
		return filepath.Join(base, "k2", "log")

	default:
		return filepath.Join(os.TempDir(), "k2")
	}
}

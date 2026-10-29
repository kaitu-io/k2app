package daemon

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/kaitu-io/k2/config"
)

// cacheDir returns the k2 cache directory for the current user.
//
//	macOS root:     /var/root/Library/Application Support/io.kaitu.desktop
//	                (NOT ~/Library/Caches — launchd does not propagate $HOME
//	                to system daemons, so os.UserCacheDir() would error; and
//	                Application Support survives OS cache purges, which
//	                matters because rule bundles are expensive to refetch
//	                under GFW conditions.)
//	macOS user:     ~/Library/Caches/k2
//	Linux root:     /root/.cache/k2     (systemd injects HOME=/root)
//	Linux user:     ~/.cache/k2
//	Windows SYSTEM: %ProgramData%\Kaitu\cache  (avoids system32 DLL search issues)
//	Windows user:   %LocalAppData%\k2
func cacheDir() string {
	base, _ := os.UserCacheDir()
	return resolveCacheDir(runtime.GOOS, config.IsRoot(), base, os.Getenv("ProgramData"), os.TempDir())
}

// resolveCacheDir is the pure-function form of cacheDir, parameterized for
// testability. Does not touch the filesystem or environment.
func resolveCacheDir(goos string, isRoot bool, userCacheBase, programData, tempDir string) string {
	if isRoot {
		switch goos {
		case "darwin":
			// launchd starts system daemons with no $HOME, which makes
			// os.UserCacheDir() return ("", error). The previous fallback
			// landed in /tmp, which macOS periodic cleanup can purge.
			// Pin to the canonical root Application Support path; using
			// the Tauri bundle identifier keeps namespace consistency
			// with the desktop shell at ~/Library/Application Support/io.kaitu.desktop.
			return "/var/root/Library/Application Support/io.kaitu.desktop"
		case "windows":
			if programData == "" {
				programData = `C:\ProgramData`
			}
			return filepath.Join(programData, "Kaitu", "cache")
		}
	}
	if userCacheBase == "" {
		userCacheBase = tempDir
	}
	return filepath.Join(userCacheBase, "k2")
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

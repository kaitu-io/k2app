//go:build darwin

package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const (
	serviceLabel = "kaitu"
	plistDir     = "/Library/LaunchDaemons"
)

func plistPath() string {
	return filepath.Join(plistDir, serviceLabel+".plist")
}

// plistContent generates the launchd plist XML for the daemon service.
// Primary logging is handled by Go (slog + lumberjack → /var/log/kaitu/k2.log).
// Stderr is redirected into k2.log at startup (config.SetupLogging).
func plistContent(label, exe string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>%s</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
        <string>run</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>HardResourceLimits</key>
    <dict>
        <key>NumberOfProcesses</key>
        <integer>4000</integer>
        <key>NumberOfFiles</key>
        <integer>10240</integer>
    </dict>
    <key>SoftResourceLimits</key>
    <dict>
        <key>NumberOfProcesses</key>
        <integer>4000</integer>
        <key>NumberOfFiles</key>
        <integer>10240</integer>
    </dict>
    <key>ThrottleInterval</key>
    <integer>2</integer>
</dict>
</plist>
`, label, exe)
}

const (
	watchdogLabel = "kaitu-watchdog"
)

func watchdogPlistPath() string {
	return filepath.Join(plistDir, watchdogLabel+".plist")
}

// watchdogPlistContent generates the launchd plist for the external health check.
// Runs `k2 healthcheck` every 60s. Uses the same binary as the daemon.
// Stderr goes to a separate log file for debugging.
func watchdogPlistContent(label, exe string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>%s</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
        <string>healthcheck</string>
    </array>
    <key>StartInterval</key>
    <integer>60</integer>
    <key>StandardErrorPath</key>
    <string>/var/log/kaitu/healthcheck.log</string>
</dict>
</plist>
`, label, exe)
}

func installService() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find executable: %w", err)
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}

	// Unload existing service first (ignore errors if not loaded).
	exec.Command("launchctl", "unload", plistPath()).Run()

	if err := os.WriteFile(plistPath(), []byte(plistContent(serviceLabel, exe)), 0644); err != nil {
		return fmt.Errorf("write plist: %w", err)
	}

	if err := exec.Command("launchctl", "load", plistPath()).Run(); err != nil {
		return fmt.Errorf("launchctl load: %w", err)
	}

	// Ensure log directory exists for watchdog stderr.
	os.MkdirAll("/var/log/kaitu", 0755)

	// Reset stale healthcheck state from previous install/reboot.
	os.Remove("/var/run/k2-healthcheck-failures")

	// Install companion watchdog — external health check every 60s.
	// Uses the same binary path resolved above.
	exec.Command("launchctl", "unload", watchdogPlistPath()).Run()
	if err := os.WriteFile(watchdogPlistPath(), []byte(watchdogPlistContent(watchdogLabel, exe)), 0644); err != nil {
		return fmt.Errorf("write watchdog plist: %w", err)
	}
	if err := exec.Command("launchctl", "load", watchdogPlistPath()).Run(); err != nil {
		// Non-fatal: daemon works without watchdog.
		fmt.Fprintf(os.Stderr, "warning: watchdog install failed: %v\n", err)
	}

	return nil
}

func uninstallService() error {
	// Unload watchdog first (depends on daemon being installed).
	exec.Command("launchctl", "unload", watchdogPlistPath()).Run()
	os.Remove(watchdogPlistPath())

	// Unload daemon.
	exec.Command("launchctl", "unload", plistPath()).Run()
	os.Remove(plistPath())

	// Clean up healthcheck state.
	os.Remove("/var/run/k2-healthcheck-failures")

	return nil
}

func isServiceInstalled() bool {
	_, err := os.Stat(plistPath())
	return err == nil
}

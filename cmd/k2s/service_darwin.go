//go:build darwin

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const (
	k2sServiceLabel = "io.kaitu.k2s"
	k2sPlistDir     = "/Library/LaunchDaemons"
)

func k2sPlistPath() string {
	return filepath.Join(k2sPlistDir, k2sServiceLabel+".plist")
}

// installService installs k2s as a launchd daemon and starts it.
// Log output is handled by Go-level logging (lumberjack); launchd log
// redirection (StandardOutPath/StandardErrorPath) is intentionally omitted.
func installService() error {
	exe, err := resolveExecutable()
	if err != nil {
		return err
	}

	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>%s</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
        <string>run</string>
        <string>--foreground</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
</dict>
</plist>
`, k2sServiceLabel, exe)

	if err := os.WriteFile(k2sPlistPath(), []byte(plist), 0644); err != nil {
		return fmt.Errorf("write plist: %w", err)
	}

	if err := exec.Command("launchctl", "load", k2sPlistPath()).Run(); err != nil {
		return fmt.Errorf("launchctl load: %w", err)
	}

	return nil
}

// uninstallService unloads and removes the k2s launchd daemon.
func uninstallService() error {
	plistPath := k2sPlistPath()

	// Unload the service (ignore error if not loaded).
	_ = exec.Command("launchctl", "unload", plistPath).Run()

	// Remove the plist file.
	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove plist: %w", err)
	}

	return nil
}

// isServiceInstalled reports whether the k2s plist file exists.
func isServiceInstalled() bool {
	_, err := os.Stat(k2sPlistPath())
	return err == nil
}

// isServiceRunning reports whether k2s is currently running under launchd.
func isServiceRunning() bool {
	out, err := exec.Command("launchctl", "list", k2sServiceLabel).Output()
	return err == nil && len(out) > 0
}

// startService starts the k2s launchd service.
func startService() error {
	return exec.Command("launchctl", "start", k2sServiceLabel).Run()
}

// stopRunningService stops the k2s service if running.
func stopRunningService() {
	_ = exec.Command("launchctl", "stop", k2sServiceLabel).Run()
}

// installServiceWithConfig installs k2s as a launchd daemon with -c config flag and starts it.
func installServiceWithConfig(configPath string) error {
	exe, err := resolveExecutable()
	if err != nil {
		return err
	}

	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>%s</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
        <string>run</string>
        <string>--foreground</string>
        <string>-c</string>
        <string>%s</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
</dict>
</plist>
`, k2sServiceLabel, exe, configPath)

	// Unload existing if any
	_ = exec.Command("launchctl", "unload", k2sPlistPath()).Run()

	if err := os.WriteFile(k2sPlistPath(), []byte(plist), 0644); err != nil {
		return fmt.Errorf("write plist: %w", err)
	}

	if err := exec.Command("launchctl", "load", k2sPlistPath()).Run(); err != nil {
		return fmt.Errorf("launchctl load: %w", err)
	}

	return nil
}

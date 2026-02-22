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
func plistContent(label, exe string) string {
	logDir := "/tmp/k2"
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>%s</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
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
    <key>StandardOutPath</key>
    <string>%s/k2.log</string>
    <key>StandardErrorPath</key>
    <string>%s/k2.log</string>
</dict>
</plist>
`, label, exe, logDir, logDir)
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

	os.MkdirAll("/tmp/k2", 0755)

	if err := os.WriteFile(plistPath(), []byte(plistContent(serviceLabel, exe)), 0644); err != nil {
		return fmt.Errorf("write plist: %w", err)
	}

	if err := exec.Command("launchctl", "load", plistPath()).Run(); err != nil {
		return fmt.Errorf("launchctl load: %w", err)
	}

	return nil
}

func uninstallService() error {
	exec.Command("launchctl", "unload", plistPath()).Run()
	os.Remove(plistPath())
	return nil
}

func isServiceInstalled() bool {
	_, err := os.Stat(plistPath())
	return err == nil
}

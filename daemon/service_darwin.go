//go:build darwin

package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const (
	serviceLabel = "io.kaitu.k2"
	plistDir     = "/Library/LaunchDaemons"
)

func plistPath() string {
	return filepath.Join(plistDir, serviceLabel+".plist")
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

	logDir := "/tmp/k2"
	os.MkdirAll(logDir, 0755)

	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
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
    <key>StandardOutPath</key>
    <string>%s/k2.log</string>
    <key>StandardErrorPath</key>
    <string>%s/k2.log</string>
</dict>
</plist>
`, serviceLabel, exe, logDir, logDir)

	if err := os.WriteFile(plistPath(), []byte(plist), 0644); err != nil {
		return fmt.Errorf("write plist: %w", err)
	}

	// Load and start the service.
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

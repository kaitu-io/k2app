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

func installK2sService() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find executable: %w", err)
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}

	logDir := "/tmp/k2s"
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
        <string>run</string>
        <string>--foreground</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>%s/k2s.log</string>
    <key>StandardErrorPath</key>
    <string>%s/k2s.log</string>
</dict>
</plist>
`, k2sServiceLabel, exe, logDir, logDir)

	if err := os.WriteFile(k2sPlistPath(), []byte(plist), 0644); err != nil {
		return fmt.Errorf("write plist: %w", err)
	}

	if err := exec.Command("launchctl", "load", k2sPlistPath()).Run(); err != nil {
		return fmt.Errorf("launchctl load: %w", err)
	}

	return nil
}

func isK2sServiceInstalled() bool {
	_, err := os.Stat(k2sPlistPath())
	return err == nil
}

func isK2sServiceRunning() bool {
	out, err := exec.Command("launchctl", "list", k2sServiceLabel).Output()
	return err == nil && len(out) > 0
}

func startK2sService() error {
	return exec.Command("launchctl", "start", k2sServiceLabel).Run()
}

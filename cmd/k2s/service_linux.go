//go:build linux

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const (
	k2sServiceUnit = "k2s.service"
	k2sUnitDir     = "/etc/systemd/system"
)

func k2sUnitPath() string {
	return filepath.Join(k2sUnitDir, k2sServiceUnit)
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

	unit := fmt.Sprintf(`[Unit]
Description=k2s network tunnel server
After=network.target

[Service]
Type=simple
ExecStart=%s run --foreground
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
`, exe)

	if err := os.WriteFile(k2sUnitPath(), []byte(unit), 0644); err != nil {
		return fmt.Errorf("write unit: %w", err)
	}

	if err := exec.Command("systemctl", "daemon-reload").Run(); err != nil {
		return fmt.Errorf("daemon-reload: %w", err)
	}
	if err := exec.Command("systemctl", "enable", "--now", k2sServiceUnit).Run(); err != nil {
		return fmt.Errorf("enable service: %w", err)
	}

	return nil
}

func isK2sServiceInstalled() bool {
	_, err := os.Stat(k2sUnitPath())
	return err == nil
}

func isK2sServiceRunning() bool {
	err := exec.Command("systemctl", "is-active", "--quiet", k2sServiceUnit).Run()
	return err == nil
}

func startK2sService() error {
	return exec.Command("systemctl", "start", k2sServiceUnit).Run()
}

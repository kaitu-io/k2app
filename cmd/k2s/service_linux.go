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

// installService installs k2s as a systemd service and starts it.
// ExecStart uses only the binary path — the binary itself is the foreground
// server process (nginx-style, no "run --foreground" args needed).
func installService() error {
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
ExecStart=%s
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

// uninstallService disables and removes the k2s systemd service unit.
func uninstallService() error {
	// Stop and disable the service (ignore error if not active).
	_ = exec.Command("systemctl", "disable", "--now", k2sServiceUnit).Run()

	// Remove the unit file.
	if err := os.Remove(k2sUnitPath()); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove unit: %w", err)
	}

	// Reload systemd to pick up the removal.
	if err := exec.Command("systemctl", "daemon-reload").Run(); err != nil {
		return fmt.Errorf("daemon-reload: %w", err)
	}

	return nil
}

// isServiceInstalled reports whether the k2s systemd unit file exists.
func isServiceInstalled() bool {
	_, err := os.Stat(k2sUnitPath())
	return err == nil
}

// isServiceRunning reports whether k2s is currently active.
func isServiceRunning() bool {
	err := exec.Command("systemctl", "is-active", "--quiet", k2sServiceUnit).Run()
	return err == nil
}

// startService starts the k2s systemd service.
func startService() error {
	return exec.Command("systemctl", "start", k2sServiceUnit).Run()
}

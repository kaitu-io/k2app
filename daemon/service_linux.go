//go:build linux

package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const (
	serviceUnit = "k2.service"
	unitDir     = "/etc/systemd/system"
)

func unitPath() string {
	return filepath.Join(unitDir, serviceUnit)
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

	unit := fmt.Sprintf(`[Unit]
Description=k2 network tunnel daemon
After=network.target

[Service]
Type=simple
ExecStart=%s run
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
`, exe)

	if err := os.WriteFile(unitPath(), []byte(unit), 0644); err != nil {
		return fmt.Errorf("write unit file: %w", err)
	}

	if err := exec.Command("systemctl", "daemon-reload").Run(); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %w", err)
	}
	if err := exec.Command("systemctl", "enable", "--now", serviceUnit).Run(); err != nil {
		return fmt.Errorf("systemctl enable: %w", err)
	}

	return nil
}

func uninstallService() error {
	exec.Command("systemctl", "disable", "--now", serviceUnit).Run()
	os.Remove(unitPath())
	exec.Command("systemctl", "daemon-reload").Run()
	return nil
}

func isServiceInstalled() bool {
	_, err := os.Stat(unitPath())
	return err == nil
}

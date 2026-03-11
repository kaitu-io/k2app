//go:build linux

package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// systemd constants.
const (
	serviceUnit = "k2.service"
	unitDir     = "/etc/systemd/system"
)

// OpenWRT init.d constants.
const (
	initdScript = "/etc/init.d/k2"
)

func unitPath() string {
	return filepath.Join(unitDir, serviceUnit)
}

func installService() error {
	if isOpenWRT() {
		return installOpenWRT()
	}
	return installSystemd()
}

func uninstallService() error {
	if isOpenWRT() {
		return uninstallOpenWRT()
	}
	return uninstallSystemd()
}

func isServiceInstalled() bool {
	if isOpenWRT() {
		_, err := os.Stat(initdScript)
		return err == nil
	}
	_, err := os.Stat(unitPath())
	return err == nil
}

// isOpenWRT detects OpenWRT by checking for /etc/openwrt_release or fw4.
func isOpenWRT() bool {
	if _, err := os.Stat("/etc/openwrt_release"); err == nil {
		return true
	}
	if _, err := exec.LookPath("fw4"); err == nil {
		return true
	}
	return false
}

// --- systemd ---

func installSystemd() error {
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

func uninstallSystemd() error {
	exec.Command("systemctl", "disable", "--now", serviceUnit).Run()
	os.Remove(unitPath())
	exec.Command("systemctl", "daemon-reload").Run()
	return nil
}

// --- OpenWRT init.d/procd ---

func installOpenWRT() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find executable: %w", err)
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}

	script := fmt.Sprintf(`#!/bin/sh /etc/rc.common
# k2 network tunnel daemon

START=99
STOP=10

USE_PROCD=1

start_service() {
    procd_open_instance
    procd_set_param command %s run
    procd_set_param respawn
    procd_set_param stdout 1
    procd_set_param stderr 1
    procd_close_instance
}
`, exe)

	if err := os.WriteFile(initdScript, []byte(script), 0755); err != nil {
		return fmt.Errorf("write init.d script: %w", err)
	}

	if err := exec.Command(initdScript, "enable").Run(); err != nil {
		return fmt.Errorf("init.d enable: %w", err)
	}
	if err := exec.Command(initdScript, "start").Run(); err != nil {
		return fmt.Errorf("init.d start: %w", err)
	}

	return nil
}

func uninstallOpenWRT() error {
	exec.Command(initdScript, "stop").Run()
	exec.Command(initdScript, "disable").Run()
	os.Remove(initdScript)
	return nil
}

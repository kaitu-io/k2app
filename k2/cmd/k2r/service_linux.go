//go:build linux

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/kaitu-io/k2/gateway"
)

const (
	systemdUnit = "k2r.service"
	unitDir     = "/etc/systemd/system"
	initdScript = "/etc/init.d/k2r"
)

// InstallService installs k2r as a system service (systemd or OpenWRT procd).
func InstallService() error {
	if isOpenWRT() {
		return installOpenWRT()
	}
	return installSystemd()
}

// UninstallService removes the k2r system service and the firewall rules a
// k2r daemon installs.
func UninstallService() error {
	uninstall := uninstallSystemd
	if isOpenWRT() {
		uninstall = uninstallOpenWRT
	}
	return uninstallThenClearRules(uninstall, gateway.RemoveInterception)
}

// uninstallThenClearRules stops and removes the service, then clears the
// rules — in that order, so nothing is left running that could install them
// again. The daemon removes its own rules when it is stopped; this covers the
// ones it could not: a daemon that crashed, or a build from before it handled
// SIGTERM. Left in place they keep redirecting LAN DNS to a closed port.
func uninstallThenClearRules(uninstall func() error, clearRules func()) error {
	err := uninstall()
	clearRules()
	return err
}

// IsServiceInstalled reports whether the k2r service is installed.
func IsServiceInstalled() bool {
	if isOpenWRT() {
		_, err := os.Stat(initdScript)
		return err == nil
	}
	_, err := os.Stat(filepath.Join(unitDir, systemdUnit))
	return err == nil
}

func isOpenWRT() bool {
	if _, err := os.Stat("/etc/openwrt_release"); err == nil {
		return true
	}
	if _, err := exec.LookPath("fw4"); err == nil {
		return true
	}
	return false
}

func installSystemd() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	unit := fmt.Sprintf(`[Unit]
Description=k2r Gateway
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s run
Restart=always
RestartSec=5
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
`, exe)
	path := filepath.Join(unitDir, systemdUnit)
	if err := os.WriteFile(path, []byte(unit), 0644); err != nil {
		return err
	}
	_ = runCmd("systemctl", "daemon-reload")
	_ = runCmd("systemctl", "enable", systemdUnit)
	return runCmd("systemctl", "start", systemdUnit)
}

func uninstallSystemd() error {
	_ = runCmd("systemctl", "stop", systemdUnit)
	_ = runCmd("systemctl", "disable", systemdUnit)
	os.Remove(filepath.Join(unitDir, systemdUnit))
	_ = runCmd("systemctl", "daemon-reload")
	return nil
}

// generateOpenWRTInitScript renders the procd init.d script for the given
// k2r binary path. Extracted so unit tests can verify the procd parameters
// without writing to /etc/init.d/.
//
// `respawn 3600 5 0`: threshold=3600 s (count crashes within an hour),
// term grace 5 s, retry count 0 = INFINITE retries. Empirically verified
// on procd to mean infinite (15 respawns in 15 s). Without explicit args,
// procd defaults to retry=5 which permanently disables the service after
// 5 crashes in an hour — leaving a VPN gateway without iptables protection
// (fail-OPEN). Fix tracked as Bug 7 (2026-05-21).
func generateOpenWRTInitScript(exe string) string {
	return fmt.Sprintf(`#!/bin/sh /etc/rc.common

START=99
STOP=10
USE_PROCD=1

start_service() {
    procd_open_instance
    procd_set_param command %s run
    procd_set_param respawn 3600 5 0
    procd_set_param stdout 1
    procd_set_param stderr 1
    procd_close_instance
}
`, exe)
}

func installOpenWRT() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	script := generateOpenWRTInitScript(exe)
	if err := os.WriteFile(initdScript, []byte(script), 0755); err != nil {
		return err
	}
	_ = runCmd(initdScript, "enable")
	return runCmd(initdScript, "start")
}

func uninstallOpenWRT() error {
	_ = runCmd(initdScript, "stop")
	_ = runCmd(initdScript, "disable")
	os.Remove(initdScript)
	return nil
}

func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s %s: %s: %w", name, strings.Join(args, " "), out, err)
	}
	return nil
}

//go:build linux

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// systemd constants. Unit name matches packaging/linux/kaitu.service in the
// k2app repo so both install paths (tarball install.sh and `k2 service
// install`) produce a single, coherent unit on disk.
const (
	serviceUnit = "kaitu.service"
	unitDir     = "/etc/systemd/system"
)

func unitPath() string {
	return filepath.Join(unitDir, serviceUnit)
}

func installService() error {
	return installSystemd()
}

func uninstallService() error {
	return uninstallSystemd()
}

func isServiceInstalled() bool {
	_, err := os.Stat(unitPath())
	return err == nil
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

	// Unit content mirrors packaging/linux/kaitu.service (in the k2app
	// repo) so both install paths stay in lockstep. Hardening notes:
	//   - Ambient CAP_NET_ADMIN + CAP_NET_RAW let the daemon create TUN
	//     devices without UID 0 privileges on the rest of the syscall
	//     surface; CAP_SYS_ADMIN stays in the bounding set because some
	//     sing-tun paths still reach for it on exotic kernels.
	//   - ReadWritePaths=/etc/kaitu is declarative here (no ProtectSystem)
	//     but kept for parity with the packaging unit if that's ever added.
	unit := fmt.Sprintf(`[Unit]
Description=Kaitu VPN
Documentation=https://kaitu.io
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s run
Restart=on-failure
RestartSec=3
User=root

AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW
CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_RAW CAP_SYS_ADMIN
NoNewPrivileges=yes

ReadWritePaths=/etc/kaitu

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

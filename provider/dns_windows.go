//go:build windows

package provider

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

// windowsDNSState is a marker file written when the TUN provider sets DNS.
// If this file exists at daemon startup, the previous session crashed
// without calling Close().
type windowsDNSState struct {
	InterfaceName string    `json:"interface_name"` // "sing-tun"
	DNSServer     string    `json:"dns_server"`     // "198.18.0.8"
	Timestamp     time.Time `json:"timestamp"`
}

func dnsStateFilePath() string {
	programData := os.Getenv("ProgramData")
	if programData == "" {
		return filepath.Join(os.TempDir(), "k2", "dns-state.json")
	}
	return filepath.Join(programData, "k2", "dns-state.json")
}

// saveDNSState writes a marker file when TUN DNS is configured.
// Called from TUNProvider.Start() after sing-tun sets DNS.
func saveDNSState(ifaceName, dnsServer string) {
	state := windowsDNSState{
		InterfaceName: ifaceName,
		DNSServer:     dnsServer,
		Timestamp:     time.Now(),
	}
	data, err := json.Marshal(state)
	if err != nil {
		return
	}
	path := dnsStateFilePath()
	os.MkdirAll(filepath.Dir(path), 0700)
	os.WriteFile(path, data, 0600)
}

// removeDNSState deletes the marker file on clean TUN shutdown.
// Called from TUNProvider.Close().
func removeDNSState() {
	os.Remove(dnsStateFilePath())
}

// CleanupStaleDNSOverride detects and removes stale DNS settings and
// routes from a previous crash. Called at daemon startup (daemon.go).
func CleanupStaleDNSOverride() {
	path := dnsStateFilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		// No marker — still probe for stale interface as safety net.
		probeStaleTunInterface()
		return
	}

	var state windowsDNSState
	if err := json.Unmarshal(data, &state); err != nil {
		os.Remove(path)
		return
	}

	if time.Since(state.Timestamp) > 24*time.Hour {
		slog.Info("recovery: stale DNS marker too old, removing",
			"age", time.Since(state.Timestamp))
		os.Remove(path)
		return
	}

	slog.Info("recovery: found DNS state from previous session",
		"interface", state.InterfaceName, "dns", state.DNSServer)

	// Check if the TUN interface still exists.
	iface, err := net.InterfaceByName(state.InterfaceName)
	if err != nil {
		// Interface gone — wintun cleaned up on adapter GC.
		slog.Info("recovery: TUN interface already removed, no cleanup needed")
		os.Remove(path)
		return
	}

	slog.Warn("recovery: stale TUN interface detected, cleaning DNS and routes",
		"interface", state.InterfaceName, "index", iface.Index)

	resetInterfaceDNS(iface.Index)
	deleteStaleRoutes(iface.Index)
	flushDNSCache()

	os.Remove(path)
	slog.Info("recovery: Windows network cleanup complete")
}

// probeStaleTunInterface checks for a stale sing-tun interface even when
// the DNS state marker file is missing (e.g. crash between removeDNSState
// and actual TUN close). This is a safety net for edge cases.
func probeStaleTunInterface() {
	iface, err := net.InterfaceByName("sing-tun")
	if err != nil {
		return // no stale interface — normal case
	}
	slog.Warn("recovery: found stale sing-tun interface without marker",
		"index", iface.Index)
	resetInterfaceDNS(iface.Index)
	deleteStaleRoutes(iface.Index)
	flushDNSCache()
}

// resetInterfaceDNS resets DNS settings to DHCP for the given interface.
func resetInterfaceDNS(ifaceIndex int) {
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command",
		fmt.Sprintf("Set-DnsClientServerAddress -InterfaceIndex %d -ResetServerAddresses", ifaceIndex))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Run(); err != nil {
		slog.Warn("recovery: PowerShell DNS reset failed, trying netsh", "err", err)
		fallback := exec.Command("netsh", "interface", "ipv4", "set", "dnsservers",
			fmt.Sprintf("name=%d", ifaceIndex), "source=dhcp")
		fallback.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		if err := fallback.Run(); err != nil {
			slog.Error("recovery: netsh DNS reset also failed", "err", err)
		}
	}
}

// deleteStaleRoutes removes split default routes pointing to the stale TUN.
func deleteStaleRoutes(ifaceIndex int) {
	for _, dest := range []string{"0.0.0.0", "128.0.0.0"} {
		cmd := exec.Command("route", "delete", dest, "mask", "128.0.0.0",
			"if", fmt.Sprintf("%d", ifaceIndex))
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		cmd.Run() // best-effort
	}
}

func flushDNSCache() {
	cmd := exec.Command("ipconfig", "/flushdns")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	cmd.Run()
}

// dnsOverride is a no-op on Windows. DNS is set by sing-tun directly
// via luid.SetDNS(). Crash recovery is handled by CleanupStaleDNSOverride.
type dnsOverride struct{}

func newDNSOverride(tunDNS string) *dnsOverride { return nil }
func (d *dnsOverride) set() error               { return nil }
func (d *dnsOverride) monitor()                 {}
func (d *dnsOverride) stop()                    {}

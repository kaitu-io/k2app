//go:build linux

package gateway

import (
	"fmt"
	"log/slog"
	"os/exec"
	"strings"

	"github.com/kaitu-io/k2/config"
)

// runSlotWifiCmdsFn is a package-level indirection so tests can stub command
// execution without touching the real system (no real OpenWrt host in CI).
var runSlotWifiCmdsFn = runSlotWifiCmds

// validSSID: 1..32 bytes, no control chars. Defense in depth only —
// injection safety comes from argv-vector exec (no shell), not from here.
func validSSID(s string) bool {
	if len(s) == 0 || len(s) > 32 {
		return false
	}
	return !strings.ContainsAny(s, "\n\r\x00")
}

// validWifiPassword: WPA2-PSK passphrase, 8..63 bytes.
func validWifiPassword(s string) bool {
	if len(s) < 8 || len(s) > 63 {
		return false
	}
	return !strings.ContainsAny(s, "\n\r\x00")
}

func setSlotSSIDCmds(slot int, ssid string) [][]string {
	return [][]string{
		{"uci", "set", fmt.Sprintf("wireless.slot%d.ssid=%s", slot, ssid)},
		{"uci", "commit", "wireless"},
		{"wifi", "reload"},
	}
}

func setSlotPasswordCmds(slot int, password string) [][]string {
	return [][]string{
		{"uci", "set", fmt.Sprintf("wireless.slot%d.key=%s", slot, password)},
		{"uci", "commit", "wireless"},
		{"wifi", "reload"},
	}
}

// wifiSlotCommands converges the wireless plane to the manifest: bound
// slots get default SSID (unless customer-taken-over) + enabled; unbound
// slots are disabled. Only the three sanctioned write classes exist:
// rename / re-key / enable-disable. Firmware contract: wifi-iface named
// sections wireless.slot1..slot8.
func wifiSlotCommands(bindings []config.SlotBinding, local *slotLocalState) [][]string {
	bound := map[int]config.SlotBinding{}
	for _, b := range bindings {
		bound[b.Slot] = b
	}
	var cmds [][]string
	for slot := 1; slot <= maxSlots; slot++ {
		if b, ok := bound[slot]; ok {
			if !local.TakenOver[slot] {
				cmds = append(cmds, []string{"uci", "set", fmt.Sprintf("wireless.slot%d.ssid=%s", slot, slotSSID(b))})
			}
			cmds = append(cmds, []string{"uci", "set", fmt.Sprintf("wireless.slot%d.disabled=0", slot)})
		} else {
			cmds = append(cmds, []string{"uci", "set", fmt.Sprintf("wireless.slot%d.disabled=1", slot)})
		}
	}
	cmds = append(cmds, []string{"uci", "commit", "wireless"}, []string{"wifi", "reload"})
	return cmds
}

// applySlotWifi executes the convergence commands. Best-effort: a missing
// section (non-reference hardware) logs and continues — the tunnel plane
// must not die because wireless config is absent.
func applySlotWifi(bindings []config.SlotBinding, local *slotLocalState) error {
	if _, err := exec.LookPath("uci"); err != nil {
		slog.Warn("DIAG: uci-unavailable, skipping wifi convergence")
		return nil
	}
	for _, argv := range wifiSlotCommands(bindings, local) {
		if out, err := exec.Command(argv[0], argv[1:]...).CombinedOutput(); err != nil {
			slog.Warn("DIAG: uci-cmd-failed", "cmd", strings.Join(argv, " "), "err", err, "out", string(out))
		}
	}
	return nil
}

// runSlotWifiCmds executes a command batch, argv-vector only (no shell —
// SSID/password are user input; shell interpolation would be an injection
// surface). Stops at the first failure so callers can distinguish success
// from partial application.
func runSlotWifiCmds(cmds [][]string) error {
	for _, argv := range cmds {
		if out, err := exec.Command(argv[0], argv[1:]...).CombinedOutput(); err != nil {
			return fmt.Errorf("%s: %w (%s)", strings.Join(argv, " "), err, string(out))
		}
	}
	return nil
}

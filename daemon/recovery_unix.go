//go:build darwin || linux

package daemon

import "log/slog"

// restoreNetworkPlatform attempts platform-specific network restoration.
// sing-tun should auto-clean routes on Close(), but on crash we may need
// manual cleanup. For now, log the situation.
func restoreNetworkPlatform() {
	slog.Info("recovery: sing-tun should auto-restore routes on Close()")
	// TODO: If sing-tun doesn't clean up on crash, add manual route removal here.
	// On macOS: route delete default / networksetup -setdnsservers
	// On Linux: ip route del / resolvconf
}

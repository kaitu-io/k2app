//go:build windows

package daemon

import "log/slog"

// restoreNetworkPlatform attempts platform-specific network restoration.
// On Windows, sing-tun should auto-clean routes on Close().
func restoreNetworkPlatform() {
	slog.Info("recovery: sing-tun should auto-restore routes on Close()")
	// TODO: If sing-tun doesn't clean up on crash, add manual route/DNS removal here.
}

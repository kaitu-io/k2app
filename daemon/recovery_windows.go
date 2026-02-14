//go:build windows

package daemon

import "log"

// restoreNetworkPlatform attempts platform-specific network restoration.
// On Windows, sing-tun should auto-clean routes on Close().
func restoreNetworkPlatform() {
	log.Println("Recovery: sing-tun should auto-restore routes on Close()")
	// TODO: If sing-tun doesn't clean up on crash, add manual route/DNS removal here.
}

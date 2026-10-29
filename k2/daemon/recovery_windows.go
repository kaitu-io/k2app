//go:build windows

package daemon

import (
	"log/slog"

	"github.com/kaitu-io/k2/provider"
)

// restoreNetworkPlatform attempts Windows-specific network restoration
// after a panic. Best-effort cleanup of DNS and routes.
func restoreNetworkPlatform() {
	slog.Info("recovery: attempting Windows network restoration after panic")
	provider.CleanupStaleDNSOverride()
}

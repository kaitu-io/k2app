//go:build darwin && !ios

package daemon

import (
	"log/slog"

	"github.com/kaitu-io/k2/provider"
)

func restoreNetworkPlatform() {
	slog.Info("recovery: attempting macOS DNS restoration after panic")
	provider.CleanupStaleDNSOverride()
}

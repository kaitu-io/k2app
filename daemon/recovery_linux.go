//go:build linux && !android

package daemon

import "log/slog"

func restoreNetworkPlatform() {
	slog.Info("recovery: Linux DNS handled by resolvectl on interface removal")
}

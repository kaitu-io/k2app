package daemon

import "log/slog"

const (
	defaultMaxProcs = 4000
	defaultMaxFiles = 10240
)

// ApplyResourceLimits sets OS-level resource limits.
// Should be called early in main() before creating goroutines.
func ApplyResourceLimits() {
	if err := setMaxProcs(defaultMaxProcs); err != nil {
		slog.Warn("failed to set max processes", "err", err)
	}
	if err := setMaxFiles(defaultMaxFiles); err != nil {
		slog.Warn("failed to set max files", "err", err)
	}
}

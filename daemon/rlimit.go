package daemon

import "log"

const (
	defaultMaxProcs = 4000
	defaultMaxFiles = 10240
)

// ApplyResourceLimits sets OS-level resource limits.
// Should be called early in main() before creating goroutines.
func ApplyResourceLimits() {
	if err := setMaxProcs(defaultMaxProcs); err != nil {
		log.Printf("Warning: failed to set max processes: %v", err)
	}
	if err := setMaxFiles(defaultMaxFiles); err != nil {
		log.Printf("Warning: failed to set max files: %v", err)
	}
}

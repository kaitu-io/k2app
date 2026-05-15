//go:build !darwin && !linux && !windows

package daemon

import "github.com/kaitu-io/k2/provider"

// newProcessSearcher returns nil on platforms that don't support
// process-name attribution from the daemon shell (e.g. iOS, where appext
// owns the engine and wires its own searcher).
func newProcessSearcher() provider.ProcessSearcher {
	return nil
}

//go:build !darwin || ios

package daemon

import "github.com/kaitu-io/k2/provider"

// newProcessSearcher returns nil on non-Darwin desktop platforms.
// Linux and Windows process-name attribution is not yet wired into the
// desktop daemon (Linux has provider.LinuxProcessSearcher but it requires
// a PackageResolver which is Android-specific).
func newProcessSearcher() provider.ProcessSearcher {
	return nil
}

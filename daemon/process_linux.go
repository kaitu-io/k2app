//go:build linux

package daemon

import "github.com/kaitu-io/k2/provider"

// newProcessSearcher returns a Linux desktop ProcessSearcher.
// Desktop builds pass nil PackageResolver — Android-only UID→package mapping
// is wired separately in appext.
func newProcessSearcher() provider.ProcessSearcher {
	return provider.NewLinuxProcessSearcher(nil)
}

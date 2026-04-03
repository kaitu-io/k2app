//go:build !linux

package appext

import "github.com/kaitu-io/k2/provider"

// makeProcessSearcher returns nil on non-Linux platforms.
// Process search is handled differently (Darwin uses lsof via DarwinProcessSearcher).
func makeProcessSearcher(_ PackageResolver) provider.ProcessSearcher {
	return nil
}

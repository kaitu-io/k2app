//go:build linux

package appext

import "github.com/kaitu-io/k2/provider"

// makeProcessSearcher creates a platform-appropriate ProcessSearcher.
// On Linux (Android), creates LinuxProcessSearcher with PackageResolver.
func makeProcessSearcher(pr PackageResolver) provider.ProcessSearcher {
	return provider.NewLinuxProcessSearcher(pr)
}

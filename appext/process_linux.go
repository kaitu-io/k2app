//go:build linux

package appext

import "github.com/kaitu-io/k2/provider"

// makeProcessSearcher creates a platform-appropriate ProcessSearcher.
//
// Android with a ConnectionOwner callback (API 29+ path): the owner-UID
// searcher, which asks the platform (getConnectionOwnerUid) instead of
// scanning /proc — Android 10+ SELinux/hidepid made the /proc path return
// empty for every foreign-app socket, silently disabling all package rules.
// The /proc LinuxProcessSearcher stays wired as its fallback (Android ≤9,
// destination-less lookups).
//
// Without the callback (old Kotlin shell), behavior is unchanged: /proc only.
func makeProcessSearcher(cfg *EngineConfig) provider.ProcessSearcher {
	fallback := provider.NewLinuxProcessSearcher(cfg.PackageResolver)
	if cfg.ConnectionOwner == nil {
		return fallback
	}
	return provider.NewOwnerProcessSearcher(cfg.ConnectionOwner, cfg.PackageResolver, fallback)
}

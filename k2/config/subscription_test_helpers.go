//go:build !production

package config

// NewSubscriptionForTest is an in-package test helper that builds a
// *Subscription with a fixed Tunnels list and no HTTP fetcher. Used only
// by tests in subscription/ and consumers that need a deterministic
// Subscription without spinning up an HTTP server.
//
// Lives in package config so it can populate the unexported `tunnels`
// field directly — no exported API surface change.
func NewSubscriptionForTest(tunnels []Tunnel) *Subscription {
	return &Subscription{tunnels: tunnels}
}

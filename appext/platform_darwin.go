//go:build darwin

package appext

// isNetworkExtension indicates appext is running under Apple's NEPacketTunnelProvider.
// All Darwin appext builds (iOS + macOS sysext) use NetworkExtension.
const isNetworkExtension = true

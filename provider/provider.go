package provider

import "io"

// Provider represents a platform-specific way to read/write IP packets
// This could be a TUN device, PacketTunnelProvider (iOS), or iptables redirect (OpenWRT)
type Provider interface {
	io.ReadWriteCloser

	// Name returns the provider name (e.g., "tun0")
	Name() string

	// MTU returns the maximum transmission unit
	MTU() int
}

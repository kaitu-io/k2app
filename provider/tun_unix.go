//go:build darwin || linux

package provider

import (
	"fmt"
	"net/netip"

	"github.com/sagernet/sing-tun"
)

// TUNProvider implements Provider using a TUN device
type TUNProvider struct {
	tunIf tun.Tun
	name  string
	mtu   int
}

// Default network addresses to avoid conflicts with real networks
// Using RFC 2544 test network range (198.18.0.0/15)
const (
	// IPv4: 198.18.0.7/15 (198.18.0.0 - 198.19.255.255)
	DefaultIPv4Address = "198.18.0.7/15"

	// IPv6: fdfe:dcba:9876::7/64 (ULA - Unique Local Address)
	DefaultIPv6Address = "fdfe:dcba:9876::7/64"

	// DefaultMTU is optimized for QUIC overhead (1500 - 100)
	DefaultMTU = 1400
)

// NewTUNProvider creates a new TUN device provider with automatic configuration
// Device name is auto-allocated by sing-tun (e.g., utun0, utun1)
// MTU is hardcoded to 1400 (optimized for QUIC overhead)
// IP addresses are hardcoded to RFC 2544 test network ranges
// Requires root/sudo privileges
func NewTUNProvider() (*TUNProvider, error) {
	// Parse hardcoded IPv4 address
	ipv4Prefix, err := netip.ParsePrefix(DefaultIPv4Address)
	if err != nil {
		return nil, fmt.Errorf("parse IPv4 prefix: %w", err)
	}

	// Parse hardcoded IPv6 address
	ipv6Prefix, err := netip.ParsePrefix(DefaultIPv6Address)
	if err != nil {
		return nil, fmt.Errorf("parse IPv6 prefix: %w", err)
	}

	// Build sing-tun options
	tunOptions := tun.Options{
		Name:         "", // Empty = auto-allocate by sing-tun
		MTU:          DefaultMTU,
		Inet4Address: []netip.Prefix{ipv4Prefix},
		Inet6Address: []netip.Prefix{ipv6Prefix},
		AutoRoute:    true, // Let sing-tun manage routing table automatically
	}

	// Create and start TUN interface
	tunIf, err := tun.New(tunOptions)
	if err != nil {
		return nil, fmt.Errorf("create TUN device: %w", err)
	}

	if err := tunIf.Start(); err != nil {
		tunIf.Close()
		return nil, fmt.Errorf("start TUN device: %w", err)
	}

	// Get the actual allocated device name
	deviceName, err := tunIf.Name()
	if err != nil {
		tunIf.Close()
		return nil, fmt.Errorf("get TUN device name: %w", err)
	}

	return &TUNProvider{
		tunIf: tunIf,
		name:  deviceName,
		mtu:   DefaultMTU,
	}, nil
}

// Read reads an IP packet from the TUN device
func (p *TUNProvider) Read(buf []byte) (int, error) {
	n, err := p.tunIf.Read(buf)
	if err != nil {
		return 0, fmt.Errorf("read from TUN: %w", err)
	}
	return n, nil
}

// Write writes an IP packet to the TUN device
func (p *TUNProvider) Write(buf []byte) (int, error) {
	n, err := p.tunIf.Write(buf)
	if err != nil {
		return 0, fmt.Errorf("write to TUN: %w", err)
	}
	return n, nil
}

// Close closes the TUN device
func (p *TUNProvider) Close() error {
	return p.tunIf.Close()
}

// Name returns the TUN device name
func (p *TUNProvider) Name() string {
	return p.name
}

// MTU returns the TUN device MTU
func (p *TUNProvider) MTU() int {
	return p.mtu
}

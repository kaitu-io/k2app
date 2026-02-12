package provider

import (
	"context"
	"net"
	"net/netip"
)

// Provider represents a platform-specific source of TCP/UDP connections.
// Implementations include TUN devices (via sing-tun stack), proxy servers
// (SOCKS5/HTTP), and mobile network extensions (iOS/Android FD).
type Provider interface {
	Start(ctx context.Context, handler ConnectionHandler) error
	Close() error
	Name() string
}

// ConnectionHandler processes TCP and UDP connections from a Provider.
type ConnectionHandler interface {
	HandleTCP(conn net.Conn, dest string)
	HandleUDP(conn net.PacketConn, dest string)
}

// ProviderConfig holds configuration for creating a Provider.
type ProviderConfig struct {
	Mode           string       // "tun" (default) or "proxy"
	FileDescriptor int          // 0 = create TUN; >0 = use existing FD (iOS/Android)
	DNSServers     []netip.Addr // excluded from TUN route (anti-loop)
	MTU            int          // default 1400
	ProxyListen string // proxy mode: listen address (default "127.0.0.1:1080")
}

// DefaultMTU is optimized for QUIC overhead (1500 - 100).
const DefaultMTU = 1400

// SetDefaults fills in zero-value fields with defaults.
func (c *ProviderConfig) SetDefaults() {
	if c.Mode == "" {
		c.Mode = "tun"
	}
	if c.MTU == 0 {
		c.MTU = DefaultMTU
	}
	if c.ProxyListen == "" {
		c.ProxyListen = "127.0.0.1:1080"
	}
}

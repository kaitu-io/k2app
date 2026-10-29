package provider

import (
	"context"
	"net"
	"net/netip"
	"os"
)

// closeRawFD releases a platform-supplied TUN fd that was never handed to
// sing-tun (Close before Start, or Start failed before tun.New consumed it).
// Uses os.NewFile instead of syscall.Close so the package compiles on
// Windows, where this path is unreachable (FileDescriptor is only > 0 on
// mobile / NE builds).
func closeRawFD(fd int) {
	if fd > 0 {
		_ = os.NewFile(uintptr(fd), "k2-tun-fd").Close()
	}
}

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

// UDPRejecter sends ICMP Port Unreachable for a UDP connection.
// Implemented by TUN-backed connections (singPacketConn).
// Non-TUN providers (Proxy, TProxy) don't implement this — type assert fails gracefully.
type UDPRejecter interface {
	RejectHandshake() error
}

// ProviderConfig holds configuration for creating a Provider.
type ProviderConfig struct {
	Mode           string       // "tun" (default) or "proxy"
	FileDescriptor int          // 0 = create TUN; >0 = use existing FD (iOS/Android)
	RouteExcludeAddrs []netip.Addr // IPs excluded from TUN routing (direct DNS, wire server)
	MTU            int          // default 1400 (desktop), platform-aware on mobile
	ProxyListen    string       // proxy mode: listen address (default "127.0.0.1:1080")
	IPv4Address    string       // TUN IPv4 CIDR, e.g. "10.0.0.2/24". Empty = platform default.
	IPv6Address    string       // TUN IPv6 CIDR, e.g. "fd00::2/128". Empty = platform default.

	// NetworkExtension indicates the tunnel runs under Apple's NEPacketTunnelProvider.
	// When true, MTU is capped at NetworkExtensionMTU to avoid a known kernel perf bug.
	NetworkExtension bool

	// InterfaceMonitor is a sing-tun DefaultInterfaceMonitor for TUN self-exclusion.
	// Passed from engine.Config. Type assertion done in platform-specific provider.
	InterfaceMonitor any
}

const (
	// DefaultMTU for the TUN interface on desktop. Aligned with sing-box: TUN MTU is
	// independent of wire (QUIC/TCP) MTU. A large TUN MTU reduces IP fragmentation
	// and system calls in the gVisor/system stack. Wire transports manage their own
	// packet sizes independently.
	DefaultMTU = 65535

	// NetworkExtensionMTU is the Apple NetworkExtension performance ceiling.
	// MTU > 4064 (4096 - UTUN_IF_HEADROOM_SIZE) causes severe throughput degradation.
	NetworkExtensionMTU = 4064
)

// SetDefaults fills in zero-value fields with defaults.
func (c *ProviderConfig) SetDefaults() {
	if c.Mode == "" {
		c.Mode = "tun"
	}
	if c.MTU == 0 {
		switch {
		case c.NetworkExtension:
			c.MTU = NetworkExtensionMTU
		case platformMaxMTU > 0:
			c.MTU = platformMaxMTU
		default:
			c.MTU = DefaultMTU
		}
	}
	// Cap to platform limits regardless of explicit value.
	if c.NetworkExtension && c.MTU > NetworkExtensionMTU {
		c.MTU = NetworkExtensionMTU
	} else if platformMaxMTU > 0 && c.MTU > platformMaxMTU {
		c.MTU = platformMaxMTU
	}
	if c.ProxyListen == "" {
		c.ProxyListen = "127.0.0.1:1080"
	}
}

// Package directdial provides a network dialer bound to a physical interface,
// bypassing TUN/VPN routing. This prevents routing loops when the k2 client
// needs to reach the k2s server through the real network while TUN is active.
//
// Zero dependencies on k2 internals — extractable to its own module.
package directdial

import (
	"context"
	"net"
	"strings"
	"syscall"
)

// Dialer creates network connections bound to a physical interface,
// bypassing TUN/VPN routing.
type Dialer struct {
	ifName string // physical interface name (e.g. "en0", "eth0")
	ifIdx  int    // interface index (for macOS IP_BOUND_IF)
}

// New detects the default physical interface and returns a Dialer bound to it.
// Returns a no-op dialer (nil control func) if detection fails.
func New() *Dialer {
	name, idx := detectDefault()
	return &Dialer{ifName: name, ifIdx: idx}
}

// NewWithInterface creates a Dialer bound to the named interface.
func NewWithInterface(ifName string) (*Dialer, error) {
	iface, err := net.InterfaceByName(ifName)
	if err != nil {
		return nil, err
	}
	return &Dialer{ifName: ifName, ifIdx: iface.Index}, nil
}

// DialContext dials TCP/UDP bound to the physical interface.
func (d *Dialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	nd := &net.Dialer{Control: d.control()}
	return nd.DialContext(ctx, network, addr)
}

// ListenPacket creates a UDP socket bound to the physical interface.
func (d *Dialer) ListenPacket(network, addr string) (net.PacketConn, error) {
	lc := &net.ListenConfig{Control: d.control()}
	return lc.ListenPacket(context.Background(), network, addr)
}

// InterfaceName returns the bound interface name (for logging).
func (d *Dialer) InterfaceName() string { return d.ifName }

// isIPv6Network returns true if the network string indicates IPv6.
func isIPv6Network(network string) bool {
	return strings.Contains(network, "6") || strings.Contains(network, "IPv6")
}

// controlFunc is the type signature for net.Dialer.Control / net.ListenConfig.Control.
type controlFunc func(network, address string, c syscall.RawConn) error

// Package directdial provides a network dialer bound to a physical interface,
// bypassing TUN/VPN routing. This prevents routing loops when the k2 client
// needs to reach the k2s server through the real network while TUN is active.
//
// Uses sing-tun's DefaultInterfaceMonitor for dynamic interface tracking and
// BindToInterfaceFunc for cross-platform socket binding
// (darwin IP_BOUND_IF, linux SO_BINDTODEVICE, others no-op).
package directdial

import (
	"context"
	"net"

	tun "github.com/sagernet/sing-tun"

	"github.com/sagernet/sing/common/control"
)

// Dialer creates network connections bound to a physical interface,
// bypassing TUN/VPN routing. Interface resolution is dynamic — every
// dial reads the current default interface from the monitor.
type Dialer struct {
	finder  control.InterfaceFinder
	monitor tun.DefaultInterfaceMonitor
}

// New creates a Dialer that dynamically resolves the default interface
// from a sing-tun DefaultInterfaceMonitor. Interface changes (WiFi→Ethernet)
// are picked up automatically on every dial.
func New(monitor tun.DefaultInterfaceMonitor) *Dialer {
	return &Dialer{
		finder:  control.NewDefaultInterfaceFinder(),
		monitor: monitor,
	}
}

// DialContext dials TCP/UDP bound to the physical interface.
func (d *Dialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	nd := &net.Dialer{Control: d.controlFunc()}
	return nd.DialContext(ctx, network, addr)
}

// ListenPacket creates a UDP socket bound to the physical interface.
func (d *Dialer) ListenPacket(network, addr string) (net.PacketConn, error) {
	lc := &net.ListenConfig{Control: d.controlFunc()}
	return lc.ListenPacket(context.Background(), network, addr)
}

// InterfaceName returns the current default interface name (for logging).
func (d *Dialer) InterfaceName() string {
	if iface := d.monitor.DefaultInterface(); iface != nil {
		return iface.Name
	}
	return ""
}

// controlFunc returns a socket control function that binds to the physical interface.
// Uses sing's BindToInterfaceFunc for cross-platform binding.
func (d *Dialer) controlFunc() control.Func {
	return control.BindToInterfaceFunc(d.finder, func(network, address string) (string, int, error) {
		iface := d.monitor.DefaultInterface()
		if iface == nil {
			return "", -1, nil
		}
		return iface.Name, iface.Index, nil
	})
}

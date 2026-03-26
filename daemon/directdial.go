package daemon

// directDialer creates network connections bound to a physical interface,
// bypassing TUN/VPN routing. This prevents routing loops when the k2 client
// needs to reach the k2s server through the real network while TUN is active.
//
// Uses sing-tun's DefaultInterfaceMonitor for dynamic interface tracking and
// BindToInterfaceFunc for cross-platform socket binding
// (darwin IP_BOUND_IF, linux SO_BINDTODEVICE, others no-op).

import (
	"context"
	"net"
	"syscall"
	"time"

	tun "github.com/sagernet/sing-tun"

	"github.com/sagernet/sing/common/control"
)

// directDialer creates network connections bound to a physical interface,
// bypassing TUN/VPN routing. Interface resolution is dynamic — every
// dial reads the current default interface from the monitor.
type directDialer struct {
	finder  control.InterfaceFinder
	monitor tun.DefaultInterfaceMonitor
}

// newDirectDialer creates a directDialer that dynamically resolves the default
// interface from a sing-tun DefaultInterfaceMonitor. Interface changes
// (WiFi→Ethernet) are picked up automatically on every dial.
func newDirectDialer(monitor tun.DefaultInterfaceMonitor) *directDialer {
	return &directDialer{
		finder:  control.NewDefaultInterfaceFinder(),
		monitor: monitor,
	}
}

// DialContext dials TCP/UDP bound to the physical interface.
// Timeout is derived from the context deadline (set by tcpwsConnectTimeout
// in connectTLSWebSocket). Additionally, we set an explicit Timeout as
// belt-and-suspenders for kernel-level enforcement — context cancel alone
// doesn't reliably interrupt macOS TCP SYN retransmit (can block 75s).
func (d *directDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	// Derive timeout from context deadline if set, otherwise use 10s default.
	var timeout time.Duration
	if deadline, ok := ctx.Deadline(); ok {
		timeout = time.Until(deadline)
	} else {
		timeout = 10 * time.Second
	}
	nd := &net.Dialer{
		Timeout: timeout,
		Control: d.controlFunc(),
	}
	return nd.DialContext(ctx, network, addr)
}

// ListenPacket creates a UDP socket bound to the physical interface.
func (d *directDialer) ListenPacket(network, addr string) (net.PacketConn, error) {
	lc := &net.ListenConfig{Control: d.controlFunc()}
	return lc.ListenPacket(context.Background(), network, addr)
}

// interfaceName returns the current default interface name (for logging).
func (d *directDialer) interfaceName() string {
	if iface := d.monitor.DefaultInterface(); iface != nil {
		return iface.Name
	}
	return ""
}

// controlFunc returns a socket control function that binds to the physical interface.
// When DefaultInterface() returns nil (transient during TUN route setup on Windows),
// skip binding instead of passing empty name to BindToInterface0 which would fail
// with "interface not found: ".
func (d *directDialer) controlFunc() control.Func {
	return func(network, address string, conn syscall.RawConn) error {
		iface := d.monitor.DefaultInterface()
		if iface == nil {
			return nil // skip binding — interface unknown during TUN route setup
		}
		return control.BindToInterface0(d.finder, conn, network, address, iface.Name, iface.Index, false)
	}
}

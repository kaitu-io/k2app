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
	"log/slog"
	"net"
	"sync/atomic"
	"syscall"
	"time"

	tun "github.com/sagernet/sing-tun"

	"github.com/sagernet/sing/common/control"
)

// directDialer creates network connections bound to a physical interface,
// bypassing TUN/VPN routing. Interface resolution is dynamic — every
// dial reads the current default interface from the monitor.
type directDialer struct {
	finder    control.InterfaceFinder
	monitor   tun.DefaultInterfaceMonitor
	dialCount atomic.Uint64
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
//
// Observability: first 20 dials always logged, then 1-in-100 sampled, plus all
// nil-iface and bind-error paths. Logs are INFO level so default-config users
// can capture them for diagnostic uploads (ticket #2186 Windows TUN direct fail).
func (d *directDialer) controlFunc() control.Func {
	return func(network, address string, conn syscall.RawConn) error {
		count := d.dialCount.Add(1)
		shouldLog := count <= 20 || count%100 == 0

		iface := d.monitor.DefaultInterface()
		if iface == nil {
			slog.Info("directdial: skip bind (no default interface)",
				"network", network,
				"address", address,
				"dialCount", count,
			)
			return nil
		}

		err := control.BindToInterface0(d.finder, conn, network, address, iface.Name, iface.Index, false)
		if err != nil {
			slog.Warn("directdial: bind failed",
				"network", network,
				"address", address,
				"iface", iface.Name,
				"ifaceIdx", iface.Index,
				"err", err,
			)
			return err
		}

		if shouldLog {
			readbackIdx, readbackErr := readUnicastIfV4(conn)
			slog.Info("directdial: bound",
				"network", network,
				"address", address,
				"iface", iface.Name,
				"ifaceIdx", iface.Index,
				"readbackIdx", readbackIdx,
				"readbackErr", readbackErr,
				"dialCount", count,
			)
		}
		return nil
	}
}

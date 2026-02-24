//go:build darwin

package appext

// nePlatform is the Darwin NE platform networking component.
// It provides interface-bound dialing, network change detection,
// and TUN self-exclusion — all sharing one DefaultInterfaceMonitor.
//
// Mirrors daemon's architecture:
//
//	daemon: directDialer + singTunMonitor + ifaceMon (3 separate types, shared monitor)
//	appext: nePlatform (1 type, 3 roles, same shared monitor)
//
// Implements:
//   - wire.DirectDialer (DialContext, ListenPacket)
//   - wire.DialerController (ControlFunc)
//   - engine.NetworkChangeNotifier (Start, Close)

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"syscall"

	tun "github.com/sagernet/sing-tun"
	"github.com/sagernet/sing/common/control"
)

type nePlatform struct {
	finder   control.InterfaceFinder
	netMon   tun.NetworkUpdateMonitor
	ifaceMon tun.DefaultInterfaceMonitor
	closed   sync.Once
}

// newNEPlatform creates the Darwin NE platform networking component.
// Uses UnderNetworkExtension: true so sing-tun detects the default interface
// via socket probe (getDefaultInterfaceBySocket) instead of route table parsing,
// which reflects VPN routes inside the NE process.
func newNEPlatform() (*nePlatform, error) {
	netMon, err := tun.NewNetworkUpdateMonitor(&neSlogAdapter{})
	if err != nil {
		return nil, fmt.Errorf("NE network monitor: %w", err)
	}
	finder := control.NewDefaultInterfaceFinder()
	ifaceMon, err := tun.NewDefaultInterfaceMonitor(netMon, &neSlogAdapter{}, tun.DefaultInterfaceMonitorOptions{
		InterfaceFinder:       finder,
		UnderNetworkExtension: true,
	})
	if err != nil {
		netMon.Close()
		return nil, fmt.Errorf("NE interface monitor: %w", err)
	}
	return &nePlatform{
		finder:   finder,
		netMon:   netMon,
		ifaceMon: ifaceMon,
	}, nil
}

// --- wire.DirectDialer ---

func (p *nePlatform) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	nd := &net.Dialer{Control: p.controlFunc()}
	return nd.DialContext(ctx, network, addr)
}

func (p *nePlatform) ListenPacket(network, addr string) (net.PacketConn, error) {
	lc := &net.ListenConfig{Control: p.controlFunc()}
	return lc.ListenPacket(context.Background(), network, addr)
}

// --- wire.DialerController ---

func (p *nePlatform) ControlFunc() func(network, address string, c syscall.RawConn) error {
	return p.controlFunc()
}

// --- engine.NetworkChangeNotifier ---

func (p *nePlatform) Start(callback func()) error {
	p.ifaceMon.RegisterCallback(func(defaultInterface *control.Interface, flags int) {
		if callback != nil {
			slog.Debug("NE network: default interface changed",
				"interface", defaultInterface.Name, "flags", flags)
			callback()
		}
	})
	if err := p.netMon.Start(); err != nil {
		return err
	}
	return p.ifaceMon.Start()
}

// Close stops monitors. Idempotent — engine.Stop() calls this via NetworkMonitor.Close().
func (p *nePlatform) Close() error {
	p.closed.Do(func() {
		p.ifaceMon.Close()
		p.netMon.Close()
	})
	return nil
}

// --- Accessor ---

// InterfaceMonitor returns the shared DefaultInterfaceMonitor for engine.Config.InterfaceMonitor.
// The TUN provider uses this for self-exclusion via RegisterMyInterface.
func (p *nePlatform) InterfaceMonitor() tun.DefaultInterfaceMonitor {
	return p.ifaceMon
}

// interfaceName returns the current default interface name (for logging).
func (p *nePlatform) interfaceName() string {
	if iface := p.ifaceMon.DefaultInterface(); iface != nil {
		return iface.Name
	}
	return ""
}

// --- internal ---

func (p *nePlatform) controlFunc() control.Func {
	return control.BindToInterfaceFunc(p.finder, func(network, address string) (string, int, error) {
		iface := p.ifaceMon.DefaultInterface()
		if iface == nil {
			return "", -1, nil
		}
		return iface.Name, iface.Index, nil
	})
}

// neSlogAdapter bridges sing's logger to Go's slog.
type neSlogAdapter struct{}

func (l *neSlogAdapter) Trace(args ...any) { slog.Debug("[sing-tun] " + fmt.Sprint(args...)) }
func (l *neSlogAdapter) Debug(args ...any) { slog.Debug("[sing-tun] " + fmt.Sprint(args...)) }
func (l *neSlogAdapter) Info(args ...any)  { slog.Info("[sing-tun] " + fmt.Sprint(args...)) }
func (l *neSlogAdapter) Warn(args ...any)  { slog.Warn("[sing-tun] " + fmt.Sprint(args...)) }
func (l *neSlogAdapter) Error(args ...any) { slog.Error("[sing-tun] " + fmt.Sprint(args...)) }
func (l *neSlogAdapter) Fatal(args ...any) { slog.Error("[sing-tun] FATAL: " + fmt.Sprint(args...)) }
func (l *neSlogAdapter) Panic(args ...any) { slog.Error("[sing-tun] PANIC: " + fmt.Sprint(args...)) }

package daemon

import (
	"fmt"
	"log/slog"

	tun "github.com/sagernet/sing-tun"
	"github.com/sagernet/sing/common/control"

	"github.com/kaitu-io/k2/engine"
)

// singTunMonitor adapts sing-tun's DefaultInterfaceMonitor to engine.NetworkChangeNotifier.
type singTunMonitor struct {
	networkMonitor   tun.NetworkUpdateMonitor
	interfaceMonitor tun.DefaultInterfaceMonitor
}

// NewNetworkMonitor creates a cross-platform network monitor using sing-tun.
// Returns (adapter, interfaceMonitor, error).
// The interfaceMonitor must be passed to engine.Config.InterfaceMonitor (same instance)
// so sing-tun excludes the TUN device from triggering network change events.
// Returns error on platforms without support (e.g., containers).
func NewNetworkMonitor() (engine.NetworkChangeNotifier, tun.DefaultInterfaceMonitor, error) {
	netMon, err := tun.NewNetworkUpdateMonitor(&slogAdapter{})
	if err != nil {
		return nil, nil, err
	}
	ifaceMon, err := tun.NewDefaultInterfaceMonitor(netMon, &slogAdapter{}, tun.DefaultInterfaceMonitorOptions{
		InterfaceFinder: control.NewDefaultInterfaceFinder(),
	})
	if err != nil {
		netMon.Close()
		return nil, nil, err
	}
	return &singTunMonitor{
		networkMonitor:   netMon,
		interfaceMonitor: ifaceMon,
	}, ifaceMon, nil
}

func (m *singTunMonitor) Start(callback func()) error {
	m.interfaceMonitor.RegisterCallback(func(defaultInterface *control.Interface, flags int) {
		if callback == nil {
			return
		}
		if defaultInterface == nil {
			slog.Debug("network: default interface changed but nil, skipping", "callbackFlags", flags)
			return
		}
		slog.Debug("network: default interface changed",
			"interface", defaultInterface.Name,
			"index", defaultInterface.Index,
			"mtu", defaultInterface.MTU,
			"ifFlags", defaultInterface.Flags.String(),
			"addrs", defaultInterface.Addresses,
			"callbackFlags", flags,
		)
		callback()
	})
	if err := m.networkMonitor.Start(); err != nil {
		return err
	}
	return m.interfaceMonitor.Start()
}

func (m *singTunMonitor) Close() error {
	m.interfaceMonitor.Close()
	return m.networkMonitor.Close()
}

// slogAdapter bridges sing's logger.Logger to Go's slog.
type slogAdapter struct{}

func (l *slogAdapter) Trace(args ...any) { slog.Debug("[sing-tun] " + fmt.Sprint(args...)) }
func (l *slogAdapter) Debug(args ...any) { slog.Debug("[sing-tun] " + fmt.Sprint(args...)) }
func (l *slogAdapter) Info(args ...any)  { slog.Info("[sing-tun] " + fmt.Sprint(args...)) }
func (l *slogAdapter) Warn(args ...any)  { slog.Warn("[sing-tun] " + fmt.Sprint(args...)) }
func (l *slogAdapter) Error(args ...any) { slog.Error("[sing-tun] " + fmt.Sprint(args...)) }
func (l *slogAdapter) Fatal(args ...any) { slog.Error("[sing-tun] FATAL: " + fmt.Sprint(args...)) }
func (l *slogAdapter) Panic(args ...any) { slog.Error("[sing-tun] PANIC: " + fmt.Sprint(args...)) }

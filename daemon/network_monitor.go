package daemon

import (
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
	netMon, err := tun.NewNetworkUpdateMonitor((*nopLogger)(nil))
	if err != nil {
		return nil, nil, err
	}
	ifaceMon, err := tun.NewDefaultInterfaceMonitor(netMon, (*nopLogger)(nil), tun.DefaultInterfaceMonitorOptions{})
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
		if callback != nil {
			slog.Debug("network: default interface changed", "interface", defaultInterface.Name, "flags", flags)
			callback()
		}
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

// nopLogger satisfies sing's logger.Logger interface with no-ops.
type nopLogger struct{}

func (l *nopLogger) Trace(args ...any) {}
func (l *nopLogger) Debug(args ...any) {}
func (l *nopLogger) Info(args ...any)  {}
func (l *nopLogger) Warn(args ...any)  {}
func (l *nopLogger) Error(args ...any) {}
func (l *nopLogger) Fatal(args ...any) {}
func (l *nopLogger) Panic(args ...any) {}

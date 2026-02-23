package daemon

import (
	"context"
	"net"
	"testing"

	"github.com/sagernet/sing/common/control"
	"github.com/sagernet/sing/common/x/list"

	tun "github.com/sagernet/sing-tun"
)

// mockIfaceMonitor implements tun.DefaultInterfaceMonitor for testing.
type mockIfaceMonitor struct {
	iface *control.Interface
}

func (m *mockIfaceMonitor) Start() error                            { return nil }
func (m *mockIfaceMonitor) Close() error                            { return nil }
func (m *mockIfaceMonitor) DefaultInterface() *control.Interface    { return m.iface }
func (m *mockIfaceMonitor) OverrideAndroidVPN() bool                { return false }
func (m *mockIfaceMonitor) AndroidVPNEnabled() bool                 { return false }
func (m *mockIfaceMonitor) RegisterCallback(cb tun.DefaultInterfaceUpdateCallback) *list.Element[tun.DefaultInterfaceUpdateCallback] {
	return nil
}
func (m *mockIfaceMonitor) UnregisterCallback(*list.Element[tun.DefaultInterfaceUpdateCallback]) {}
func (m *mockIfaceMonitor) RegisterMyInterface(string)                                           {}
func (m *mockIfaceMonitor) MyInterface() string                                                  { return "" }

func TestDirectDialer_DynamicInterface(t *testing.T) {
	mon := &mockIfaceMonitor{
		iface: &control.Interface{Name: "en0", Index: 12},
	}
	d := newDirectDialer(mon)

	if got := d.interfaceName(); got != "en0" {
		t.Errorf("interfaceName() = %q, want %q", got, "en0")
	}

	// Simulate network change.
	mon.iface = &control.Interface{Name: "en1", Index: 14}
	if got := d.interfaceName(); got != "en1" {
		t.Errorf("after change: interfaceName() = %q, want %q", got, "en1")
	}
}

func TestDirectDialer_NilInterface(t *testing.T) {
	mon := &mockIfaceMonitor{iface: nil}
	d := newDirectDialer(mon)

	if got := d.interfaceName(); got != "" {
		t.Errorf("interfaceName() = %q, want empty", got)
	}
}

func TestDirectDialer_DialContext(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live network test in short mode")
	}
	// Use real monitor to get a real interface for integration test.
	netMon, ifaceMon, err := createRealMonitor(t)
	if err != nil {
		t.Skip("network monitor unavailable:", err)
	}
	defer netMon.Close()
	defer ifaceMon.Close()

	d := newDirectDialer(ifaceMon)
	if d.interfaceName() == "" {
		t.Skip("no default interface detected")
	}

	// Get interface IP to listen on (can't use loopback with bound sockets).
	iface, err := net.InterfaceByName(d.interfaceName())
	if err != nil {
		t.Skipf("can't get interface %s: %v", d.interfaceName(), err)
	}
	addrs, err := iface.Addrs()
	if err != nil || len(addrs) == 0 {
		t.Skip("no addresses on interface")
	}

	var ifIP string
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && ipnet.IP.To4() != nil {
			ifIP = ipnet.IP.String()
			break
		}
	}
	if ifIP == "" {
		t.Skip("no IPv4 address on interface")
	}

	ln, err := net.Listen("tcp", ifIP+":0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	conn, err := d.DialContext(context.Background(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("DialContext: %v", err)
	}
	conn.Close()
}

func TestDirectDialer_ListenPacket(t *testing.T) {
	netMon, ifaceMon, err := createRealMonitor(t)
	if err != nil {
		t.Skip("network monitor unavailable:", err)
	}
	defer netMon.Close()
	defer ifaceMon.Close()

	d := newDirectDialer(ifaceMon)
	if d.interfaceName() == "" {
		t.Skip("no default interface detected")
	}

	pc, err := d.ListenPacket("udp", ":0")
	if err != nil {
		t.Fatalf("ListenPacket: %v", err)
	}
	pc.Close()
}

// createRealMonitor creates a real sing-tun monitor pair for integration tests.
func createRealMonitor(t *testing.T) (tun.NetworkUpdateMonitor, tun.DefaultInterfaceMonitor, error) {
	t.Helper()
	netMon, err := tun.NewNetworkUpdateMonitor(nil)
	if err != nil {
		return nil, nil, err
	}
	ifaceMon, err := tun.NewDefaultInterfaceMonitor(netMon, nil, tun.DefaultInterfaceMonitorOptions{
		InterfaceFinder: control.NewDefaultInterfaceFinder(),
	})
	if err != nil {
		netMon.Close()
		return nil, nil, err
	}
	if err := netMon.Start(); err != nil {
		ifaceMon.Close()
		netMon.Close()
		return nil, nil, err
	}
	if err := ifaceMon.Start(); err != nil {
		netMon.Close()
		return nil, nil, err
	}
	return netMon, ifaceMon, nil
}

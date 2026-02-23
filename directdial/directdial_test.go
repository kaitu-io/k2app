package directdial

import (
	"context"
	"net"
	"testing"

	"github.com/sagernet/sing/common/control"
	"github.com/sagernet/sing/common/x/list"

	tun "github.com/sagernet/sing-tun"
)

// mockMonitor implements tun.DefaultInterfaceMonitor for testing.
type mockMonitor struct {
	iface *control.Interface
}

func (m *mockMonitor) Start() error                            { return nil }
func (m *mockMonitor) Close() error                            { return nil }
func (m *mockMonitor) DefaultInterface() *control.Interface    { return m.iface }
func (m *mockMonitor) OverrideAndroidVPN() bool                { return false }
func (m *mockMonitor) AndroidVPNEnabled() bool                 { return false }
func (m *mockMonitor) RegisterCallback(cb tun.DefaultInterfaceUpdateCallback) *list.Element[tun.DefaultInterfaceUpdateCallback] {
	return nil
}
func (m *mockMonitor) UnregisterCallback(*list.Element[tun.DefaultInterfaceUpdateCallback]) {}
func (m *mockMonitor) RegisterMyInterface(string)                                           {}
func (m *mockMonitor) MyInterface() string                                                  { return "" }

func TestNew_DynamicInterface(t *testing.T) {
	mon := &mockMonitor{
		iface: &control.Interface{Name: "en0", Index: 12},
	}
	d := New(mon)

	if got := d.InterfaceName(); got != "en0" {
		t.Errorf("InterfaceName() = %q, want %q", got, "en0")
	}

	// Simulate network change.
	mon.iface = &control.Interface{Name: "en1", Index: 14}
	if got := d.InterfaceName(); got != "en1" {
		t.Errorf("after change: InterfaceName() = %q, want %q", got, "en1")
	}
}

func TestNew_NilInterface(t *testing.T) {
	mon := &mockMonitor{iface: nil}
	d := New(mon)

	if got := d.InterfaceName(); got != "" {
		t.Errorf("InterfaceName() = %q, want empty", got)
	}
}

func TestDialContext(t *testing.T) {
	// Use real monitor to get a real interface for integration test.
	netMon, ifaceMon, err := createRealMonitor(t)
	if err != nil {
		t.Skip("network monitor unavailable:", err)
	}
	defer netMon.Close()
	defer ifaceMon.Close()

	d := New(ifaceMon)
	if d.InterfaceName() == "" {
		t.Skip("no default interface detected")
	}

	// Get interface IP to listen on (can't use loopback with bound sockets).
	iface, err := net.InterfaceByName(d.InterfaceName())
	if err != nil {
		t.Skipf("can't get interface %s: %v", d.InterfaceName(), err)
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

func TestListenPacket(t *testing.T) {
	netMon, ifaceMon, err := createRealMonitor(t)
	if err != nil {
		t.Skip("network monitor unavailable:", err)
	}
	defer netMon.Close()
	defer ifaceMon.Close()

	d := New(ifaceMon)
	if d.InterfaceName() == "" {
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

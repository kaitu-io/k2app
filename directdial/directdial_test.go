package directdial

import (
	"context"
	"net"
	"testing"
)

func TestNew(t *testing.T) {
	d := New()
	// Should detect at least some interface on any machine.
	if d.ifName == "" {
		t.Skip("no physical interface detected (expected in some CI environments)")
	}
	t.Logf("detected interface: %s (index=%d)", d.ifName, d.ifIdx)
}

func TestNewWithInterface_Invalid(t *testing.T) {
	_, err := NewWithInterface("nonexistent_iface_xyz")
	if err == nil {
		t.Error("NewWithInterface with invalid name should return error")
	}
}

func TestDialContext(t *testing.T) {
	d := New()
	if d.ifName == "" {
		t.Skip("no physical interface detected")
	}

	// Get the IP address of the physical interface (not loopback).
	// Can't use 127.0.0.1 because bound-to-interface sockets can't reach loopback.
	iface, err := net.InterfaceByName(d.ifName)
	if err != nil {
		t.Skipf("can't get interface %s: %v", d.ifName, err)
	}
	addrs, err := iface.Addrs()
	if err != nil || len(addrs) == 0 {
		t.Skip("no addresses on interface")
	}

	// Find first IPv4 address on the interface.
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
	d := New()
	if d.ifName == "" {
		t.Skip("no physical interface detected")
	}

	pc, err := d.ListenPacket("udp", ":0")
	if err != nil {
		t.Fatalf("ListenPacket: %v", err)
	}
	pc.Close()
}

func TestDetectDefault(t *testing.T) {
	name, idx := detectDefault()
	if name == "" {
		t.Skip("no default interface detected")
	}
	if idx <= 0 {
		t.Errorf("interface %q has invalid index %d", name, idx)
	}
	t.Logf("default interface: %s (index=%d)", name, idx)
}

func TestIsUsableInterface(t *testing.T) {
	// Loopback should not be usable.
	lo, err := net.InterfaceByName("lo0")
	if err != nil {
		lo, err = net.InterfaceByName("lo")
	}
	if err == nil {
		if isUsableInterface(lo) {
			t.Error("loopback interface should not be usable")
		}
	}
}

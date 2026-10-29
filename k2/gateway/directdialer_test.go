//go:build linux

package gateway

import (
	"context"
	"net"
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

// gatewayDirectDialer stamps every direct-network socket with SO_MARK so the
// OUTPUT-chain DNS REDIRECT rule can RETURN k2r's own DNS queries before they
// loop back through k2r's DNS listener. Without this exemption, k2r's
// DirectDNS resolution to e.g. 223.5.5.5:53 would itself be REDIRECTed →
// dnsHandler → DirectDNS → REDIRECT → infinite recursion.
//
// Verifies via getsockopt readback after creating a UDP socket through the
// dialer's ControlFunc. SO_MARK get is allowed for any user (unlike set),
// but the set in ControlFunc requires CAP_NET_ADMIN — skip if not root.
func TestGatewayDirectDialer_SetsSOMark(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("SO_MARK set requires CAP_NET_ADMIN (run as root for UAT)")
	}

	d := &gatewayDirectDialer{}
	nd := &net.Dialer{Control: d.ControlFunc()}

	// Dial to a closed local port so we get a UDP socket without sending data.
	conn, err := nd.DialContext(context.Background(), "udp", "127.0.0.1:1")
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	udpConn := conn.(*net.UDPConn)
	rawConn, err := udpConn.SyscallConn()
	if err != nil {
		t.Fatalf("SyscallConn: %v", err)
	}

	var mark int
	var optErr error
	if err := rawConn.Control(func(fd uintptr) {
		mark, optErr = unix.GetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_MARK)
	}); err != nil {
		t.Fatalf("rawConn.Control: %v", err)
	}
	if optErr != nil {
		t.Fatalf("GetsockoptInt SO_MARK: %v", optErr)
	}
	if mark != K2RFwmark {
		t.Errorf("SO_MARK = 0x%x, want 0x%x", mark, K2RFwmark)
	}
}

// DialContext + ListenPacket must both apply the same Control func, so any
// socket k2r opens via the direct path is marked — not just connected UDP.
// We do not require root for this test: we just confirm that the Control
// callback is invoked. Skip if non-root since setsockopt will fail.
func TestGatewayDirectDialer_AppliesControlOnListenPacket(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("SO_MARK set requires CAP_NET_ADMIN (run as root for UAT)")
	}

	d := &gatewayDirectDialer{}
	pconn, err := d.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenPacket: %v", err)
	}
	defer pconn.Close()

	udpConn := pconn.(*net.UDPConn)
	rawConn, err := udpConn.SyscallConn()
	if err != nil {
		t.Fatalf("SyscallConn: %v", err)
	}

	var mark int
	var optErr error
	rawConn.Control(func(fd uintptr) {
		mark, optErr = unix.GetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_MARK)
	})
	if optErr != nil {
		t.Fatalf("GetsockoptInt: %v", optErr)
	}
	if mark != K2RFwmark {
		t.Errorf("ListenPacket SO_MARK = 0x%x, want 0x%x", mark, K2RFwmark)
	}
}

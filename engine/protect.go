package engine

import (
	"context"
	"fmt"
	"net"
	"syscall"
)

// protectedDialer implements wire.DirectDialer by calling ProtectFunc
// on every socket before connection. Used on mobile (Android) to mark
// sockets for VPN routing exclusion via VpnService.protect(fd).
type protectedDialer struct {
	protect func(fd int) bool
}

// DialContext dials with socket protection applied before connect.
func (d *protectedDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	nd := &net.Dialer{
		Control: d.controlFunc(),
	}
	return nd.DialContext(ctx, network, addr)
}

// ListenPacket creates a protected UDP socket.
func (d *protectedDialer) ListenPacket(network, addr string) (net.PacketConn, error) {
	lc := &net.ListenConfig{
		Control: d.controlFunc(),
	}
	return lc.ListenPacket(context.Background(), network, addr)
}

// controlFunc returns a syscall.RawConn control function that calls protect
// on the socket fd before the OS connect() call.
func (d *protectedDialer) controlFunc() func(string, string, syscall.RawConn) error {
	return func(network, address string, c syscall.RawConn) error {
		var err error
		c.Control(func(fd uintptr) {
			if !d.protect(int(fd)) {
				err = fmt.Errorf("socket protect failed for fd %d", fd)
			}
		})
		return err
	}
}

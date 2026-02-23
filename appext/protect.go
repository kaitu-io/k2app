package appext

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"syscall"
	"time"
)

// protectedDialer implements wire.DirectDialer and wire.DialerController
// by calling protect on every socket before connection. Used on Android to
// mark sockets for VPN routing exclusion via VpnService.protect(fd).
type protectedDialer struct {
	protect func(fd int) bool
}

// DialContext dials with socket protection applied before connect.
func (d *protectedDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	ctxErr := ctx.Err()
	deadline, hasDeadline := ctx.Deadline()
	remaining := ""
	if hasDeadline {
		remaining = fmt.Sprintf("%.1fs", time.Until(deadline).Seconds())
	}
	slog.Debug("protectedDialer: DialContext", "network", network, "addr", addr,
		"ctxErr", ctxErr, "hasDeadline", hasDeadline, "remaining", remaining)
	nd := &net.Dialer{
		Control: d.ControlFunc(),
	}
	conn, err := nd.DialContext(ctx, network, addr)
	if err != nil {
		slog.Debug("protectedDialer: DialContext failed", "addr", addr, "err", err, "ctxErr", ctx.Err())
	} else {
		slog.Debug("protectedDialer: DialContext success", "addr", addr, "local", conn.LocalAddr())
	}
	return conn, err
}

// ListenPacket creates a protected UDP socket.
func (d *protectedDialer) ListenPacket(network, addr string) (net.PacketConn, error) {
	lc := &net.ListenConfig{
		Control: d.ControlFunc(),
	}
	return lc.ListenPacket(context.Background(), network, addr)
}

// ControlFunc returns a syscall.RawConn control function that calls protect
// on the socket fd before the OS connect() call.
// Implements wire.DialerController.
func (d *protectedDialer) ControlFunc() func(string, string, syscall.RawConn) error {
	return func(network, address string, c syscall.RawConn) error {
		var err error
		c.Control(func(fd uintptr) {
			ok := d.protect(int(fd))
			slog.Debug("protectedDialer: protect()", "fd", fd, "ok", ok, "network", network, "address", address)
			if !ok {
				err = fmt.Errorf("socket protect failed for fd %d", fd)
			}
		})
		return err
	}
}

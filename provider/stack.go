package provider

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"syscall"
	"time"

	"github.com/sagernet/sing/common/buf"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"

	"github.com/sagernet/sing-tun"

	"github.com/kaitu-io/k2/safego"
)

// handlerAdapter wraps ConnectionHandler to implement sing-tun's Handler interface.
type handlerAdapter struct {
	handler ConnectionHandler
}

var _ tun.Handler = (*handlerAdapter)(nil)

func (h *handlerAdapter) PrepareConnection(network string, source M.Socksaddr, destination M.Socksaddr) error {
	return nil
}

func (h *handlerAdapter) NewConnectionEx(ctx context.Context, conn net.Conn, source M.Socksaddr, destination M.Socksaddr, onClose N.CloseHandlerFunc) {
	dest := destination.String()
	safego.Go(func() {
		defer func() {
			if onClose != nil {
				onClose(nil)
			}
		}()
		h.handler.HandleTCP(conn, dest)
	})
}

func (h *handlerAdapter) NewPacketConnectionEx(ctx context.Context, conn N.PacketConn, source M.Socksaddr, destination M.Socksaddr, onClose N.CloseHandlerFunc) {
	dest := destination.String()
	safego.Go(func() {
		defer func() {
			if onClose != nil {
				onClose(nil)
			}
		}()
		h.handler.HandleUDP(&singPacketConn{conn: conn}, dest)
	})
}

// singPacketConn wraps sing's N.PacketConn into standard net.PacketConn.
type singPacketConn struct {
	conn N.PacketConn
}

var _ net.PacketConn = (*singPacketConn)(nil)

func (c *singPacketConn) ReadFrom(b []byte) (int, net.Addr, error) {
	buffer := buf.NewSize(len(b))
	defer buffer.Release()
	dest, err := c.conn.ReadPacket(buffer)
	if err != nil {
		return 0, nil, err
	}
	n := copy(b, buffer.Bytes())
	return n, dest.UDPAddr(), nil
}

func (c *singPacketConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	var dest M.Socksaddr
	if addr != nil {
		dest = M.SocksaddrFromNet(addr)
	}
	buffer := buf.NewSize(len(b))
	buffer.Write(b)
	if err := c.conn.WritePacket(buffer, dest); err != nil {
		return 0, err
	}
	return len(b), nil
}

func (c *singPacketConn) Close() error                       { return c.conn.Close() }
func (c *singPacketConn) LocalAddr() net.Addr                { return c.conn.LocalAddr() }
func (c *singPacketConn) SetDeadline(t time.Time) error      { return c.conn.SetDeadline(t) }
func (c *singPacketConn) SetReadDeadline(t time.Time) error  { return c.conn.SetReadDeadline(t) }
func (c *singPacketConn) SetWriteDeadline(t time.Time) error { return c.conn.SetWriteDeadline(t) }

// RejectHandshake sends ICMP Port Unreachable via gVisor's UDPBackWriter.
// This makes Chrome immediately fallback from QUIC to TCP.
func (c *singPacketConn) RejectHandshake() error {
	return N.ReportHandshakeFailure(c.conn, syscall.ECONNREFUSED)
}

// slogTunLogger adapts Go's slog to sing's logger.Logger interface.
type slogTunLogger struct{}

func (l *slogTunLogger) Trace(args ...any) { slog.Debug("[tun] " + fmt.Sprint(args...)) }
func (l *slogTunLogger) Debug(args ...any) { slog.Debug("[tun] " + fmt.Sprint(args...)) }
func (l *slogTunLogger) Info(args ...any)  { slog.Info("[tun] " + fmt.Sprint(args...)) }
func (l *slogTunLogger) Warn(args ...any)  { slog.Warn("[tun] " + fmt.Sprint(args...)) }
func (l *slogTunLogger) Error(args ...any) { slog.Error("[tun] " + fmt.Sprint(args...)) }
func (l *slogTunLogger) Fatal(args ...any) { slog.Error("[tun] FATAL: " + fmt.Sprint(args...)) }
func (l *slogTunLogger) Panic(args ...any) { slog.Error("[tun] PANIC: " + fmt.Sprint(args...)) }

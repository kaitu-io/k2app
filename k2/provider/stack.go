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
	// Wrap conn to preserve source addr for process lookup.
	// gVisor conn's RemoteAddr may not return the original source;
	// the source param from sing-tun is authoritative.
	wrapped := &sourceConn{Conn: conn, source: source.UDPAddr()}
	safego.Go(func() {
		defer func() {
			if onClose != nil {
				onClose(nil)
			}
		}()
		h.handler.HandleTCP(wrapped, dest)
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
		h.handler.HandleUDP(&singPacketConn{conn: conn, source: source.UDPAddr()}, dest)
	})
}

// sourceConn wraps a net.Conn to preserve the original source address.
// gVisor connections may not return the correct source via RemoteAddr();
// the source from sing-tun's handler callback is authoritative.
type sourceConn struct {
	net.Conn
	source *net.UDPAddr
}

func (c *sourceConn) RemoteAddr() net.Addr {
	if c.source != nil {
		return c.source
	}
	return c.Conn.RemoteAddr()
}

// SourceAddr extracts the original source address from a connection.
// Returns nil if the connection is not a sourceConn wrapper.
func SourceAddr(conn net.Conn) *net.UDPAddr {
	if sc, ok := conn.(*sourceConn); ok {
		return sc.source
	}
	return nil
}

// PacketSourcer is implemented by net.PacketConn wrappers that carry a
// single flow's LAN source address baked in at construction (sing-tun's
// singPacketConn, and gateway's TPROXY/DNS single-shot conns). Letting
// gateway's own conn types satisfy this via a same-named method — rather
// than requiring them to embed a provider type — keeps the two packages
// decoupled while still feeding ConnMeta.SrcIP for multi-slot routing.
type PacketSourcer interface {
	PacketSourceAddr() *net.UDPAddr
}

// SourcePacketAddr extracts the original source address from a PacketConn.
// Returns nil if the PacketConn doesn't implement PacketSourcer.
func SourcePacketAddr(conn net.PacketConn) *net.UDPAddr {
	if ps, ok := conn.(PacketSourcer); ok {
		return ps.PacketSourceAddr()
	}
	return nil
}

// singPacketConn wraps sing's N.PacketConn into standard net.PacketConn.
type singPacketConn struct {
	conn   N.PacketConn
	source *net.UDPAddr // original source address for process lookup
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
	// Unwrap() strips ::ffff: so an IPv4-mapped reply address reads as IPv4.
	// The wire outbounds build it from net.ResolveUDPAddr("udp4", ...), which
	// returns a 16-byte mapped net.IP; left mapped, Socksaddr.IsIPv6() is true
	// and gVisor's UDPBackWriter rejects every downlink packet with "send IPv6
	// packet to IPv4 connection" (iOS/Android only — desktop uses the System
	// stack). This is the one gate all downlink UDP passes through, so
	// normalising here covers every outbound. See ticket #3379 and the repro in
	// stack_gvisor_udp_repro_test.go.
	var dest M.Socksaddr
	if addr != nil {
		dest = M.SocksaddrFromNet(addr).Unwrap()
	}
	buffer := buf.NewSize(len(b))
	buffer.Write(b)
	if err := c.conn.WritePacket(buffer, dest); err != nil {
		return 0, err
	}
	return len(b), nil
}

// PacketSourceAddr implements PacketSourcer.
func (c *singPacketConn) PacketSourceAddr() *net.UDPAddr { return c.source }

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

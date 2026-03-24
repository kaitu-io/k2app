//go:build linux

package gateway

import (
	"context"
	"fmt"
	"net"
	"syscall"
	"time"
)

func newTPROXYTCPListener(ctx context.Context, port int) (net.Listener, error) {
	lc := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			return c.Control(func(fd uintptr) {
				syscall.SetsockoptInt(int(fd), syscall.SOL_IP, syscall.IP_TRANSPARENT, 1)
			})
		},
	}
	return lc.Listen(ctx, "tcp", fmt.Sprintf("0.0.0.0:%d", port))
}

func newTPROXYUDPListener(ctx context.Context, port int) (*net.UDPConn, error) {
	lc := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			return c.Control(func(fd uintptr) {
				syscall.SetsockoptInt(int(fd), syscall.SOL_IP, syscall.IP_TRANSPARENT, 1)
				syscall.SetsockoptInt(int(fd), syscall.SOL_IP, syscall.IP_RECVORIGDSTADDR, 1)
			})
		},
	}
	conn, err := lc.ListenPacket(ctx, "udp", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		return nil, err
	}
	return conn.(*net.UDPConn), nil
}

// readUDPOrigDst reads a UDP packet and extracts the original destination
// from the IP_RECVORIGDSTADDR ancillary message.
func readUDPOrigDst(conn *net.UDPConn, buf []byte) (n int, src, dst *net.UDPAddr, err error) {
	oob := make([]byte, 128)
	n, oobn, _, srcAddr, err := conn.ReadMsgUDP(buf, oob)
	if err != nil {
		return 0, nil, nil, err
	}
	msgs, err := syscall.ParseSocketControlMessage(oob[:oobn])
	if err != nil {
		return 0, nil, nil, fmt.Errorf("parse control message: %w", err)
	}
	for _, msg := range msgs {
		if msg.Header.Level == syscall.SOL_IP && msg.Header.Type == syscall.IP_RECVORIGDSTADDR {
			if len(msg.Data) >= 8 {
				port := int(msg.Data[2])<<8 | int(msg.Data[3])
				ip := net.IPv4(msg.Data[4], msg.Data[5], msg.Data[6], msg.Data[7])
				return n, srcAddr, &net.UDPAddr{IP: ip, Port: port}, nil
			}
		}
		// IPv6: SOL_IPV6 / IPV6_RECVORIGDSTADDR (74 on Linux)
		if msg.Header.Level == syscall.SOL_IPV6 && msg.Header.Type == 74 {
			if len(msg.Data) >= 28 {
				port := int(msg.Data[2])<<8 | int(msg.Data[3])
				ip := make(net.IP, 16)
				copy(ip, msg.Data[8:24])
				return n, srcAddr, &net.UDPAddr{IP: ip, Port: port}, nil
			}
		}
	}
	return n, srcAddr, srcAddr, nil
}

// tproxyUDPConn wraps a single UDP packet as a net.PacketConn.
type tproxyUDPConn struct {
	conn    *net.UDPConn
	src     *net.UDPAddr
	dst     *net.UDPAddr
	initial []byte
	read    bool
	done    chan struct{}
}

func newTProxyUDPConn(conn *net.UDPConn, src, dst *net.UDPAddr, initial []byte) *tproxyUDPConn {
	data := make([]byte, len(initial))
	copy(data, initial)
	return &tproxyUDPConn{
		conn: conn, src: src, dst: dst,
		initial: data, done: make(chan struct{}),
	}
}

func (c *tproxyUDPConn) ReadFrom(b []byte) (int, net.Addr, error) {
	if !c.read {
		c.read = true
		n := copy(b, c.initial)
		return n, c.src, nil
	}
	<-c.done
	return 0, nil, net.ErrClosed
}

// WriteTo sends the response back to the original LAN source address.
// The addr parameter is intentionally ignored — TPROXY wraps each UDP packet
// as a single-session conn, so all responses go to the originating LAN device.
func (c *tproxyUDPConn) WriteTo(b []byte, _ net.Addr) (int, error) {
	return c.conn.WriteTo(b, c.src)
}

func (c *tproxyUDPConn) Close() error {
	select {
	case <-c.done:
	default:
		close(c.done)
	}
	return nil
}

func (c *tproxyUDPConn) LocalAddr() net.Addr               { return c.dst }
func (c *tproxyUDPConn) SetDeadline(t time.Time) error      { return nil }
func (c *tproxyUDPConn) SetReadDeadline(t time.Time) error   { return nil }
func (c *tproxyUDPConn) SetWriteDeadline(t time.Time) error  { return nil }

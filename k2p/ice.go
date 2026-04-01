package k2p

import (
	"net"
	"time"

	"github.com/pion/ice/v4"
)

// icePacketConn adapts *ice.Conn (net.Conn) to net.PacketConn for QUIC transport.
// ice.Conn is backed by UDP — Read() preserves datagram boundaries.
// WriteTo ignores addr, ReadFrom returns synthetic peer addr.
type icePacketConn struct {
	conn     *ice.Conn
	peerAddr net.Addr
}

func newICEPacketConn(conn *ice.Conn) *icePacketConn {
	return &icePacketConn{
		conn:     conn,
		peerAddr: conn.RemoteAddr(),
	}
}

func (c *icePacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	n, err := c.conn.Read(p)
	return n, c.peerAddr, err
}

func (c *icePacketConn) WriteTo(p []byte, _ net.Addr) (int, error) {
	return c.conn.Write(p)
}

func (c *icePacketConn) Close() error                       { return c.conn.Close() }
func (c *icePacketConn) LocalAddr() net.Addr                { return c.conn.LocalAddr() }
func (c *icePacketConn) SetDeadline(t time.Time) error      { return c.conn.SetDeadline(t) }
func (c *icePacketConn) SetReadDeadline(t time.Time) error  { return c.conn.SetReadDeadline(t) }
func (c *icePacketConn) SetWriteDeadline(t time.Time) error { return c.conn.SetWriteDeadline(t) }

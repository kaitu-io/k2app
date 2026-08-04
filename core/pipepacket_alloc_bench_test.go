package core

import (
	"net"
	"sync/atomic"
	"testing"
	"time"
)

// benchPacketConn is a net.PacketConn that allocates nothing of its own, so an
// allocation benchmark over pipePacket measures pipePacket's per-packet cost
// and not the harness's. ReadFrom yields `reads` packets and then reports
// net.ErrClosed, which is what lets pipePacket unwind on its own: the RX loop
// (remote, constructed with 0 reads) exits immediately and closes local, the
// TX loop drains its budget and closes remote, and pipePacket returns.
//
// addr is a testAddr (a string type) rather than a *net.UDPAddr on purpose:
// *net.UDPAddr.String() allocates, and sameUDPAddr calls it on every packet,
// which would swamp the 16-byte allocation this benchmark exists to catch.
// BenchmarkPipePacketUplinkUDPAddr below measures that case separately.
type benchPacketConn struct {
	addr  net.Addr
	reads atomic.Int64
}

func newBenchPacketConn(addr net.Addr, reads int64) *benchPacketConn {
	c := &benchPacketConn{addr: addr}
	c.reads.Store(reads)
	return c
}

func (c *benchPacketConn) ReadFrom(b []byte) (int, net.Addr, error) {
	if c.reads.Add(-1) < 0 {
		return 0, nil, net.ErrClosed
	}
	return 1, c.addr, nil
}

func (c *benchPacketConn) WriteTo(b []byte, addr net.Addr) (int, error) { return len(b), nil }

func (c *benchPacketConn) Close() error                     { return nil }
func (c *benchPacketConn) LocalAddr() net.Addr              { return &net.UDPAddr{} }
func (c *benchPacketConn) SetDeadline(time.Time) error      { return nil }
func (c *benchPacketConn) SetReadDeadline(time.Time) error  { return nil }
func (c *benchPacketConn) SetWriteDeadline(time.Time) error { return nil }

// BenchmarkPipePacketUplink measures pipePacket's per-packet uplink cost. This
// branch exists entirely to make the UDP hot path correct for games, so a
// per-packet heap allocation here is a regression worth a standing benchmark:
// the session's primary destination is written once, so it must cost one
// allocation per SESSION, not one per packet.
func BenchmarkPipePacketUplink(b *testing.B) {
	local := newBenchPacketConn(testAddr("1.1.1.1:443"), int64(b.N))
	remote := newBenchPacketConn(nil, 0)

	b.ReportAllocs()
	b.ResetTimer()
	pipePacket(local, remote, nil, nil, nil)
}

// BenchmarkPipePacketUplinkUDPAddr is the same loop with the concrete address
// type the TUN stack actually hands us. It is NOT zero-alloc, and not because
// of the primary handoff: sameUDPAddr compares by String(), and
// *net.UDPAddr.String() formats from scratch on every call. Kept as a measured
// record of that separate, pre-existing cost so the zero-alloc result above is
// not misread as "the uplink path allocates nothing in production".
func BenchmarkPipePacketUplinkUDPAddr(b *testing.B) {
	local := newBenchPacketConn(&net.UDPAddr{IP: net.IPv4(1, 1, 1, 1), Port: 443}, int64(b.N))
	remote := newBenchPacketConn(nil, 0)

	b.ReportAllocs()
	b.ResetTimer()
	pipePacket(local, remote, nil, nil, nil)
}

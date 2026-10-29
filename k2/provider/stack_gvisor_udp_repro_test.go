//go:build with_gvisor

package provider

// Repro for support ticket #3379 / #3381 (iOS "上不了 YouTube").
//
// Field evidence: across 2821 UDP relay sessions on iOS (3 users, k2 0.4.3 and
// 0.4.7, AU/US/KR/SG/JP nodes), NOT ONE session ever received more than a single
// downlink packet — every `quicUDPConn: closed` line reports rxPackets<=1. The
// same k2 version on Windows desktop, against the same node, reports rxPackets
// 6..21. iOS/Android build with `-tags with_gvisor` and IncludeAllNetworks=true
// (pure gVisor stack); the desktop binary builds without the tag and lands on
// the System stack. The suspect is therefore the gVisor-only write-back path:
//
//	pipePacket RX loop (core/tunnel.go:757)  local.WriteTo(buf[:n], from)
//	  -> singPacketConn.WriteTo              (provider/stack.go:121)
//	    -> udpnat.natConn.WritePacket
//	      -> tun.UDPBackWriter.WritePacket   <- gVisor only, iOS/Android only
//
// `from` comes from wire/quic.go quicUDPConn.ReadFrom:
//
//	addr, _ := net.ResolveUDPAddr("udp4", c.targetAddr)
//
// which yields a 16-byte IPv4-MAPPED net.IP. sing's AddrFromIP uses
// netip.AddrFromSlice without Unmap(), so Socksaddr.Addr is ::ffff:a.b.c.d, and
// Socksaddr.IsIPv6() (== Addr.Is6(), no Is4In6 exclusion) reports true. The
// first branch of UDPBackWriter.WritePacket then rejects every single downlink
// packet with "send IPv6 packet to IPv4 connection".
//
// These tests drive that exact path with a gVisor channel endpoint — no TUN
// device, no root, no phone — as an A/B on the reply-address shape.

import (
	"context"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/sagernet/gvisor/pkg/buffer"
	"github.com/sagernet/gvisor/pkg/tcpip"
	"github.com/sagernet/gvisor/pkg/tcpip/checksum"
	"github.com/sagernet/gvisor/pkg/tcpip/header"
	"github.com/sagernet/gvisor/pkg/tcpip/link/channel"
	"github.com/sagernet/gvisor/pkg/tcpip/stack"
	"github.com/sagernet/gvisor/pkg/tcpip/transport/udp"
	tun "github.com/sagernet/sing-tun"
)

var (
	reproClientAddr = netip.MustParseAddr("172.19.0.2")
	reproServerAddr = netip.MustParseAddr("8.8.8.8")
)

const (
	reproClientPort = 51820
	reproServerPort = 443
	downlinkCount   = 5
)

// captureHandler hands the PacketConn for the first UDP session to the test and
// then parks, keeping the session (and its NAT entry) alive.
type captureHandler struct {
	conns chan net.PacketConn
	park  chan struct{}
}

func (h *captureHandler) HandleTCP(conn net.Conn, dest string) { conn.Close() }

func (h *captureHandler) HandleUDP(conn net.PacketConn, dest string) {
	select {
	case h.conns <- conn:
	default:
		conn.Close()
		return
	}
	<-h.park
	conn.Close()
}

// newGVisorUDPSession builds the iOS stack (pure gVisor, as tun_ios.go does via
// IncludeAllNetworks=true), pushes one uplink packet through it, and returns the
// resulting singPacketConn plus the link endpoint the downlink must reach.
func newGVisorUDPSession(t *testing.T) (net.PacketConn, *channel.Endpoint, func()) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	ep := channel.New(1024, 1500, "")
	ipStack, err := tun.NewGVisorStack(ep)
	if err != nil {
		cancel()
		t.Fatalf("NewGVisorStack: %v", err)
	}

	h := &captureHandler{conns: make(chan net.PacketConn, 1), park: make(chan struct{})}
	forwarder := tun.NewUDPForwarder(ctx, ipStack, &handlerAdapter{handler: h}, 5*time.Minute)
	ipStack.SetTransportProtocolHandler(udp.ProtocolNumber, forwarder.HandlePacket)

	injectUplink(ep, []byte("uplink-quic-initial"))

	var conn net.PacketConn
	select {
	case conn = <-h.conns:
	case <-time.After(5 * time.Second):
		close(h.park)
		ipStack.Close()
		cancel()
		t.Fatal("no UDP session created — uplink never reached the handler")
	}

	// Drain the uplink packet (what pipePacket's TX goroutine does).
	buf := make([]byte, 2048)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := conn.ReadFrom(buf); err != nil {
		close(h.park)
		ipStack.Close()
		cancel()
		t.Fatalf("uplink ReadFrom: %v", err)
	}
	conn.SetReadDeadline(time.Time{})

	for ep.NumQueued() > 0 {
		if p := ep.Read(); p != nil {
			p.DecRef()
		}
	}

	return conn, ep, func() {
		close(h.park)
		ipStack.Close()
		cancel()
	}
}

// injectUplink pushes one IPv4/UDP packet into the stack, as if the phone's app
// had just sent it into the TUN.
func injectUplink(ep *channel.Endpoint, payload []byte) {
	udpSize := header.UDPMinimumSize + len(payload)
	totalSize := header.IPv4MinimumSize + udpSize
	raw := make([]byte, totalSize)

	ip := header.IPv4(raw)
	ip.Encode(&header.IPv4Fields{
		TotalLength: uint16(totalSize),
		TTL:         64,
		Protocol:    uint8(header.UDPProtocolNumber),
		SrcAddr:     tcpip.AddrFrom4(reproClientAddr.As4()),
		DstAddr:     tcpip.AddrFrom4(reproServerAddr.As4()),
	})
	ip.SetChecksum(0)
	ip.SetChecksum(^ip.CalculateChecksum())

	u := header.UDP(raw[header.IPv4MinimumSize:])
	u.Encode(&header.UDPFields{
		SrcPort: reproClientPort,
		DstPort: reproServerPort,
		Length:  uint16(udpSize),
	})
	copy(raw[header.IPv4MinimumSize+header.UDPMinimumSize:], payload)
	xsum := header.PseudoHeaderChecksum(
		header.UDPProtocolNumber,
		tcpip.AddrFrom4(reproClientAddr.As4()),
		tcpip.AddrFrom4(reproServerAddr.As4()),
		uint16(udpSize),
	)
	xsum = checksum.Combine(xsum, checksum.Checksum(raw[header.IPv4MinimumSize:], 0))
	u.SetChecksum(^xsum)

	pkt := stack.NewPacketBuffer(stack.PacketBufferOptions{
		Payload: buffer.MakeWithData(raw),
	})
	defer pkt.DecRef()
	ep.InjectInbound(header.IPv4ProtocolNumber, pkt)
}

func drainEndpoint(ep *channel.Endpoint, want int, timeout time.Duration) int {
	got := 0
	deadline := time.Now().Add(timeout)
	for got < want && time.Now().Before(deadline) {
		p := ep.Read()
		if p == nil {
			time.Sleep(5 * time.Millisecond)
			continue
		}
		got++
		p.DecRef()
	}
	return got
}

// resolveLikeProduction mirrors wire/quic.go quicUDPConn.ReadFrom, which builds
// the reply address with net.ResolveUDPAddr("udp4", c.targetAddr).
func resolveLikeProduction(t *testing.T, hostPort string) *net.UDPAddr {
	t.Helper()
	addr, err := net.ResolveUDPAddr("udp4", hostPort)
	if err != nil {
		t.Fatalf("ResolveUDPAddr: %v", err)
	}
	t.Logf("production reply addr: ip=%v len(ip)=%d is4=%v is4in6=%v",
		addr.IP, len(addr.IP), addr.AddrPort().Addr().Is4(), addr.AddrPort().Addr().Is4In6())
	return addr
}

// TestGVisorUDPDownlinkAcceptsUnmappedAddr is the CONTROL arm: a 4-byte reply
// address on the very same stack and session.
func TestGVisorUDPDownlinkAcceptsUnmappedAddr(t *testing.T) {
	conn, ep, cleanup := newGVisorUDPSession(t)
	defer cleanup()

	from := &net.UDPAddr{IP: reproServerAddr.AsSlice(), Port: reproServerPort} // 4-byte
	for i := 0; i < downlinkCount; i++ {
		if _, err := conn.WriteTo([]byte("downlink-payload"), from); err != nil {
			t.Fatalf("control arm: 4-byte reply addr rejected on packet %d: %v", i+1, err)
		}
	}
	if got := drainEndpoint(ep, downlinkCount, 2*time.Second); got != downlinkCount {
		t.Fatalf("control arm: only %d/%d packets reached the link endpoint", got, downlinkCount)
	}
}

// TestGVisorUDPDownlinkDeliversEveryPacket is the REPRO arm: identical, except
// the reply address is shaped exactly as production shapes it.
func TestGVisorUDPDownlinkDeliversEveryPacket(t *testing.T) {
	conn, ep, cleanup := newGVisorUDPSession(t)
	defer cleanup()

	from := resolveLikeProduction(t, net.JoinHostPort(reproServerAddr.String(), "443"))

	var firstErr error
	ok := 0
	for i := 0; i < downlinkCount; i++ {
		_, err := conn.WriteTo([]byte("downlink-payload"), from)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			t.Logf("downlink packet %d: WriteTo FAILED: %v", i+1, err)
			continue
		}
		ok++
		t.Logf("downlink packet %d: WriteTo ok", i+1)
	}

	delivered := drainEndpoint(ep, downlinkCount, 2*time.Second)
	t.Logf("RESULT: WriteTo succeeded %d/%d, link endpoint delivered %d/%d",
		ok, downlinkCount, delivered, downlinkCount)

	if firstErr != nil {
		t.Errorf("downlink write-back into the TUN failed: %v\n"+
			"pipePacket's RX loop breaks on this error, so the UDP session ends "+
			"after a single downlink packet — matching rxPackets=1 in every one of "+
			"the 2821 iOS sessions on file (ticket #3379).", firstErr)
	}
	if delivered != downlinkCount {
		t.Errorf("only %d/%d downlink packets reached the link endpoint", delivered, downlinkCount)
	}
}

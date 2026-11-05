package engine

import (
	"context"
	"net"
	"net/netip"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/kaitu-io/k2/wire"
)

// trackingDialer wraps the core direct exit's DirectDialer and records the
// time of the last successful direct TCP dial. That timestamp is passive
// evidence that the user's network still reaches the internet without the
// tunnel (netEvidence.LastDirectOK): when the tunnel server fails but direct
// traffic keeps succeeding, the server is at fault, not the network.
//
// Only the core direct outbound (routes whose target is "direct") goes
// through this wrapper. Wire dials to tunnel servers and DNS direct queries
// keep the raw dialer — a wire dial succeeding proves nothing new, and DNS
// direct uses its own net.Dialer built from the raw dialer's ControlFunc.
//
// Only dials to public destinations count: LAN traffic (NAS, router UI,
// casting) routed direct keeps succeeding while the upstream internet is down,
// and counting it would make the failover verdict blame the node instead of
// holding for the network.
//
// A nil inner falls back to the stdlib, exactly like core.NewDirectOutbound(nil).
type trackingDialer struct {
	inner  wire.DirectDialer
	lastOK atomic.Int64 // UnixNano of the last successful public DialContext; 0 = never
	// isPublic decides whether a dialed destination is evidence. Always
	// isPublicDest in production; tests swap it to dial loopback listeners.
	isPublic func(netip.Addr) bool
}

func newTrackingDialer(inner wire.DirectDialer) *trackingDialer {
	return &trackingDialer{inner: inner, isPublic: isPublicDest}
}

// cgnatPrefix is RFC 6598 shared address space (carrier-grade NAT) — on the
// near side of the ISP, so reaching it does not prove internet access.
var cgnatPrefix = netip.MustParsePrefix("100.64.0.0/10")

// isPublicDest reports whether a reachable addr proves internet access: a
// global unicast address (IsGlobalUnicast already excludes loopback,
// link-local, multicast and unspecified) that is not RFC1918 / ULA
// (IsPrivate) or CGNAT. IPv4-mapped IPv6 is judged as IPv4 — the netip
// predicates unmap on their own, but Prefix.Contains does not.
func isPublicDest(a netip.Addr) bool {
	a = a.Unmap()
	return a.IsGlobalUnicast() && !a.IsPrivate() && !cgnatPrefix.Contains(a)
}

// DialContext dials through inner and records success to a public
// destination, judged by the connected conn's RemoteAddr (an unparseable
// address does not count). Failures are not recorded — a refused or
// timed-out destination says nothing either way.
func (d *trackingDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	var c net.Conn
	var err error
	if d.inner != nil {
		c, err = d.inner.DialContext(ctx, network, addr)
	} else {
		c, err = (&net.Dialer{}).DialContext(ctx, network, addr)
	}
	if err == nil && d.countsAsEvidence(c) {
		d.lastOK.Store(time.Now().UnixNano())
	}
	return c, err
}

// ListenPacket passes through. Binding a local UDP socket is not evidence of
// internet reachability, so it is never recorded.
func (d *trackingDialer) ListenPacket(network, addr string) (net.PacketConn, error) {
	if d.inner != nil {
		return d.inner.ListenPacket(network, addr)
	}
	return net.ListenPacket(network, addr)
}

// ControlFunc implements wire.DialerController by passing through to inner
// (Android VpnService.protect, k2r SO_MARK). Returns nil when inner has no
// control func — net.Dialer treats a nil Control as "none", the same as the
// raw dialer.
func (d *trackingDialer) ControlFunc() func(network, address string, c syscall.RawConn) error {
	if dc, ok := d.inner.(wire.DialerController); ok {
		return dc.ControlFunc()
	}
	return nil
}

func (d *trackingDialer) countsAsEvidence(c net.Conn) bool {
	ra := c.RemoteAddr()
	if ra == nil {
		return false
	}
	ap, err := netip.ParseAddrPort(ra.String())
	if err != nil {
		return false
	}
	return d.isPublic(ap.Addr())
}

// LastOK returns the time of the last successful direct dial (zero = never).
// Lock-free; safe from any goroutine.
func (d *trackingDialer) LastOK() time.Time {
	n := d.lastOK.Load()
	if n == 0 {
		return time.Time{}
	}
	return time.Unix(0, n)
}

package engine

import (
	"context"
	"net"
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
// A nil inner falls back to the stdlib, exactly like core.NewDirectOutbound(nil).
type trackingDialer struct {
	inner  wire.DirectDialer
	lastOK atomic.Int64 // UnixNano of the last successful DialContext; 0 = never
}

func newTrackingDialer(inner wire.DirectDialer) *trackingDialer {
	return &trackingDialer{inner: inner}
}

// DialContext dials through inner and records success. Failures are not
// recorded — a refused or timed-out destination says nothing either way.
func (d *trackingDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	var c net.Conn
	var err error
	if d.inner != nil {
		c, err = d.inner.DialContext(ctx, network, addr)
	} else {
		c, err = (&net.Dialer{}).DialContext(ctx, network, addr)
	}
	if err == nil {
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

// LastOK returns the time of the last successful direct dial (zero = never).
// Lock-free; safe from any goroutine.
func (d *trackingDialer) LastOK() time.Time {
	n := d.lastOK.Load()
	if n == 0 {
		return time.Time{}
	}
	return time.Unix(0, n)
}

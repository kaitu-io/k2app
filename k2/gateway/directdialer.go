//go:build linux

package gateway

import (
	"context"
	"net"
	"syscall"

	"golang.org/x/sys/unix"
)

// K2RFwmark is the firewall mark k2r stamps on its own direct-network sockets
// (DirectDNS upstream queries, direct route outbounds) so the K2R_DNS_OUT NAT
// chain can RETURN them before the port 53 REDIRECT. Without this exemption,
// k2r's own DNS query to e.g. 223.5.5.5:53 would loop back into the DNS
// listener and recurse indefinitely.
//
// 0x4b32 = "K2" in ASCII; not a kernel-reserved or commonly-used value so it
// won't collide with other software's marks on a typical router.
const K2RFwmark = 0x4b32

// gatewayDirectDialer is k2r's wire.DirectDialer + wire.DialerController
// implementation. It does no interface binding (k2r has no TUN to bypass);
// its sole job is to set SO_MARK on every direct socket so OUTPUT REDIRECT
// doesn't trap k2r's own DNS resolution traffic.
type gatewayDirectDialer struct{}

// DialContext dials TCP/UDP with the k2r fwmark applied to the socket.
func (d *gatewayDirectDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	nd := &net.Dialer{Control: d.ControlFunc()}
	return nd.DialContext(ctx, network, addr)
}

// ListenPacket creates a marked UDP socket — used by DirectDNS's persistent
// pooling and by any direct UDP outbound.
func (d *gatewayDirectDialer) ListenPacket(network, addr string) (net.PacketConn, error) {
	lc := &net.ListenConfig{Control: d.ControlFunc()}
	return lc.ListenPacket(context.Background(), network, addr)
}

// ControlFunc returns a syscall control callback that stamps SO_MARK on the
// raw socket fd before connect/listen. Required by wire.DialerController so
// that DNS direct queries (which the engine wraps in their own net.Dialer)
// also pick up the mark.
//
// SO_MARK requires CAP_NET_ADMIN to set — k2r runs as root on routers, so
// this always succeeds in production. Set errors are returned but in practice
// won't fire; we log via the caller if they do.
func (d *gatewayDirectDialer) ControlFunc() func(network, address string, c syscall.RawConn) error {
	return func(network, address string, c syscall.RawConn) error {
		var setErr error
		ctrlErr := c.Control(func(fd uintptr) {
			setErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_MARK, K2RFwmark)
		})
		if ctrlErr != nil {
			return ctrlErr
		}
		return setErr
	}
}

package core

import (
	"context"
	"net"

	"github.com/kaitu-io/k2/wire"
)

// directOutbound routes connections via the local network (net.Dial).
type directOutbound struct {
	dialer wire.DirectDialer // nil → stdlib net.Dial/ListenPacket
}

// NewDirectOutbound creates an outbound that dials destinations directly.
// If dialer is nil, stdlib net.Dial/net.ListenPacket are used.
func NewDirectOutbound(dialer wire.DirectDialer) Outbound {
	return &directOutbound{dialer: dialer}
}

func (d *directOutbound) DialTCP(ctx context.Context, dest string, _ []byte) (net.Conn, error) {
	if d.dialer != nil {
		return d.dialer.DialContext(ctx, "tcp", dest)
	}
	return (&net.Dialer{}).DialContext(ctx, "tcp", dest)
}

func (d *directOutbound) DialUDP(ctx context.Context, dest string) (net.PacketConn, net.Addr, error) {
	var pc net.PacketConn
	var err error
	if d.dialer != nil {
		pc, err = d.dialer.ListenPacket("udp4", "")
	} else {
		pc, err = net.ListenPacket("udp4", "")
	}
	if err != nil {
		return nil, nil, err
	}
	addr, err := net.ResolveUDPAddr("udp", dest)
	if err != nil {
		pc.Close()
		return nil, nil, err
	}
	return pc, addr, nil
}

func (d *directOutbound) Name() string { return "direct" }
func (d *directOutbound) Close() error { return nil }

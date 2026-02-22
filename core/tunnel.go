package core

import (
	"context"
	"io"
	"log/slog"
	"net"
	"sync"

	"github.com/kaitu-io/k2rule"
	"github.com/kaitu-io/k2/provider"
	"github.com/kaitu-io/k2/wire"
)

// ClientTunnel connects a Provider to wire.Dialer, implementing provider.ConnectionHandler.
// It routes each TCP/UDP connection through the Router, then either direct (net.Dial)
// or proxy (wire.DialTCP/DialUDP).
type ClientTunnel struct {
	provider     provider.Provider
	dialer       wire.Dialer
	router       *Router
	directDialer wire.DirectDialer

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

var _ provider.ConnectionHandler = (*ClientTunnel)(nil)

// ClientTunnelConfig holds dependencies for creating a ClientTunnel.
type ClientTunnelConfig struct {
	Provider     provider.Provider
	Dialer       wire.Dialer
	Router       *Router
	DirectDialer wire.DirectDialer
}

// NewClientTunnel creates a new L4 client tunnel.
func NewClientTunnel(cfg ClientTunnelConfig) *ClientTunnel {
	return &ClientTunnel{
		provider:     cfg.Provider,
		dialer:       cfg.Dialer,
		router:       cfg.Router,
		directDialer: cfg.DirectDialer,
	}
}

// Start starts the provider, which will call HandleTCP/HandleUDP for each connection.
func (t *ClientTunnel) Start(ctx context.Context) error {
	t.ctx, t.cancel = context.WithCancel(ctx)
	return t.provider.Start(t.ctx, t)
}

// Close stops the tunnel and provider.
func (t *ClientTunnel) Close() error {
	if t.cancel != nil {
		t.cancel()
	}
	t.wg.Wait()
	if t.provider != nil {
		t.provider.Close()
	}
	if t.dialer != nil {
		t.dialer.Close()
	}
	return nil
}

// HandleTCP processes a TCP connection from the provider.
func (t *ClientTunnel) HandleTCP(conn net.Conn, dest string) {
	t.wg.Add(1)
	defer t.wg.Done()
	defer conn.Close()

	action := t.route(dest)

	switch action {
	case k2rule.TargetDirect:
		t.handleTCPDirect(conn, dest)
	case k2rule.TargetProxy:
		t.handleTCPProxy(conn, dest)
	case k2rule.TargetReject:
		// Silently drop
	}
}

// HandleUDP processes a UDP connection from the provider.
func (t *ClientTunnel) HandleUDP(conn net.PacketConn, dest string) {
	t.wg.Add(1)
	defer t.wg.Done()
	defer conn.Close()

	action := t.route(dest)

	switch action {
	case k2rule.TargetDirect:
		t.handleUDPDirect(conn, dest)
	case k2rule.TargetProxy:
		t.handleUDPProxy(conn, dest)
	case k2rule.TargetReject:
		// Silently drop
	}
}

func (t *ClientTunnel) route(dest string) k2rule.Target {
	if t.router == nil {
		return k2rule.TargetProxy
	}
	host, _, err := net.SplitHostPort(dest)
	if err != nil {
		return k2rule.TargetProxy
	}
	return t.router.RouteAddr(host)
}

func (t *ClientTunnel) handleTCPDirect(local net.Conn, dest string) {
	var remote net.Conn
	var err error
	if t.directDialer != nil {
		remote, err = t.directDialer.DialContext(context.Background(), "tcp", dest)
	} else {
		remote, err = net.Dial("tcp", dest)
	}
	if err != nil {
		slog.Warn("tunnel: direct TCP dial failed", "dest", dest, "err", err)
		return
	}
	defer remote.Close()
	pipe(local, remote)
}

func (t *ClientTunnel) handleTCPProxy(local net.Conn, dest string) {
	remote, err := t.dialer.DialTCP(t.ctx, dest)
	if err != nil {
		slog.Warn("tunnel: proxy TCP dial failed", "dest", dest, "err", err)
		return
	}
	defer remote.Close()
	pipe(local, remote)
}

func (t *ClientTunnel) handleUDPDirect(local net.PacketConn, dest string) {
	var remote net.PacketConn
	var err error
	if t.directDialer != nil {
		remote, err = t.directDialer.ListenPacket("udp", "")
	} else {
		remote, err = net.ListenPacket("udp", "")
	}
	if err != nil {
		slog.Warn("tunnel: direct UDP listen failed", "err", err)
		return
	}
	defer remote.Close()

	destAddr, err := net.ResolveUDPAddr("udp", dest)
	if err != nil {
		slog.Warn("tunnel: resolve UDP addr failed", "dest", dest, "err", err)
		return
	}
	pipePacket(local, remote, destAddr)
}

func (t *ClientTunnel) handleUDPProxy(local net.PacketConn, dest string) {
	remote, err := t.dialer.DialUDP(t.ctx, dest)
	if err != nil {
		slog.Warn("tunnel: proxy UDP dial failed", "dest", dest, "err", err)
		return
	}
	defer remote.Close()
	pipePacket(local, remote, nil)
}

// Buffer pools to avoid per-connection allocations.
var (
	tcpBufPool = sync.Pool{New: func() any { b := make([]byte, 32*1024); return &b }}
	udpBufPool = sync.Pool{New: func() any { b := make([]byte, 4096); return &b }}
)

// pipe copies data bidirectionally between two connections.
// Each direction's completion closes the peer to unblock the other.
func pipe(a, b net.Conn) {
	done := make(chan struct{})
	go func() {
		bp := tcpBufPool.Get().(*[]byte)
		io.CopyBuffer(b, a, *bp)
		tcpBufPool.Put(bp)
		b.Close()
		close(done)
	}()
	bp := tcpBufPool.Get().(*[]byte)
	io.CopyBuffer(a, b, *bp)
	tcpBufPool.Put(bp)
	a.Close()
	<-done
}

// pipePacket forwards packets between two PacketConns.
// destAddr is the target for local→remote; if nil, WriteTo uses nil addr.
func pipePacket(local, remote net.PacketConn, destAddr net.Addr) {
	done := make(chan struct{}, 2)

	// local → remote
	go func() {
		bp := udpBufPool.Get().(*[]byte)
		buf := *bp
		for {
			n, _, err := local.ReadFrom(buf)
			if err != nil {
				break
			}
			if _, err := remote.WriteTo(buf[:n], destAddr); err != nil {
				break
			}
		}
		udpBufPool.Put(bp)
		done <- struct{}{}
	}()

	// remote → local
	go func() {
		bp := udpBufPool.Get().(*[]byte)
		buf := *bp
		for {
			n, from, err := remote.ReadFrom(buf)
			if err != nil {
				break
			}
			if _, err := local.WriteTo(buf[:n], from); err != nil {
				break
			}
		}
		udpBufPool.Put(bp)
		done <- struct{}{}
	}()

	<-done
}

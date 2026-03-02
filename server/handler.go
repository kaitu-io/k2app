package server

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/kaitu-io/k2/wire"
)

// Buffer pools to avoid per-connection allocations.
var (
	tcpBufPool = sync.Pool{New: func() any { b := make([]byte, 32*1024); return &b }}
	udpBufPool = sync.Pool{New: func() any { b := make([]byte, 4096); return &b }}
)

// cgn is the Carrier-Grade NAT range (100.64.0.0/10), not covered by net.IP.IsPrivate().
var cgn = net.IPNet{
	IP:   net.IP{100, 64, 0, 0},
	Mask: net.CIDRMask(10, 32),
}

// isReservedIP returns true if the IP is in a reserved range that should not be
// reachable through the proxy (private, loopback, link-local, CGN, unspecified).
func isReservedIP(ip net.IP) bool {
	return ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() || cgn.Contains(ip)
}

// checkAddr resolves addr (host:port) and returns an error if the target IP is reserved.
func checkAddr(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid address %q: %w", addr, err)
	}

	// Fast path: host is an IP literal.
	if ip := net.ParseIP(host); ip != nil {
		if isReservedIP(ip) {
			return fmt.Errorf("blocked: %s is a reserved IP", host)
		}
		return nil
	}

	// Host is a domain — resolve and check all IPs.
	ips, err := net.DefaultResolver.LookupHost(context.TODO(), host)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", host, err)
	}
	for _, ipStr := range ips {
		if ip := net.ParseIP(ipStr); ip != nil && isReservedIP(ip) {
			return fmt.Errorf("blocked: %s resolves to reserved IP %s", host, ipStr)
		}
	}
	return nil
}

// ProxyHandler processes TCP and UDP proxy requests from k2v5 clients.
type ProxyHandler struct {
	DialTimeout    time.Duration
	AllowPrivateIP bool // for testing only; default false blocks reserved IPs
}

// NewProxyHandler creates a proxy handler with default settings.
func NewProxyHandler() *ProxyHandler {
	return &ProxyHandler{DialTimeout: 10 * time.Second}
}

// HandleTCP proxies a TCP stream to the target address.
// Writes a stream response, then pipes data bidirectionally.
func (h *ProxyHandler) HandleTCP(conn net.Conn, addr string) {
	defer conn.Close()
	slog.Debug("server: HandleTCP enter", "addr", addr)

	if !h.AllowPrivateIP {
		if err := checkAddr(addr); err != nil {
			wire.WriteStreamResponse(conn, &wire.StreamError{Code: 403, Message: err.Error()})
			slog.Error("server: TCP proxy blocked", "addr", addr, "err", err)
			return
		}
	}

	slog.Debug("server: HandleTCP dialing target", "addr", addr)
	target, err := net.DialTimeout("tcp4", addr, h.DialTimeout)
	if err != nil {
		wire.WriteStreamResponse(conn, &wire.StreamError{Code: 502, Message: err.Error()})
		slog.Error("server: TCP proxy dial", "addr", addr, "err", err)
		return
	}
	defer target.Close()
	slog.Debug("server: HandleTCP target connected, writing response", "addr", addr)

	if err := wire.WriteStreamResponse(conn, nil); err != nil {
		slog.Debug("server: HandleTCP write response failed", "addr", addr, "err", err)
		return
	}

	slog.Debug("server: HandleTCP piping", "addr", addr)
	pipe(conn, target)
}

// HandleUDP proxies a UDP session to the target address.
// Implements wire.UDPHandler.
func (h *ProxyHandler) HandleUDP(conn net.PacketConn, addr string) {
	slog.Info("server: HandleUDP start", "addr", addr)
	if !h.AllowPrivateIP {
		if err := checkAddr(addr); err != nil {
			slog.Error("server: UDP proxy blocked", "addr", addr, "err", err)
			conn.Close()
			return
		}
	}

	targetAddr, err := net.ResolveUDPAddr("udp4", addr)
	if err != nil {
		slog.Error("server: UDP resolve", "addr", addr, "err", err)
		conn.Close()
		return
	}

	target, err := net.DialUDP("udp4", nil, targetAddr)
	if err != nil {
		slog.Error("server: UDP dial", "addr", addr, "err", err)
		conn.Close()
		return
	}

	done := make(chan struct{})

	// client -> target
	go func() {
		defer close(done)
		bp := udpBufPool.Get().(*[]byte)
		buf := *bp
		for {
			n, _, err := conn.ReadFrom(buf)
			if err != nil {
				slog.Debug("server: HandleUDP client->target ReadFrom done", "addr", addr, "err", err)
				break
			}
			slog.Debug("server: HandleUDP client->target forwarding", "addr", addr, "len", n)
			if _, err := target.Write(buf[:n]); err != nil {
				slog.Warn("server: HandleUDP client->target Write failed", "addr", addr, "err", err)
				break
			}
		}
		udpBufPool.Put(bp)
	}()

	// target -> client
	bp := udpBufPool.Get().(*[]byte)
	buf := *bp
	for {
		target.SetReadDeadline(time.Now().Add(30 * time.Second))
		n, err := target.Read(buf)
		if err != nil {
			slog.Debug("server: HandleUDP target->client Read done", "addr", addr, "err", err)
			break
		}
		slog.Debug("server: HandleUDP target->client forwarding", "addr", addr, "len", n)
		if _, err := conn.WriteTo(buf[:n], nil); err != nil {
			slog.Warn("server: HandleUDP target->client WriteTo failed", "addr", addr, "err", err)
			break
		}
	}
	udpBufPool.Put(bp)

	conn.Close()
	target.Close()
	<-done
	slog.Debug("server: HandleUDP done", "addr", addr)
}

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

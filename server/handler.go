package server

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"time"

	"github.com/kaitu-io/k2/wire"
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

	if !h.AllowPrivateIP {
		if err := checkAddr(addr); err != nil {
			wire.WriteStreamResponse(conn, err)
			log.Printf("server: TCP proxy blocked %s: %v", addr, err)
			return
		}
	}

	target, err := net.DialTimeout("tcp", addr, h.DialTimeout)
	if err != nil {
		wire.WriteStreamResponse(conn, err)
		log.Printf("server: TCP proxy dial %s: %v", addr, err)
		return
	}
	defer target.Close()

	if err := wire.WriteStreamResponse(conn, nil); err != nil {
		return
	}

	pipe(conn, target)
}

// HandleUDP proxies a UDP session to the target address.
// Implements wire.UDPHandler.
func (h *ProxyHandler) HandleUDP(conn net.PacketConn, addr string) {
	if !h.AllowPrivateIP {
		if err := checkAddr(addr); err != nil {
			log.Printf("server: UDP proxy blocked %s: %v", addr, err)
			conn.Close()
			return
		}
	}

	targetAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		log.Printf("server: UDP resolve %s: %v", addr, err)
		conn.Close()
		return
	}

	target, err := net.DialUDP("udp", nil, targetAddr)
	if err != nil {
		log.Printf("server: UDP dial %s: %v", addr, err)
		conn.Close()
		return
	}

	done := make(chan struct{})

	// client -> target
	go func() {
		defer close(done)
		buf := make([]byte, 4096)
		for {
			n, _, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			if _, err := target.Write(buf[:n]); err != nil {
				return
			}
		}
	}()

	// target -> client
	buf := make([]byte, 4096)
	for {
		target.SetReadDeadline(time.Now().Add(30 * time.Second))
		n, err := target.Read(buf)
		if err != nil {
			break
		}
		if _, err := conn.WriteTo(buf[:n], nil); err != nil {
			break
		}
	}

	conn.Close()
	target.Close()
	<-done
}

// pipe copies data bidirectionally between two connections.
// Each direction's completion closes the peer to unblock the other.
func pipe(a, b net.Conn) {
	done := make(chan struct{})
	go func() {
		io.Copy(b, a)
		b.Close()
		close(done)
	}()
	io.Copy(a, b)
	a.Close()
	<-done
}

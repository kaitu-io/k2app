package server

import (
	"io"
	"log"
	"net"
	"time"

	"github.com/kaitu-io/k2/wire"
)

// ProxyHandler processes TCP and UDP proxy requests from k2v5 clients.
type ProxyHandler struct {
	DialTimeout time.Duration
}

// NewProxyHandler creates a proxy handler with default settings.
func NewProxyHandler() *ProxyHandler {
	return &ProxyHandler{DialTimeout: 10 * time.Second}
}

// HandleTCP proxies a TCP stream to the target address.
// Writes a stream response, then pipes data bidirectionally.
func (h *ProxyHandler) HandleTCP(conn net.Conn, addr string) {
	defer conn.Close()

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

package engine

import (
	"net"

	"github.com/kaitu-io/k2/core/dns"
	"github.com/kaitu-io/k2/provider"
)

// dnsHandler wraps a ConnectionHandler to intercept DNS queries (port 53).
// UDP packets destined for port 53 are handled locally by the DNS middleware
// instead of being forwarded through the tunnel. TCP and non-DNS UDP pass through.
type dnsHandler struct {
	inner provider.ConnectionHandler
	dns   *dns.Middleware
}

func (h *dnsHandler) HandleTCP(conn net.Conn, dest string) {
	h.inner.HandleTCP(conn, dest)
}

func (h *dnsHandler) HandleUDP(conn net.PacketConn, dest string) {
	_, port, _ := net.SplitHostPort(dest)
	if port == "53" && h.dns != nil {
		h.serveDNS(conn)
		return
	}
	h.inner.HandleUDP(conn, dest)
}

func (h *dnsHandler) serveDNS(conn net.PacketConn) {
	defer conn.Close()
	buf := make([]byte, 4096)
	for {
		n, addr, err := conn.ReadFrom(buf)
		if err != nil {
			return
		}
		resp, err := h.dns.HandleQuery(buf[:n])
		if err != nil {
			continue
		}
		conn.WriteTo(resp, addr)
	}
}

package engine

import (
	"encoding/binary"
	"io"
	"net"
	"sync"

	"github.com/kaitu-io/k2/core/dns"
	"github.com/kaitu-io/k2/provider"
)

// dnsBufPool pools 4096-byte buffers for DNS TCP message reads.
var dnsBufPool = sync.Pool{New: func() any { b := make([]byte, 4096); return &b }}

// dnsHandler wraps a ConnectionHandler to intercept DNS queries (port 53).
// Both TCP and UDP packets destined for port 53 are handled locally by the
// DNS middleware instead of being forwarded through the tunnel.
type dnsHandler struct {
	inner provider.ConnectionHandler
	dns   *dns.Middleware
}

func (h *dnsHandler) HandleTCP(conn net.Conn, dest string) {
	_, port, _ := net.SplitHostPort(dest)
	if port == "53" && h.dns != nil {
		h.serveDNSStream(conn)
		return
	}
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

// serveDNSStream handles DNS-over-TCP (RFC 1035 §4.2.2).
// Each message is prefixed with a 2-byte big-endian length.
func (h *dnsHandler) serveDNSStream(conn net.Conn) {
	defer conn.Close()
	bp := dnsBufPool.Get().(*[]byte)
	defer dnsBufPool.Put(bp)
	for {
		// Read 2-byte length prefix.
		var length uint16
		if err := binary.Read(conn, binary.BigEndian, &length); err != nil {
			return
		}
		if length == 0 {
			return
		}

		// Use pooled buffer for typical messages, heap-allocate for oversized.
		var buf []byte
		if int(length) <= len(*bp) {
			buf = (*bp)[:length]
		} else {
			buf = make([]byte, length)
		}

		if _, err := io.ReadFull(conn, buf); err != nil {
			return
		}

		resp, err := h.dns.HandleQuery(buf)
		if err != nil {
			continue
		}

		// Write 2-byte length prefix + response.
		var respLen [2]byte
		binary.BigEndian.PutUint16(respLen[:], uint16(len(resp)))
		if _, err := conn.Write(respLen[:]); err != nil {
			return
		}
		if _, err := conn.Write(resp); err != nil {
			return
		}
	}
}

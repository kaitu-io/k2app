package engine

import (
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/kaitu-io/k2/core/dns"
	"github.com/kaitu-io/k2/provider"
)

// dnsBufPool pools 4096-byte buffers for DNS TCP message reads.
var dnsBufPool = sync.Pool{New: func() any { b := make([]byte, 4096); return &b }}

// dnsHandler wraps a ConnectionHandler to intercept DNS queries (port 53).
// Both TCP and UDP packets destined for port 53 are handled locally by the
// DNS middleware instead of being forwarded through the tunnel.
type dnsHandler struct {
	inner  provider.ConnectionHandler
	dns    *dns.Middleware
	engine *Engine // for wire error reporting (same package); nil in tests

	consecutiveFails atomic.Int32
}

const dnsWireErrorThreshold int32 = 30

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
			if isDNSParseError(err) {
				// Incoming query is malformed (not a wire/upstream issue).
				// Send SERVFAIL so the client retries immediately instead
				// of waiting for a DNS timeout.
				if sf := buildSERVFAIL(buf[:n]); sf != nil {
					conn.WriteTo(sf, addr)
				}
				continue
			}
			h.recordDNSFailure(err)
			// Upstream failure: send SERVFAIL so the client retries
			// immediately instead of waiting for a DNS timeout.
			if sf := buildSERVFAIL(buf[:n]); sf != nil {
				conn.WriteTo(sf, addr)
			}
			continue
		}
		h.recordDNSSuccess()
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
			if !isDNSParseError(err) {
				h.recordDNSFailure(err)
			}
			// Send SERVFAIL so the client retries immediately.
			if sf := buildSERVFAIL(buf); sf != nil {
				var respLen [2]byte
				binary.BigEndian.PutUint16(respLen[:], uint16(len(sf)))
				conn.Write(respLen[:])
				conn.Write(sf)
			}
			continue
		}
		h.recordDNSSuccess()

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

// isDNSParseError returns true if the error is from parsing the incoming
// DNS query (not an upstream/wire failure). These should not count toward
// the wire error threshold.
func isDNSParseError(err error) bool {
	s := err.Error()
	return strings.HasPrefix(s, "unpack DNS query") || s == "no domain in DNS query"
}

// buildSERVFAIL constructs a minimal DNS SERVFAIL response from raw query
// bytes. Only the query ID (first 2 bytes) is needed. Returns nil if the
// packet is too short to contain a valid DNS header (12 bytes).
func buildSERVFAIL(query []byte) []byte {
	if len(query) < 12 {
		return nil
	}
	// DNS header: ID(2) + Flags(2) + QDCOUNT(2) + ANCOUNT(2) + NSCOUNT(2) + ARCOUNT(2)
	resp := make([]byte, 12)
	// Copy query ID
	resp[0] = query[0]
	resp[1] = query[1]
	// Flags: QR=1 (response), RCODE=2 (SERVFAIL)
	resp[2] = 0x80 // QR=1, Opcode=0
	resp[3] = 0x02 // RCODE=SERVFAIL
	// All counts zero (no question/answer/authority/additional sections)
	return resp
}

func (h *dnsHandler) recordDNSFailure(err error) {
	if h.engine == nil {
		return
	}
	if h.consecutiveFails.Add(1) == dnsWireErrorThreshold {
		slog.Warn("dns: consecutive failure threshold reached",
			"threshold", dnsWireErrorThreshold,
			"lastErr", err.Error(),
		)
		h.engine.ReportWireError(fmt.Errorf("DNS: %d consecutive failures: %w",
			dnsWireErrorThreshold, err))
	}
}

func (h *dnsHandler) recordDNSSuccess() {
	if h.engine == nil {
		return
	}
	if prev := h.consecutiveFails.Swap(0); prev >= dnsWireErrorThreshold {
		slog.Info("dns: recovered after consecutive failures", "previousFails", prev)
		h.engine.ClearWireError()
	}
}

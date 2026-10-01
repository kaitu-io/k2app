package engine

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kaitu-io/k2/core/dns"
	"github.com/kaitu-io/k2/provider"
	"github.com/kaitu-io/k2/rule"
)

// dnsBufPool pools 4096-byte buffers for DNS TCP message reads.
var dnsBufPool = sync.Pool{New: func() any { b := make([]byte, 4096); return &b }}

// dnsQueryTimeout is the per-query context timeout.
// Each DNS query gets its own bounded context derived from the engine context.
const dnsQueryTimeout = 30 * time.Second

// dnsHandler wraps a ConnectionHandler to intercept DNS queries (port 53).
// Both TCP and UDP packets destined for port 53 are handled locally by the
// DNS middleware instead of being forwarded through the tunnel.
type dnsHandler struct {
	inner  provider.ConnectionHandler
	dns    *dns.Middleware
	ctx    context.Context // engine context — cancelled on Stop()
	engine *Engine         // for wire error reporting (same package); nil in tests

	// forward, when non-empty (host:port), bypasses the DNS middleware:
	// port-53 flows are handed to inner with dest rewritten to this
	// upstream, so they route through the rule engine like ordinary
	// traffic. Multi-slot gateway mode: each LAN subnet's DNS exits via
	// its own slot outbound (DNS exit IP == traffic exit IP); a dead
	// slot rejects DNS too (fail-closed).
	forward string

	// searcher, when non-nil, attributes the DNS QUERY connection itself to
	// a process, so a purely-process App Bypass route can steer the resolver
	// view (rule.MatchDNSMeta via Middleware.HandleQueryMeta). Wired with the
	// SAME gated searcher the tunnel gets (engine.go gateProcessSearcher):
	// nil whenever no loaded rule has process criteria, so the 99% of users
	// without an App Bypass list pay zero attribution cost per DNS query.
	// Platform reality check: attribution only identifies apps that resolve
	// from their own process (Chromium/Electron async resolvers, most Linux
	// apps). System-resolver queries attribute to svchost/mDNSResponder,
	// match no app rule, and fall through unchanged — fail-open by design.
	searcher provider.ProcessSearcher

	consecutiveFails atomic.Int32
}

// queryMeta builds the process-identity-only ConnMeta for one DNS flow, or
// nil when attribution is off/unavailable. Only process fields are set —
// every other ConnMeta dimension stays at its fail-closed zero value so
// combined routes ({apps, port: 443}…) never match the query itself (see
// rule.MatchDNSMeta).
func (h *dnsHandler) queryMeta(network string, src *net.UDPAddr, dest string) *rule.ConnMeta {
	if h.searcher == nil || src == nil {
		return nil
	}
	addr, ok := netip.AddrFromSlice(src.IP)
	if !ok {
		return nil
	}
	// Android's owner-UID searcher needs the remote address too
	// (getConnectionOwnerUid takes the full 4-tuple); dest is the DNS flow's
	// original destination (TUN DNS IP:53) as the app's kernel socket sees it.
	if cs, ok := h.searcher.(provider.ConnProcessSearcher); ok {
		if dst, perr := netip.ParseAddrPort(dest); perr == nil {
			name, pkgs, installer := cs.FindProcessConn(network, addr.Unmap(), uint16(src.Port), dst.Addr().Unmap(), dst.Port())
			if name == "" && len(pkgs) == 0 && installer == "" {
				return nil
			}
			meta := &rule.ConnMeta{ProcessName: name, PackageNames: pkgs, InstallerPackage: installer}
			if len(pkgs) > 0 {
				meta.PackageName = pkgs[0]
			}
			return meta
		}
	}
	if ps, ok := h.searcher.(provider.ProcessPathSearcher); ok {
		name, path := ps.FindProcessPath(network, addr.Unmap(), uint16(src.Port))
		if name == "" && path == "" {
			return nil
		}
		return &rule.ConnMeta{ProcessName: name, ProcessPath: path}
	}
	name, pkg, installer := h.searcher.FindProcess(network, addr.Unmap(), uint16(src.Port))
	if name == "" && pkg == "" && installer == "" {
		return nil
	}
	return &rule.ConnMeta{ProcessName: name, PackageName: pkg, InstallerPackage: installer}
}

const dnsWireErrorThreshold int32 = 30

func (h *dnsHandler) HandleTCP(conn net.Conn, dest string) {
	_, port, _ := net.SplitHostPort(dest)
	if port == "53" && h.forward != "" {
		h.inner.HandleTCP(conn, h.forward)
		return
	}
	if port == "53" && h.dns != nil {
		h.serveDNSStream(conn, dest)
		return
	}
	h.inner.HandleTCP(conn, dest)
}

func (h *dnsHandler) HandleUDP(conn net.PacketConn, dest string) {
	_, port, _ := net.SplitHostPort(dest)
	if port == "53" && h.forward != "" {
		h.inner.HandleUDP(conn, h.forward)
		return
	}
	if port == "53" && h.dns != nil {
		h.serveDNS(conn, dest)
		return
	}
	h.inner.HandleUDP(conn, dest)
}

func (h *dnsHandler) queryCtx() (context.Context, context.CancelFunc) {
	ctx := h.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(ctx, dnsQueryTimeout)
}

func (h *dnsHandler) serveDNS(conn net.PacketConn, dest string) {
	defer conn.Close()
	// One gVisor PacketConn == one LAN flow: attribute its source once, not
	// per query. nil when attribution is off/unavailable (fail-open).
	meta := h.queryMeta("udp", provider.SourcePacketAddr(conn), dest)
	buf := make([]byte, 4096)
	for {
		n, addr, err := conn.ReadFrom(buf)
		if err != nil {
			return
		}
		h.processOneUDPQuery(conn, buf[:n], addr, meta)
	}
}

// processOneUDPQuery handles a single UDP DNS query under a recover guard.
// A panic in the middleware (corrupted rule bundle, unexpected type assertion,
// dependency bug) must not propagate to the serveDNS goroutine root, where
// debug.SetTraceback("crash") in appext converts it to a SIGABRT that kills
// the iOS/Android VPN session. After recover, we send SERVFAIL so the client
// retries immediately instead of waiting for a DNS timeout.
func (h *dnsHandler) processOneUDPQuery(conn net.PacketConn, query []byte, addr net.Addr, meta *rule.ConnMeta) {
	defer func() {
		if r := recover(); r != nil {
			slog.Warn("DIAG: dns-handler-panic",
				"transport", "udp",
				"panic", fmt.Sprint(r),
				"stack", string(debug.Stack()),
			)
			if sf := buildSERVFAIL(query); sf != nil {
				conn.WriteTo(sf, addr)
			}
		}
	}()
	ctx, cancel := h.queryCtx()
	resp, err := h.dns.HandleQueryMeta(ctx, query, meta)
	cancel()
	if err != nil {
		if !isDNSParseError(err) {
			h.recordDNSFailure(err)
		}
		if sf := buildSERVFAIL(query); sf != nil {
			conn.WriteTo(sf, addr)
		}
		return
	}
	h.recordDNSSuccess()
	conn.WriteTo(resp, addr)
}

// serveDNSStream handles DNS-over-TCP (RFC 1035 §4.2.2).
// Each message is prefixed with a 2-byte big-endian length.
func (h *dnsHandler) serveDNSStream(conn net.Conn, dest string) {
	defer conn.Close()
	meta := h.queryMeta("tcp", provider.SourceAddr(conn), dest)
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

		// processOneTCPQuery returns false to signal that the loop should
		// exit (write error, conn unrecoverable). It returns true after a
		// recovered panic so the next query can be served on the same conn.
		if !h.processOneTCPQuery(conn, buf, meta) {
			return
		}
	}
}

// processOneTCPQuery handles a single TCP DNS query under a recover guard.
// Returns false if the underlying conn write failed (caller should exit the
// loop); true otherwise (including after a recovered panic — the conn is
// still usable for the next length-prefixed message).
func (h *dnsHandler) processOneTCPQuery(conn net.Conn, query []byte, meta *rule.ConnMeta) bool {
	keepGoing := true
	defer func() {
		if r := recover(); r != nil {
			slog.Warn("DIAG: dns-handler-panic",
				"transport", "tcp",
				"panic", fmt.Sprint(r),
				"stack", string(debug.Stack()),
			)
			if sf := buildSERVFAIL(query); sf != nil {
				var respLen [2]byte
				binary.BigEndian.PutUint16(respLen[:], uint16(len(sf)))
				conn.Write(respLen[:])
				conn.Write(sf)
			}
		}
	}()
	ctx, cancel := h.queryCtx()
	resp, err := h.dns.HandleQueryMeta(ctx, query, meta)
	cancel()
	if err != nil {
		if !isDNSParseError(err) {
			h.recordDNSFailure(err)
		}
		if sf := buildSERVFAIL(query); sf != nil {
			var respLen [2]byte
			binary.BigEndian.PutUint16(respLen[:], uint16(len(sf)))
			conn.Write(respLen[:])
			conn.Write(sf)
		}
		return keepGoing
	}
	h.recordDNSSuccess()

	var respLen [2]byte
	binary.BigEndian.PutUint16(respLen[:], uint16(len(resp)))
	if _, err := conn.Write(respLen[:]); err != nil {
		keepGoing = false
		return keepGoing
	}
	if _, err := conn.Write(resp); err != nil {
		keepGoing = false
	}
	return keepGoing
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
	count := h.consecutiveFails.Add(1)
	if count == dnsWireErrorThreshold {
		// Log for diagnostics, but do NOT report as wire error.
		// DNS failure is a symptom (local network down, remote DNS broken, etc.),
		// not a wire transport diagnosis. Wire errors come from proxy dial and probe.
		slog.Warn("dns: consecutive failure threshold reached",
			"threshold", dnsWireErrorThreshold,
			"lastErr", err.Error(),
		)
	}
}

func (h *dnsHandler) recordDNSSuccess() {
	if h.engine == nil {
		return
	}
	if prev := h.consecutiveFails.Swap(0); prev >= dnsWireErrorThreshold {
		slog.Info("dns: recovered after consecutive failures", "previousFails", prev)
	}
}

// CancelInflight cancels all pending upstream DNS queries.
// Called on network change — stale queries hold old sockets.
func (h *dnsHandler) CancelInflight() {
	if h.dns != nil {
		h.dns.CancelInflight()
	}
}

// ResetCounter resets the consecutive DNS failure counter.
// Called on network recovery — old failures from the outage are stale.
func (h *dnsHandler) ResetCounter() {
	if prev := h.consecutiveFails.Swap(0); prev > 0 {
		slog.Debug("dns: failure counter reset", "previousFails", prev)
	}
}

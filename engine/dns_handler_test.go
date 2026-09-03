package engine

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/kaitu-io/k2/core/dns"
	"github.com/kaitu-io/k2/rule"
	mdns "github.com/miekg/dns"
)

// failingMiddleware returns an error for every query.
type failingMiddleware struct{}

func (f *failingMiddleware) HandleQuery(ctx context.Context, query []byte) ([]byte, error) {
	return nil, fmt.Errorf("upstream unreachable")
}

// --- UDP tests ---

func TestServeDNS_UpstreamFailure_ReturnsSERVFAIL(t *testing.T) {
	// Build a valid DNS query.
	msg := new(mdns.Msg)
	msg.SetQuestion(mdns.Fqdn("example.com"), mdns.TypeA)
	msg.Id = 0xABCD
	queryBytes, err := msg.Pack()
	if err != nil {
		t.Fatalf("pack query: %v", err)
	}

	// Create a UDP pipe.
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	// Wrap in packetConn adapter.
	clientPC := &pipePacketConn{Conn: client}
	serverPC := &pipePacketConn{Conn: server}

	mw := dns.NewMiddleware(dns.MiddlewareConfig{
		DirectDNS:  &alwaysFailQuerier{},
		ProxyDNS:   &alwaysFailQuerier{},
		MatchRule:    func(string) rule.Target { return rule.Target(0) },
		SetTmpRule:   func(string, rule.Target) {},
		RejectTarget: rule.Target(1),
	})

	h := &dnsHandler{dns: mw}

	// Run serveDNS in background.
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.serveDNS(serverPC, "198.18.0.2:53")
	}()

	// Send query from client side.
	clientPC.WriteTo(queryBytes, nil)

	// Read response.
	buf := make([]byte, 4096)
	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := clientPC.ReadFrom(buf)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}

	// Verify SERVFAIL response.
	if n < 12 {
		t.Fatalf("response too short: %d bytes", n)
	}

	// Check query ID preserved.
	respID := binary.BigEndian.Uint16(buf[:2])
	if respID != 0xABCD {
		t.Errorf("query ID mismatch: got 0x%04X, want 0xABCD", respID)
	}

	// Check QR=1 (response) and RCODE=SERVFAIL (2).
	if buf[2]&0x80 == 0 {
		t.Error("QR bit not set in response")
	}
	rcode := buf[3] & 0x0F
	if rcode != 2 {
		t.Errorf("RCODE: got %d, want 2 (SERVFAIL)", rcode)
	}

	// Close client to unblock serveDNS.
	client.Close()
	<-done
}

// --- TCP tests ---

func TestServeDNSStream_UpstreamFailure_ReturnsSERVFAIL(t *testing.T) {
	// Build a valid DNS query.
	msg := new(mdns.Msg)
	msg.SetQuestion(mdns.Fqdn("example.com"), mdns.TypeA)
	msg.Id = 0x1234
	queryBytes, err := msg.Pack()
	if err != nil {
		t.Fatalf("pack query: %v", err)
	}

	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	mw := dns.NewMiddleware(dns.MiddlewareConfig{
		DirectDNS:  &alwaysFailQuerier{},
		ProxyDNS:   &alwaysFailQuerier{},
		MatchRule:    func(string) rule.Target { return rule.Target(0) },
		SetTmpRule:   func(string, rule.Target) {},
		RejectTarget: rule.Target(1),
	})

	h := &dnsHandler{dns: mw}

	done := make(chan struct{})
	go func() {
		defer close(done)
		h.serveDNSStream(server, "198.18.0.2:53")
	}()

	// Send TCP DNS message: 2-byte length prefix + query.
	var lenBuf [2]byte
	binary.BigEndian.PutUint16(lenBuf[:], uint16(len(queryBytes)))
	client.Write(lenBuf[:])
	client.Write(queryBytes)

	// Read response: 2-byte length prefix + response.
	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	var respLen uint16
	if err := binary.Read(client, binary.BigEndian, &respLen); err != nil {
		t.Fatalf("read response length: %v", err)
	}

	respBuf := make([]byte, respLen)
	if _, err := client.Read(respBuf); err != nil {
		t.Fatalf("read response body: %v", err)
	}

	// Verify SERVFAIL.
	if len(respBuf) < 12 {
		t.Fatalf("response too short: %d bytes", len(respBuf))
	}

	respID := binary.BigEndian.Uint16(respBuf[:2])
	if respID != 0x1234 {
		t.Errorf("query ID mismatch: got 0x%04X, want 0x1234", respID)
	}

	rcode := respBuf[3] & 0x0F
	if rcode != 2 {
		t.Errorf("RCODE: got %d, want 2 (SERVFAIL)", rcode)
	}

	client.Close()
	<-done
}

// --- helpers ---

// alwaysFailQuerier implements dns.DNSQuerier and always returns an error.
type alwaysFailQuerier struct{}

func (q *alwaysFailQuerier) Query(ctx context.Context, msg *mdns.Msg) (*mdns.Msg, error) {
	return nil, fmt.Errorf("upstream unreachable")
}

// pipePacketConn wraps net.Conn as net.PacketConn for testing with net.Pipe().
type pipePacketConn struct {
	net.Conn
}

func (p *pipePacketConn) ReadFrom(b []byte) (int, net.Addr, error) {
	n, err := p.Conn.Read(b)
	return n, nil, err
}

func (p *pipePacketConn) WriteTo(b []byte, _ net.Addr) (int, error) {
	return p.Conn.Write(b)
}

func (p *pipePacketConn) Close() error {
	return p.Conn.Close()
}

func (p *pipePacketConn) SetReadDeadline(t time.Time) error {
	return p.Conn.SetReadDeadline(t)
}

// Ensure pipePacketConn has the full PacketConn interface including LocalAddr.
func (p *pipePacketConn) LocalAddr() net.Addr {
	return p.Conn.LocalAddr()
}

// TestServeDNS_RecoversFromMiddlewarePanic verifies that a panic during DNS
// query handling does not propagate to the goroutine root. Without recover,
// a panic in the middleware (e.g. from a malicious rule bundle, a future
// type-assertion bug, or any unexpected nil deref) crashes the entire k2
// process — and on iOS/Android, debug.SetTraceback("crash") in appext turns
// it into a SIGABRT that kills the user's VPN session.
//
// Defense: serveDNS must wrap each query with recover() so a single bad
// query cannot bring down the whole DNS handler goroutine.
func TestServeDNS_RecoversFromMiddlewarePanic(t *testing.T) {
	msg := new(mdns.Msg)
	msg.SetQuestion(mdns.Fqdn("example.com"), mdns.TypeA)
	msg.Id = 0xDEAD
	queryBytes, err := msg.Pack()
	if err != nil {
		t.Fatalf("pack query: %v", err)
	}

	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	clientPC := &pipePacketConn{Conn: client}
	serverPC := &pipePacketConn{Conn: server}

	mw := dns.NewMiddleware(dns.MiddlewareConfig{
		DirectDNS:    &alwaysFailQuerier{},
		ProxyDNS:     &alwaysFailQuerier{},
		MatchRule:    func(string) rule.Target { panic("simulated rule engine panic") },
		SetTmpRule:   func(string, rule.Target) {},
		RejectTarget: rule.Target(1),
	})
	h := &dnsHandler{dns: mw}

	done := make(chan struct{})
	go func() {
		defer close(done)
		h.serveDNS(serverPC, "198.18.0.2:53")
	}()

	// Send the query. Without recover, the goroutine panics and the panic
	// propagates to the runtime, crashing the test binary. With recover,
	// serveDNS continues running until the conn is closed.
	if _, err := clientPC.WriteTo(queryBytes, nil); err != nil {
		t.Fatalf("write query: %v", err)
	}

	// Allow time for the panic-or-recover path to settle.
	time.Sleep(100 * time.Millisecond)

	// Close the client to unblock serveDNS's ReadFrom and let it exit.
	client.Close()

	select {
	case <-done:
		// serveDNS exited cleanly (recovered the panic, then ReadFrom errored
		// when the conn closed). This is the expected behavior with recover.
	case <-time.After(2 * time.Second):
		t.Fatal("serveDNS did not exit after client close — likely stuck or panicked uncleanly")
	}
}

// TestServeDNSStream_RecoversFromMiddlewarePanic mirrors the UDP test for the
// TCP DNS path (RFC 1035 §4.2.2). Same threat, same defense.
func TestServeDNSStream_RecoversFromMiddlewarePanic(t *testing.T) {
	msg := new(mdns.Msg)
	msg.SetQuestion(mdns.Fqdn("example.com"), mdns.TypeA)
	msg.Id = 0xBEEF
	queryBytes, err := msg.Pack()
	if err != nil {
		t.Fatalf("pack query: %v", err)
	}

	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	mw := dns.NewMiddleware(dns.MiddlewareConfig{
		DirectDNS:    &alwaysFailQuerier{},
		ProxyDNS:     &alwaysFailQuerier{},
		MatchRule:    func(string) rule.Target { panic("simulated rule engine panic") },
		SetTmpRule:   func(string, rule.Target) {},
		RejectTarget: rule.Target(1),
	})
	h := &dnsHandler{dns: mw}

	done := make(chan struct{})
	go func() {
		defer close(done)
		h.serveDNSStream(server, "198.18.0.2:53")
	}()

	var lenBuf [2]byte
	binary.BigEndian.PutUint16(lenBuf[:], uint16(len(queryBytes)))
	if _, err := client.Write(lenBuf[:]); err != nil {
		t.Fatalf("write length prefix: %v", err)
	}
	if _, err := client.Write(queryBytes); err != nil {
		t.Fatalf("write query: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	client.Close()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("serveDNSStream did not exit after client close — likely stuck or panicked uncleanly")
	}
}

// Ensure buildSERVFAIL works correctly.
func TestBuildSERVFAIL(t *testing.T) {
	// Too short.
	if sf := buildSERVFAIL([]byte{0x00}); sf != nil {
		t.Error("expected nil for short input")
	}

	// Valid 12-byte header.
	query := make([]byte, 12)
	query[0] = 0xAB
	query[1] = 0xCD
	sf := buildSERVFAIL(query)
	if sf == nil {
		t.Fatal("expected non-nil SERVFAIL")
	}
	if len(sf) != 12 {
		t.Fatalf("expected 12 bytes, got %d", len(sf))
	}
	if !bytes.Equal(sf[:2], []byte{0xAB, 0xCD}) {
		t.Error("query ID not preserved")
	}
	if sf[2]&0x80 == 0 {
		t.Error("QR bit not set")
	}
	if sf[3]&0x0F != 2 {
		t.Errorf("RCODE: got %d, want 2", sf[3]&0x0F)
	}
}

// --- DNSForwardUpstream (multi-slot gateway mode) ---

type recordingInner struct {
	udpDest string
	tcpDest string
}

func (r *recordingInner) HandleTCP(conn net.Conn, dest string)       { r.tcpDest = dest; conn.Close() }
func (r *recordingInner) HandleUDP(conn net.PacketConn, dest string) { r.udpDest = dest; conn.Close() }

func TestDNSHandler_ForwardUpstream(t *testing.T) {
	inner := &recordingInner{}
	h := &dnsHandler{inner: inner, dns: nil, forward: "1.1.1.1:53"}
	c1, c2 := net.Pipe()
	defer c2.Close()
	h.HandleTCP(c1, ":53")
	if inner.tcpDest != "1.1.1.1:53" {
		t.Fatalf("tcp dest = %q, want rewritten upstream", inner.tcpDest)
	}
	pc, _ := net.ListenPacket("udp", "127.0.0.1:0")
	h.HandleUDP(pc, "10.81.1.1:53")
	if inner.udpDest != "1.1.1.1:53" {
		t.Fatalf("udp dest = %q, want rewritten upstream", inner.udpDest)
	}
}

func TestDNSHandler_ForwardEmpty_KeepsMiddlewarePath(t *testing.T) {
	inner := &recordingInner{}
	h := &dnsHandler{inner: inner, dns: nil, forward: ""}
	// forward 空 + dns 中间件 nil → 现状行为:透传 inner(dest 原样)
	c1, _ := net.Pipe()
	h.HandleTCP(c1, ":53")
	if inner.tcpDest != ":53" {
		t.Fatalf("dest = %q, want passthrough unchanged", inner.tcpDest)
	}
}

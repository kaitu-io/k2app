package provider

import (
	"context"
	"net"
	"testing"
	"time"
)

// startUDPAssociate spins up a ProxyProvider, drives a real SOCKS5 UDP
// ASSOCIATE handshake over a TCP control connection (reusing
// socks5UDPHandshake from provider_test.go), and returns the relay's bound
// UDP address plus the fake handler used to observe HandleUDP calls. The
// caller must close both tcpConn (tearing the relay down per RFC 1928) and p.
func startUDPAssociate(t *testing.T) (relayAddr *net.UDPAddr, handler *testHandler, tcpConn net.Conn, p *ProxyProvider) {
	t.Helper()
	p = NewProxyProvider(ProviderConfig{ProxyListen: "127.0.0.1:0"})
	handler = newTestHandler()
	if err := p.Start(context.Background(), handler); err != nil {
		t.Fatalf("Start: %v", err)
	}

	addr := p.listener.Addr().String()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	relayAddr = socks5UDPHandshake(t, conn)
	return relayAddr, handler, conn, p
}

// T1: one client source address addressing three destinations must produce
// exactly one HandleUDP session, and that session's ReadFrom must yield each
// payload paired with its own destination, in order.
func TestUDPRelay_OneSessionManyDestinations(t *testing.T) {
	relayAddr, handler, tcpConn, p := startUDPAssociate(t)
	defer p.Close()
	defer tcpConn.Close()

	client, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("client listen: %v", err)
	}
	defer client.Close()

	dests := []string{"93.184.216.34:80", "93.184.216.34:443", "8.8.8.8:53"}
	payloads := [][]byte{[]byte("one"), []byte("two"), []byte("three")}

	for i, dest := range dests {
		pkt := buildSOCKS5UDPPacket(dest, payloads[i])
		if _, err := client.WriteTo(pkt, relayAddr); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}

	var call udpCall
	select {
	case call = <-handler.udpCalls:
	case <-time.After(2 * time.Second):
		t.Fatal("HandleUDP not called")
	}

	select {
	case <-handler.udpCalls:
		t.Fatal("HandleUDP invoked a second time for the same client source address")
	case <-time.After(200 * time.Millisecond):
	}

	buf := make([]byte, 4096)
	for i := range dests {
		call.conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, addr, err := call.conn.ReadFrom(buf)
		if err != nil {
			t.Fatalf("ReadFrom %d: %v", i, err)
		}
		if string(buf[:n]) != string(payloads[i]) {
			t.Errorf("payload %d = %q, want %q", i, buf[:n], payloads[i])
		}
		if addr == nil || addr.String() != dests[i] {
			t.Errorf("addr %d = %v, want %q", i, addr, dests[i])
		}
	}
}

// T2: two distinct client source addresses must produce two distinct sessions.
func TestUDPRelay_DistinctSourcesGetDistinctSessions(t *testing.T) {
	relayAddr, handler, tcpConn, p := startUDPAssociate(t)
	defer p.Close()
	defer tcpConn.Close()

	clientA, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("clientA listen: %v", err)
	}
	defer clientA.Close()
	clientB, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("clientB listen: %v", err)
	}
	defer clientB.Close()

	pkt := buildSOCKS5UDPPacket("93.184.216.34:80", []byte("hi"))
	if _, err := clientA.WriteTo(pkt, relayAddr); err != nil {
		t.Fatalf("clientA write: %v", err)
	}
	if _, err := clientB.WriteTo(pkt, relayAddr); err != nil {
		t.Fatalf("clientB write: %v", err)
	}

	seen := make(map[net.PacketConn]bool)
	for i := 0; i < 2; i++ {
		select {
		case call := <-handler.udpCalls:
			seen[call.conn] = true
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d HandleUDP calls observed, want 2", i)
		}
	}
	if len(seen) != 2 {
		t.Fatalf("got %d distinct sessions, want 2", len(seen))
	}
}

// T3: WriteTo(payload, peerAddr) for a peer that is not the session's
// primary destination must carry peerAddr in the SOCKS5 reply header (not
// the stale primary dest), and must be delivered to the original client.
func TestUDPSession_WriteTo_UsesAddrArgument(t *testing.T) {
	relayAddr, handler, tcpConn, p := startUDPAssociate(t)
	defer p.Close()
	defer tcpConn.Close()

	client, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("client listen: %v", err)
	}
	defer client.Close()

	primaryDest := "93.184.216.34:80"
	pkt := buildSOCKS5UDPPacket(primaryDest, []byte("req"))
	if _, err := client.WriteTo(pkt, relayAddr); err != nil {
		t.Fatalf("write: %v", err)
	}

	var call udpCall
	select {
	case call = <-handler.udpCalls:
	case <-time.After(2 * time.Second):
		t.Fatal("HandleUDP not called")
	}

	peerAddr, err := net.ResolveUDPAddr("udp", "8.8.8.8:53")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if peerAddr.String() == primaryDest {
		t.Fatalf("test setup bug: peerAddr equals primaryDest")
	}
	if _, err := call.conn.WriteTo([]byte("reply"), peerAddr); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	buf := make([]byte, 4096)
	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, from, err := client.ReadFrom(buf)
	if err != nil {
		t.Fatalf("client ReadFrom: %v", err)
	}
	if host, _, _ := net.SplitHostPort(from.String()); host != "127.0.0.1" {
		t.Errorf("reply arrived from %v, want the relay's UDP socket", from)
	}
	gotDest, payload, err := parseSOCKS5UDPHeader(buf[:n])
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if gotDest != peerAddr.String() {
		t.Errorf("reply header dest = %q, want peerAddr %q (not primary dest %q)", gotDest, peerAddr.String(), primaryDest)
	}
	if string(payload) != "reply" {
		t.Errorf("payload = %q, want %q", payload, "reply")
	}
}

package live

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/kaitu-io/k2/wire"
	"github.com/miekg/dns"
)

// TestLive_QUICDatagramDNS connects to a real k2s server and tests
// proxy DNS through QUIC datagrams. Run with:
//
//	K2_URL="k2v5://..." go test -v -run TestLive_QUICDatagramDNS ./test/live/
func TestLive_QUICDatagramDNS(t *testing.T) {
	rawURL := os.Getenv("K2_URL")
	if rawURL == "" {
		t.Skip("K2_URL not set")
	}

	cfg, err := wire.ParseURL(rawURL)
	if err != nil {
		t.Fatalf("parse URL: %v", err)
	}

	client := wire.NewQUICClient(cfg)
	defer client.Close()

	// 1. Test TCP (stream) — should work
	t.Log("=== Testing TCP (QUIC stream) ===")
	tcpCtx, tcpCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer tcpCancel()
	tcpConn, err := client.DialTCP(tcpCtx, "1.1.1.1:443", nil)
	if err != nil {
		t.Fatalf("DialTCP failed: %v", err)
	}
	tcpConn.Close()
	t.Log("TCP (stream): OK")

	// 2. Test UDP (datagram) — the suspected failure point
	t.Log("=== Testing UDP (QUIC datagram) ===")
	udpCtx, udpCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer udpCancel()
	udpConn, err := client.DialUDP(udpCtx, "8.8.8.8:53")
	if err != nil {
		t.Fatalf("DialUDP failed: %v", err)
	}
	defer udpConn.Close()
	t.Log("DialUDP: OK (connection created)")

	// Build DNS query
	msg := new(dns.Msg)
	msg.SetQuestion("google.com.", dns.TypeA)
	queryBytes, err := msg.Pack()
	if err != nil {
		t.Fatalf("pack DNS query: %v", err)
	}

	// Send
	t.Logf("Sending DNS query (%d bytes) to 8.8.8.8:53 via QUIC datagram...", len(queryBytes))
	if _, err := udpConn.WriteTo(queryBytes, nil); err != nil {
		t.Fatalf("WriteTo failed: %v", err)
	}
	t.Log("WriteTo: OK (datagram sent)")

	// Receive
	udpConn.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 4096)
	t.Log("Waiting for DNS response (5s timeout)...")
	n, _, err := udpConn.ReadFrom(buf)
	if err != nil {
		t.Fatalf("ReadFrom FAILED: %v — QUIC datagram response never arrived", err)
	}

	// Parse response
	resp := new(dns.Msg)
	if err := resp.Unpack(buf[:n]); err != nil {
		t.Fatalf("unpack DNS response: %v", err)
	}
	t.Logf("DNS response: %d bytes, %d answers", n, len(resp.Answer))
	for _, a := range resp.Answer {
		fmt.Printf("  %s\n", a.String())
	}
	t.Log("=== QUIC datagram path: WORKING ===")
}

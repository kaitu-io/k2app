package packet

import (
	"bytes"
	"testing"

	"github.com/miekg/dns"
)

func TestExtractUDPPayload(t *testing.T) {
	// Build a test UDP/IP packet
	dnsQuery := &dns.Msg{}
	dnsQuery.SetQuestion("example.com.", dns.TypeA)
	dnsBytes, _ := dnsQuery.Pack()

	pkt, err := BuildUDPIP("10.0.0.1", "8.8.8.8", 12345, 53, dnsBytes)
	if err != nil {
		t.Fatalf("BuildUDPIP failed: %v", err)
	}

	// Extract UDP payload
	payload, err := ExtractUDPPayload(pkt)
	if err != nil {
		t.Fatalf("ExtractUDPPayload failed: %v", err)
	}

	// Verify payload matches original DNS query
	if !bytes.Equal(payload, dnsBytes) {
		t.Errorf("Extracted payload doesn't match:\nGot:  %v\nWant: %v", payload, dnsBytes)
	}
}

func TestBuildUDPIP(t *testing.T) {
	testPayload := []byte("test data")

	pkt, err := BuildUDPIP("192.168.1.1", "192.168.1.2", 1234, 5678, testPayload)
	if err != nil {
		t.Fatalf("BuildUDPIP failed: %v", err)
	}

	// Verify packet structure
	if len(pkt) < 20+8+len(testPayload) {
		t.Fatalf("Packet too small: %d bytes", len(pkt))
	}

	// Verify IP version and header length
	if pkt[0]>>4 != 4 {
		t.Errorf("IP version incorrect: %d", pkt[0]>>4)
	}

	// Verify protocol is UDP (17)
	if pkt[9] != 17 {
		t.Errorf("Protocol not UDP: %d", pkt[9])
	}

	// Verify source IP
	if pkt[12] != 192 || pkt[13] != 168 || pkt[14] != 1 || pkt[15] != 1 {
		t.Errorf("Source IP incorrect: %d.%d.%d.%d", pkt[12], pkt[13], pkt[14], pkt[15])
	}

	// Verify destination IP
	if pkt[16] != 192 || pkt[17] != 168 || pkt[18] != 1 || pkt[19] != 2 {
		t.Errorf("Dest IP incorrect: %d.%d.%d.%d", pkt[16], pkt[17], pkt[18], pkt[19])
	}

	// Extract and verify payload
	extracted, err := ExtractUDPPayload(pkt)
	if err != nil {
		t.Fatalf("ExtractUDPPayload failed: %v", err)
	}

	if !bytes.Equal(extracted, testPayload) {
		t.Errorf("Payload mismatch: got %v, want %v", extracted, testPayload)
	}
}

func TestDNSPacketRoundtrip(t *testing.T) {
	// Create a DNS query
	query := &dns.Msg{}
	query.SetQuestion("google.com.", dns.TypeA)
	query.RecursionDesired = true

	queryBytes, err := query.Pack()
	if err != nil {
		t.Fatalf("DNS Pack failed: %v", err)
	}

	// Build UDP/IP packet
	pkt, err := BuildUDPIP("10.0.0.100", "8.8.8.8", 54321, 53, queryBytes)
	if err != nil {
		t.Fatalf("BuildUDPIP failed: %v", err)
	}

	// Extract UDP payload
	payload, err := ExtractUDPPayload(pkt)
	if err != nil {
		t.Fatalf("ExtractUDPPayload failed: %v", err)
	}

	// Unpack DNS message
	var extractedQuery dns.Msg
	if err := extractedQuery.Unpack(payload); err != nil {
		t.Fatalf("DNS Unpack failed: %v", err)
	}

	// Verify DNS query is intact
	if len(extractedQuery.Question) != 1 {
		t.Fatalf("Expected 1 question, got %d", len(extractedQuery.Question))
	}

	if extractedQuery.Question[0].Name != "google.com." {
		t.Errorf("Question name mismatch: got %s, want google.com.", extractedQuery.Question[0].Name)
	}

	if extractedQuery.Question[0].Qtype != dns.TypeA {
		t.Errorf("Question type mismatch: got %d, want %d", extractedQuery.Question[0].Qtype, dns.TypeA)
	}
}

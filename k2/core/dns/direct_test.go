package dns

import (
	"context"
	"testing"

	"github.com/miekg/dns"
)

func TestDirectDNSClient_New(t *testing.T) {
	// Test with custom servers
	client := NewDirectDNSClient([]string{"8.8.8.8:53", "1.1.1.1:53"})
	if len(client.resolver.servers) != 2 {
		t.Errorf("Expected 2 servers, got %d", len(client.resolver.servers))
	}
	if client.resolver.servers[0].addr != "8.8.8.8:53" {
		t.Errorf("Expected first server 8.8.8.8:53, got %s", client.resolver.servers[0].addr)
	}

	// Test with empty servers (should use defaults)
	client = NewDirectDNSClient(nil)
	if len(client.resolver.servers) != 6 {
		t.Errorf("Expected 6 default servers, got %d", len(client.resolver.servers))
	}
	if client.resolver.servers[0].addr != "114.114.114.114:53" {
		t.Errorf("Expected default server 114.114.114.114:53, got %s", client.resolver.servers[0].addr)
	}
}

func TestDirectDNSClient_Query(t *testing.T) {
	// Skip if no network available
	if testing.Short() {
		t.Skip("Skipping network test in short mode")
	}

	client := NewDirectDNSClient([]string{"8.8.8.8:53"})

	// Build DNS query for example.com
	query := new(dns.Msg)
	query.SetQuestion(dns.Fqdn("example.com"), dns.TypeA)

	resp, err := client.Query(context.Background(), query)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if resp.Rcode != dns.RcodeSuccess {
		t.Errorf("Expected successful response, got %s",
			dns.RcodeToString[resp.Rcode])
	}

	if len(resp.Answer) == 0 {
		t.Error("Expected at least one answer")
	}
}

func TestDirectDNSClient_QueryFailover(t *testing.T) {
	// Skip if no network available
	if testing.Short() {
		t.Skip("Skipping network test in short mode")
	}

	// First server is invalid, second is valid
	client := NewDirectDNSClient([]string{
		"192.0.2.1:53",    // Invalid (TEST-NET-1, should not route)
		"8.8.8.8:53",      // Valid
	})

	query := new(dns.Msg)
	query.SetQuestion(dns.Fqdn("example.com"), dns.TypeA)

	resp, err := client.Query(context.Background(), query)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if resp.Rcode != dns.RcodeSuccess {
		t.Errorf("Expected successful response, got %s",
			dns.RcodeToString[resp.Rcode])
	}
}

func TestExtractDomain(t *testing.T) {
	query := new(dns.Msg)
	query.SetQuestion(dns.Fqdn("example.com"), dns.TypeA)

	domain := extractDomain(query)
	if domain != "example.com." {
		t.Errorf("Expected domain example.com., got %s", domain)
	}

	// Test with empty query
	emptyQuery := new(dns.Msg)
	domain = extractDomain(emptyQuery)
	if domain != "<unknown>" {
		t.Errorf("Expected <unknown>, got %s", domain)
	}
}

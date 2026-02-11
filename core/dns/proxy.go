package dns

import (
	"fmt"
	"log"
	"math/rand"
	"net"
	"time"

	"github.com/kaitu-io/k2/core/packet"
	"github.com/miekg/dns"
)

// ProxyDNSClient queries DNS through the VPN tunnel
// DNS queries are sent as regular IP packets through the tunnel to upstream DNS servers
type ProxyDNSClient struct {
	wireConn net.Conn // VPN tunnel connection
	servers  []string // DNS server addresses (e.g., "8.8.8.8:53")
	clientIP string   // Client's TUN IP address
	timeout  time.Duration
}

// ProxyDNSClientConfig contains configuration for proxy DNS client
type ProxyDNSClientConfig struct {
	WireConn net.Conn
	Servers  []string
	ClientIP string
}

// NewProxyDNSClient creates a new proxy DNS client
func NewProxyDNSClient(cfg ProxyDNSClientConfig) *ProxyDNSClient {
	servers := cfg.Servers
	if len(servers) == 0 {
		// Default to Google and Cloudflare DNS
		servers = []string{
			"8.8.8.8:53",
			"1.1.1.1:53",
		}
	}

	clientIP := cfg.ClientIP
	if clientIP == "" {
		clientIP = "198.18.0.7" // Default client TUN IP
	}

	return &ProxyDNSClient{
		wireConn: cfg.WireConn,
		servers:  servers,
		clientIP: clientIP,
		timeout:  5 * time.Second,
	}
}

// Query sends a DNS query through the VPN tunnel
func (c *ProxyDNSClient) Query(query *dns.Msg) (*dns.Msg, error) {
	// Pack DNS query
	queryBytes, err := query.Pack()
	if err != nil {
		return nil, fmt.Errorf("pack DNS query: %w", err)
	}

	// Try each DNS server
	for _, server := range c.servers {
		host, port, err := net.SplitHostPort(server)
		if err != nil {
			log.Printf("Invalid DNS server address %s: %v", server, err)
			continue
		}

		// Build UDP/IP packet targeting DNS server
		srcPort := uint16(10000 + rand.Intn(55535)) // Random source port
		pkt, err := packet.BuildUDPIP(c.clientIP, host, srcPort, 53, queryBytes)
		if err != nil {
			log.Printf("Failed to build DNS packet for %s: %v", server, err)
			continue
		}

		// Send through VPN tunnel
		if err := packet.WriteFramed(c.wireConn, pkt); err != nil {
			log.Printf("Failed to send DNS query through tunnel to %s: %v", server, err)
			continue
		}

		// Wait for response with timeout
		respChan := make(chan *dns.Msg, 1)
		errChan := make(chan error, 1)

		go func() {
			// Read response from tunnel
			respPacket, err := packet.ReadFramed(c.wireConn)
			if err != nil {
				errChan <- fmt.Errorf("read DNS response: %w", err)
				return
			}

			// Extract UDP payload
			respBytes, err := packet.ExtractUDPPayload(respPacket)
			if err != nil {
				errChan <- fmt.Errorf("extract UDP payload: %w", err)
				return
			}

			// Unpack DNS response
			var resp dns.Msg
			if err := resp.Unpack(respBytes); err != nil {
				errChan <- fmt.Errorf("unpack DNS response: %w", err)
				return
			}

			respChan <- &resp
		}()

		select {
		case resp := <-respChan:
			log.Printf("Proxy DNS: %s → %s (via tunnel, port %s)",
				extractDomain(query), host, port)
			return resp, nil

		case err := <-errChan:
			log.Printf("Proxy DNS error for %s: %v", server, err)
			continue

		case <-time.After(c.timeout):
			log.Printf("Proxy DNS timeout for %s", server)
			continue
		}
	}

	return nil, fmt.Errorf("all proxy DNS servers failed")
}

// Note: extractUDPPayload and buildUDPIPPacket are imported from core package

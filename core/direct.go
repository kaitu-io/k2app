package core

import (
	"fmt"
	"net"

	"golang.org/x/net/ipv4"
)

// DirectSender sends IP packets directly to the physical network interface
// bypassing the tunnel
type DirectSender struct {
	conn *ipv4.RawConn
}

// NewDirectSender creates a new direct sender
// Requires root/administrator privileges
func NewDirectSender() (*DirectSender, error) {
	// Create raw IP socket
	c, err := net.ListenPacket("ip4:tcp", "0.0.0.0")
	if err != nil {
		return nil, fmt.Errorf("create raw socket: %w", err)
	}

	rawConn, err := ipv4.NewRawConn(c)
	if err != nil {
		c.Close()
		return nil, fmt.Errorf("create raw conn: %w", err)
	}

	return &DirectSender{conn: rawConn}, nil
}

// Send sends an IP packet directly to the network
func (d *DirectSender) Send(packet []byte) error {
	if len(packet) < 20 {
		return fmt.Errorf("packet too small")
	}

	// Parse IP header
	header, err := ipv4.ParseHeader(packet)
	if err != nil {
		return fmt.Errorf("parse IP header: %w", err)
	}

	// Extract payload (everything after IP header)
	payload := packet[header.Len:]

	// Send via raw socket
	if err := d.conn.WriteTo(header, payload, nil); err != nil {
		return fmt.Errorf("write to raw socket: %w", err)
	}

	return nil
}

// Close closes the direct sender
func (d *DirectSender) Close() error {
	return d.conn.Close()
}

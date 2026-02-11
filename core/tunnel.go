package core

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"

	"github.com/kaitu-io/k2rule"
	"github.com/kaitu-io/k2tunnel/core/packet"
	"github.com/kaitu-io/k2tunnel/provider"
	"github.com/kaitu-io/k2tunnel/wire"
)

// DNSMiddleware defines the interface for DNS query handling
type DNSMiddleware interface {
	HandleDNSQuery(pkt []byte, info packet.Info) error
}

// ClientTunnel implements the client-side tunnel
type ClientTunnel struct {
	provider      provider.Provider
	dialer        wire.Dialer
	router        *Router
	direct        *DirectSender
	dnsMiddleware DNSMiddleware // DNS middleware for query interception
}

// ClientTunnelConfig contains client tunnel configuration
type ClientTunnelConfig struct {
	Provider      provider.Provider
	Dialer        wire.Dialer
	Router        *Router
	DNSMiddleware DNSMiddleware // Optional: DNS middleware for query interception
}

// NewClientTunnel creates a new client tunnel
func NewClientTunnel(cfg ClientTunnelConfig) (*ClientTunnel, error) {
	// Create direct sender (optional, may fail without root)
	direct, err := NewDirectSender()
	if err != nil {
		log.Printf("Warning: direct sender not available: %v", err)
		direct = nil
	}

	return &ClientTunnel{
		provider:      cfg.Provider,
		dialer:        cfg.Dialer,
		router:        cfg.Router,
		direct:        direct,
		dnsMiddleware: cfg.DNSMiddleware,
	}, nil
}

// Start starts the client tunnel
func (t *ClientTunnel) Start(ctx context.Context) error {
	log.Println("Client tunnel starting...")

	// Establish connection to server
	conn, err := t.dialer.Dial(ctx)
	if err != nil {
		return fmt.Errorf("dial server: %w", err)
	}
	defer conn.Close()

	log.Println("Connected to server")

	// Create error channel for goroutines
	errCh := make(chan error, 2)

	// Provider -> Wire (upload)
	go func() {
		errCh <- t.uploadLoop(conn)
	}()

	// Wire -> Provider (download)
	go func() {
		errCh <- t.downloadLoop(conn)
	}()

	// Wait for context cancellation or error
	select {
	case <-ctx.Done():
		log.Println("Client tunnel stopped by context")
		return ctx.Err()
	case err := <-errCh:
		log.Printf("Client tunnel stopped by error: %v", err)
		return err
	}
}

// uploadLoop reads packets from provider and sends them through wire
func (t *ClientTunnel) uploadLoop(conn net.Conn) error {
	buf := make([]byte, 2048)

	for {
		// Read packet from TUN device
		n, err := t.provider.Read(buf)
		if err != nil {
			return fmt.Errorf("read from provider: %w", err)
		}

		pkt := buf[:n]

		// Parse packet info for routing
		info, err := packet.Parse(pkt)
		if err != nil {
			log.Printf("Warning: failed to parse packet: %v", err)
			continue
		}

		// Intercept DNS queries (UDP port 53)
		if t.dnsMiddleware != nil && info.Protocol == "udp" && info.DstPort == 53 {
			if err := t.dnsMiddleware.HandleDNSQuery(pkt, info); err != nil {
				log.Printf("Warning: DNS middleware failed: %v", err)
			}
			continue
		}

		// Make routing decision
		action := t.router.Route(info)

		switch action {
		case k2rule.TargetProxy:
			// Send through tunnel
			if err := WritePacket(conn, pkt); err != nil {
				return fmt.Errorf("write packet to wire: %w", err)
			}

		case k2rule.TargetDirect:
			// Send directly (bypass tunnel)
			if t.direct != nil {
				if err := t.direct.Send(pkt); err != nil {
					log.Printf("Warning: direct send failed: %v", err)
				}
			}

		case k2rule.TargetReject:
			// Drop the packet
			log.Printf("Packet rejected: %s:%d -> %s:%d", info.SrcIP, info.SrcPort, info.DstIP, info.DstPort)
		}
	}
}

// downloadLoop reads packets from wire and writes them to provider
func (t *ClientTunnel) downloadLoop(conn net.Conn) error {
	for {
		// Read packet from wire
		pkt, err := ReadPacket(conn)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("read packet from wire: %w", err)
		}

		// Write packet to TUN device
		if _, err := t.provider.Write(pkt); err != nil {
			return fmt.Errorf("write to provider: %w", err)
		}
	}
}

// Close closes the client tunnel
func (t *ClientTunnel) Close() error {
	if t.direct != nil {
		t.direct.Close()
	}
	return t.provider.Close()
}

// ServerTunnel implements the server-side tunnel
type ServerTunnel struct {
	listener    wire.Listener
	serverTUN   provider.Provider
	maxClients  int
}

// ServerTunnelConfig contains server tunnel configuration
type ServerTunnelConfig struct {
	Listener   wire.Listener
	ServerTUN  provider.Provider
	MaxClients int
}

// NewServerTunnel creates a new server tunnel
func NewServerTunnel(cfg ServerTunnelConfig) *ServerTunnel {
	if cfg.MaxClients == 0 {
		cfg.MaxClients = 100
	}

	return &ServerTunnel{
		listener:   cfg.Listener,
		serverTUN:  cfg.ServerTUN,
		maxClients: cfg.MaxClients,
	}
}

// Start starts the server tunnel
func (t *ServerTunnel) Start(ctx context.Context) error {
	log.Println("Server tunnel starting...")

	// Accept loop
	for {
		conn, err := t.listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				log.Printf("Accept error: %v", err)
				continue
			}
		}

		log.Printf("New client connected: %s", conn.RemoteAddr())

		// Handle client connection in goroutine
		go t.handleClient(ctx, conn)
	}
}

// handleClient handles a single client connection
func (t *ServerTunnel) handleClient(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	errCh := make(chan error, 2)

	// Client -> TUN (download from client perspective)
	go func() {
		errCh <- t.clientToTUN(conn)
	}()

	// TUN -> Client (upload from client perspective)
	go func() {
		errCh <- t.tunToClient(conn)
	}()

	// Wait for error or context cancellation
	select {
	case <-ctx.Done():
		log.Println("Client handler stopped by context")
	case err := <-errCh:
		if err != nil && err != io.EOF {
			log.Printf("Client handler error: %v", err)
		}
	}
}

// clientToTUN reads packets from client and writes to TUN
func (t *ServerTunnel) clientToTUN(conn net.Conn) error {
	for {
		packet, err := ReadPacket(conn)
		if err != nil {
			return err
		}

		if _, err := t.serverTUN.Write(packet); err != nil {
			return fmt.Errorf("write to TUN: %w", err)
		}
	}
}

// tunToClient reads packets from TUN and writes to client
func (t *ServerTunnel) tunToClient(conn net.Conn) error {
	buf := make([]byte, 2048)

	for {
		n, err := t.serverTUN.Read(buf)
		if err != nil {
			return fmt.Errorf("read from TUN: %w", err)
		}

		packet := buf[:n]

		if err := WritePacket(conn, packet); err != nil {
			return fmt.Errorf("write packet to client: %w", err)
		}
	}
}

// Close closes the server tunnel
func (t *ServerTunnel) Close() error {
	t.listener.Close()
	return t.serverTUN.Close()
}

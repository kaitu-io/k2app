package provider

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
)

// ProxyProvider implements Provider as a local SOCKS5/HTTP mixed proxy server.
// No root/admin privileges required. Applications must be configured to use the proxy.
type ProxyProvider struct {
	cfg      ProviderConfig
	listener net.Listener
	done     chan struct{}
}

var _ Provider = (*ProxyProvider)(nil)

// NewProxyProvider creates a proxy-based provider.
func NewProxyProvider(cfg ProviderConfig) *ProxyProvider {
	cfg.SetDefaults()
	return &ProxyProvider{cfg: cfg, done: make(chan struct{})}
}

func (p *ProxyProvider) Start(ctx context.Context, handler ConnectionHandler) error {
	ln, err := net.Listen("tcp", p.cfg.ProxyListen)
	if err != nil {
		return fmt.Errorf("provider: listen %s: %w", p.cfg.ProxyListen, err)
	}
	p.listener = ln

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				select {
				case <-p.done:
					return
				default:
					continue
				}
			}
			go p.handleConn(conn, handler)
		}
	}()

	return nil
}

func (p *ProxyProvider) Close() error {
	select {
	case <-p.done:
		return nil
	default:
		close(p.done)
	}
	if p.listener != nil {
		return p.listener.Close()
	}
	return nil
}

func (p *ProxyProvider) Name() string { return "proxy" }

func (p *ProxyProvider) handleConn(conn net.Conn, handler ConnectionHandler) {
	br := bufio.NewReader(conn)
	first, err := br.Peek(1)
	if err != nil {
		conn.Close()
		return
	}

	// SOCKS5 starts with 0x05, HTTP starts with ASCII letter.
	if first[0] == 0x05 {
		p.handleSOCKS5(conn, br, handler)
	} else {
		p.handleHTTP(conn, br, handler)
	}
}

// handleSOCKS5 handles SOCKS5 CONNECT requests (RFC 1928).
func (p *ProxyProvider) handleSOCKS5(conn net.Conn, br *bufio.Reader, handler ConnectionHandler) {
	// Auth negotiation: version + nmethods + methods
	ver, err := br.ReadByte()
	if err != nil || ver != 0x05 {
		conn.Close()
		return
	}
	nmethods, err := br.ReadByte()
	if err != nil {
		conn.Close()
		return
	}
	methods := make([]byte, nmethods)
	if _, err := io.ReadFull(br, methods); err != nil {
		conn.Close()
		return
	}

	// Reply: no auth required
	conn.Write([]byte{0x05, 0x00})

	// Request: VER CMD RSV ATYP DST.ADDR DST.PORT
	header := make([]byte, 4)
	if _, err := io.ReadFull(br, header); err != nil {
		conn.Close()
		return
	}
	if header[0] != 0x05 {
		conn.Close()
		return
	}

	cmd := header[1]
	atyp := header[3]

	var host string
	switch atyp {
	case 0x01: // IPv4
		ipBytes := make([]byte, 4)
		if _, err := io.ReadFull(br, ipBytes); err != nil {
			conn.Close()
			return
		}
		host = net.IP(ipBytes).String()
	case 0x03: // Domain
		dLen, err := br.ReadByte()
		if err != nil {
			conn.Close()
			return
		}
		domain := make([]byte, dLen)
		if _, err := io.ReadFull(br, domain); err != nil {
			conn.Close()
			return
		}
		host = string(domain)
	case 0x04: // IPv6
		ipBytes := make([]byte, 16)
		if _, err := io.ReadFull(br, ipBytes); err != nil {
			conn.Close()
			return
		}
		host = net.IP(ipBytes).String()
	default:
		conn.Write([]byte{0x05, 0x08, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		conn.Close()
		return
	}

	portBytes := make([]byte, 2)
	if _, err := io.ReadFull(br, portBytes); err != nil {
		conn.Close()
		return
	}
	port := int(portBytes[0])<<8 | int(portBytes[1])
	dest := net.JoinHostPort(host, strconv.Itoa(port))

	switch cmd {
	case 0x01: // CONNECT
		// Success reply
		conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		// Wrap with buffered reader to handle any remaining data
		handler.HandleTCP(&bufferedConn{Conn: conn, br: br}, dest)
	default:
		// Command not supported
		conn.Write([]byte{0x05, 0x07, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		conn.Close()
	}
}

// handleHTTP handles HTTP CONNECT proxy requests.
func (p *ProxyProvider) handleHTTP(conn net.Conn, br *bufio.Reader, handler ConnectionHandler) {
	req, err := http.ReadRequest(br)
	if err != nil {
		conn.Close()
		return
	}

	if req.Method != http.MethodConnect {
		resp := &http.Response{
			StatusCode: http.StatusMethodNotAllowed,
			ProtoMajor: 1,
			ProtoMinor: 1,
			Header:     make(http.Header),
		}
		resp.Write(conn)
		conn.Close()
		return
	}

	dest := req.Host
	if !strings.Contains(dest, ":") {
		dest = dest + ":443"
	}

	// 200 Connection Established
	conn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
	handler.HandleTCP(&bufferedConn{Conn: conn, br: br}, dest)
}

// bufferedConn wraps net.Conn with a buffered reader for residual data after proxy handshake.
type bufferedConn struct {
	net.Conn
	br *bufio.Reader
}

func (c *bufferedConn) Read(b []byte) (int, error) {
	return c.br.Read(b)
}

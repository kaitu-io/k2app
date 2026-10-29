package provider

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
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
			go func(c net.Conn) {
				defer func() {
					if r := recover(); r != nil {
						slog.Error("proxy: handleConn panic", "panic", fmt.Sprint(r))
						c.Close()
					}
				}()
				p.handleConn(c, handler)
			}(conn)
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
	case 0x03: // UDP ASSOCIATE
		p.handleUDPAssociate(conn, handler)
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

// ============================================================================
// SOCKS5 UDP ASSOCIATE (RFC 1928 Section 7)
// ============================================================================

// handleUDPAssociate handles SOCKS5 UDP ASSOCIATE (CMD=0x03).
// Opens a UDP relay, replies with its address, then blocks until the
// TCP control connection closes (per RFC 1928).
func (p *ProxyProvider) handleUDPAssociate(conn net.Conn, handler ConnectionHandler) {
	// Listen on same IP as TCP listener, random port.
	tcpAddr := p.listener.Addr().(*net.TCPAddr)
	udpConn, err := net.ListenPacket("udp", net.JoinHostPort(tcpAddr.IP.String(), "0"))
	if err != nil {
		conn.Write([]byte{0x05, 0x01, 0x00, 0x01, 0, 0, 0, 0, 0, 0}) // general failure
		conn.Close()
		return
	}

	// Reply with relay's bound address.
	udpAddr := udpConn.LocalAddr().(*net.UDPAddr)
	conn.Write(buildSOCKS5Reply(udpAddr))

	// Create and start relay.
	relay := &udpRelay{
		udpConn:  udpConn,
		handler:  handler,
		sessions: make(map[string]*udpSession),
		done:     make(chan struct{}),
	}
	go relay.run()

	// Block until TCP control connection closes.
	buf := make([]byte, 1)
	for {
		if _, err := conn.Read(buf); err != nil {
			break
		}
	}

	relay.close()
	conn.Close()
}

// buildSOCKS5Reply constructs the SOCKS5 reply for UDP ASSOCIATE with the relay's bound address.
func buildSOCKS5Reply(addr *net.UDPAddr) []byte {
	// VER=5 REP=0(success) RSV=0 ATYP BND.ADDR BND.PORT
	ip4 := addr.IP.To4()
	if ip4 != nil {
		reply := make([]byte, 10)
		reply[0] = 0x05
		reply[3] = 0x01 // IPv4
		copy(reply[4:8], ip4)
		binary.BigEndian.PutUint16(reply[8:10], uint16(addr.Port))
		return reply
	}
	reply := make([]byte, 22)
	reply[0] = 0x05
	reply[3] = 0x04 // IPv6
	copy(reply[4:20], addr.IP.To16())
	binary.BigEndian.PutUint16(reply[20:22], uint16(addr.Port))
	return reply
}

// ============================================================================
// SOCKS5 UDP header parse/write (RFC 1928 Section 7)
// Format: [RSV:2][FRAG:1][ATYP:1][DST.ADDR:variable][DST.PORT:2][DATA]
// ============================================================================

func parseSOCKS5UDPHeader(data []byte) (dest string, payload []byte, err error) {
	if len(data) < 4 {
		return "", nil, errors.New("socks5 udp: packet too short")
	}
	if data[2] != 0x00 {
		return "", nil, errors.New("socks5 udp: fragmented packets not supported")
	}

	atyp := data[3]
	var host string
	var offset int

	switch atyp {
	case 0x01: // IPv4
		if len(data) < 10 {
			return "", nil, errors.New("socks5 udp: IPv4 packet too short")
		}
		host = net.IP(data[4:8]).String()
		offset = 10
	case 0x03: // Domain
		if len(data) < 5 {
			return "", nil, errors.New("socks5 udp: domain packet too short")
		}
		dLen := int(data[4])
		end := 5 + dLen + 2
		if len(data) < end {
			return "", nil, errors.New("socks5 udp: domain packet truncated")
		}
		host = string(data[5 : 5+dLen])
		offset = end
	case 0x04: // IPv6
		if len(data) < 22 {
			return "", nil, errors.New("socks5 udp: IPv6 packet too short")
		}
		host = net.IP(data[4:20]).String()
		offset = 22
	default:
		return "", nil, fmt.Errorf("socks5 udp: unsupported ATYP %d", atyp)
	}

	port := int(data[offset-2])<<8 | int(data[offset-1])
	return net.JoinHostPort(host, strconv.Itoa(port)), data[offset:], nil
}

func writeSOCKS5UDPHeader(dest string) []byte {
	host, portStr, _ := net.SplitHostPort(dest)
	port, _ := strconv.Atoi(portStr)

	// RSV(2) + FRAG(1) = 3 bytes of zeros
	var buf []byte

	if ip := net.ParseIP(host); ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			buf = make([]byte, 10)
			buf[3] = 0x01 // ATYP = IPv4
			copy(buf[4:8], ip4)
			binary.BigEndian.PutUint16(buf[8:10], uint16(port))
		} else {
			buf = make([]byte, 22)
			buf[3] = 0x04 // ATYP = IPv6
			copy(buf[4:20], ip.To16())
			binary.BigEndian.PutUint16(buf[20:22], uint16(port))
		}
	} else {
		// Domain
		buf = make([]byte, 4+1+len(host)+2)
		buf[3] = 0x03 // ATYP = Domain
		buf[4] = byte(len(host))
		copy(buf[5:], host)
		binary.BigEndian.PutUint16(buf[5+len(host):], uint16(port))
	}
	return buf
}

// ============================================================================
// UDP Relay — per-TCP-connection relay coordinator
// ============================================================================

type udpRelay struct {
	udpConn   net.PacketConn
	handler   ConnectionHandler
	sessions  map[string]*udpSession // client source addr → session
	mu        sync.Mutex
	done      chan struct{}
	closeOnce sync.Once
}

func (r *udpRelay) run() {
	buf := make([]byte, 4096)
	for {
		n, from, err := r.udpConn.ReadFrom(buf)
		if err != nil {
			select {
			case <-r.done:
				return
			default:
				continue
			}
		}

		dest, payload, err := parseSOCKS5UDPHeader(buf[:n])
		if err != nil {
			continue // drop malformed/fragmented
		}

		// Sessions are keyed by client source address, not destination: a
		// single client source port must map to one egress session that
		// carries many destinations (full-cone parity with TUN mode). dest
		// from this first packet becomes the session's primary destination;
		// later packets on the same session may target other destinations.
		key := from.String()
		r.mu.Lock()
		sess, ok := r.sessions[key]
		if !ok {
			sess = &udpSession{
				relay:      r,
				clientAddr: from,
				dest:       dest,
				readCh:     make(chan udpPacket, 32),
				done:       make(chan struct{}),
			}
			r.sessions[key] = sess
			go r.handler.HandleUDP(sess, dest)
		}
		r.mu.Unlock()

		sess.setClientAddr(from)
		sess.deliver(dest, payload)
	}
}

func (r *udpRelay) close() {
	r.closeOnce.Do(func() {
		close(r.done)
		r.udpConn.Close()
		// concurrency:allow — sess.close() is closeOnce.Do(func(){ close(done) }), pure memory op
		r.mu.Lock()
		for _, sess := range r.sessions {
			sess.close()
		}
		r.mu.Unlock()
	})
}

// ============================================================================
// UDP Session — virtual net.PacketConn per destination
// ============================================================================

// readDeadline is a thread-safe read deadline for channel-based PacketConn types.
// Zero value is ready to use (no deadline set).
// Local copy of wire.readDeadline to avoid cross-layer dependency.
type readDeadline struct {
	mu sync.Mutex
	t  time.Time
}

func (d *readDeadline) set(t time.Time) {
	d.mu.Lock()
	d.t = t
	d.mu.Unlock()
}

func (d *readDeadline) makeTimer() (<-chan time.Time, *time.Timer, error) {
	d.mu.Lock()
	t := d.t
	d.mu.Unlock()
	if t.IsZero() {
		return nil, nil, nil
	}
	dur := time.Until(t)
	if dur <= 0 {
		return nil, nil, os.ErrDeadlineExceeded
	}
	timer := time.NewTimer(dur)
	return timer.C, timer, nil
}

// udpPacket carries one queued uplink payload alongside the destination it
// was addressed to. A session now fans out to many destinations, so the
// destination must travel with the payload rather than being fixed per
// session.
type udpPacket struct {
	dest    string
	payload []byte
}

type udpSession struct {
	relay      *udpRelay
	clientAddr net.Addr
	dest       string
	readCh     chan udpPacket
	done       chan struct{}
	closeOnce  sync.Once
	mu         sync.Mutex // protects clientAddr
	rd         readDeadline
}

var _ net.PacketConn = (*udpSession)(nil)

func (s *udpSession) deliver(dest string, payload []byte) {
	p := make([]byte, len(payload))
	copy(p, payload)
	select {
	case s.readCh <- udpPacket{dest: dest, payload: p}:
	case <-s.done:
	}
}

func (s *udpSession) setClientAddr(addr net.Addr) {
	s.mu.Lock()
	s.clientAddr = addr
	s.mu.Unlock()
}

func (s *udpSession) ReadFrom(b []byte) (int, net.Addr, error) {
	timerCh, timer, expired := s.rd.makeTimer()
	if expired != nil {
		return 0, nil, expired
	}
	if timer != nil {
		defer timer.Stop()
	}
	select {
	case pkt := <-s.readCh:
		n := copy(b, pkt.payload)
		addr, _ := net.ResolveUDPAddr("udp", pkt.dest)
		return n, addr, nil
	case <-s.done:
		return 0, nil, net.ErrClosed
	case <-timerCh:
		return 0, nil, os.ErrDeadlineExceeded
	}
}

// WriteTo sends a downlink reply to the client. addr is the true reply
// source (the peer that just sent this session data) — with one session
// now serving many peers, the client can only tell them apart if the
// SOCKS5 UDP header carries the real source. Fall back to the session's
// primary dest only when addr is nil (defensive; callers should pass it).
func (s *udpSession) WriteTo(b []byte, addr net.Addr) (int, error) {
	replyFrom := s.dest
	if addr != nil {
		replyFrom = addr.String()
	}
	header := writeSOCKS5UDPHeader(replyFrom)
	pkt := make([]byte, len(header)+len(b))
	copy(pkt, header)
	copy(pkt[len(header):], b)

	s.mu.Lock()
	clientAddr := s.clientAddr
	s.mu.Unlock()

	if _, err := s.relay.udpConn.WriteTo(pkt, clientAddr); err != nil {
		return 0, err
	}
	return len(b), nil
}

func (s *udpSession) close() {
	s.closeOnce.Do(func() { close(s.done) })
}

func (s *udpSession) Close() error {
	s.close()
	return nil
}

func (s *udpSession) LocalAddr() net.Addr                    { return s.relay.udpConn.LocalAddr() }
func (s *udpSession) SetDeadline(t time.Time) error          { s.rd.set(t); return nil }
func (s *udpSession) SetReadDeadline(t time.Time) error      { s.rd.set(t); return nil }
func (s *udpSession) SetWriteDeadline(time.Time) error       { return nil }

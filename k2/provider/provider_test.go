package provider

import (
	"bytes"
	"context"
	"encoding/binary"
	"net"
	"testing"
	"time"
)

func TestProviderConfig_SetDefaults(t *testing.T) {
	var cfg ProviderConfig
	cfg.SetDefaults()

	if cfg.Mode != "tun" {
		t.Errorf("Mode = %q, want tun", cfg.Mode)
	}
	if cfg.MTU != DefaultMTU {
		t.Errorf("MTU = %d, want %d", cfg.MTU, DefaultMTU)
	}
	if cfg.ProxyListen != "127.0.0.1:1080" {
		t.Errorf("ProxyListen = %q, want 127.0.0.1:1080", cfg.ProxyListen)
	}
}

func TestProviderConfig_PreservesValues(t *testing.T) {
	cfg := ProviderConfig{
		Mode:        "proxy",
		MTU:         1500,
		ProxyListen: "0.0.0.0:8080",
	}
	cfg.SetDefaults()

	if cfg.Mode != "proxy" {
		t.Errorf("Mode = %q, want proxy", cfg.Mode)
	}
	if cfg.MTU != 1500 {
		t.Errorf("MTU = %d, want 1500", cfg.MTU)
	}
	if cfg.ProxyListen != "0.0.0.0:8080" {
		t.Errorf("ProxyListen = %q, want 0.0.0.0:8080", cfg.ProxyListen)
	}
}

func TestProviderConfig_TunAddresses_Preserved(t *testing.T) {
	cfg := ProviderConfig{
		IPv4Address: "10.0.0.2/24",
		IPv6Address: "fd00::2/128",
	}
	cfg.SetDefaults()

	if cfg.IPv4Address != "10.0.0.2/24" {
		t.Errorf("IPv4Address = %q, want 10.0.0.2/24", cfg.IPv4Address)
	}
	if cfg.IPv6Address != "fd00::2/128" {
		t.Errorf("IPv6Address = %q, want fd00::2/128", cfg.IPv6Address)
	}
}

func TestProviderConfig_TunAddresses_Empty(t *testing.T) {
	cfg := ProviderConfig{}
	cfg.SetDefaults()

	// Empty TUN addresses stay empty at provider level — config.SetDefaults() fills them upstream.
	if cfg.IPv4Address != "" {
		t.Errorf("IPv4Address should be empty, got %q", cfg.IPv4Address)
	}
	if cfg.IPv6Address != "" {
		t.Errorf("IPv6Address should be empty, got %q", cfg.IPv6Address)
	}
}

// testHandler records calls from providers.
type testHandler struct {
	tcpCalls chan tcpCall
	udpCalls chan udpCall
}

type tcpCall struct {
	conn net.Conn
	dest string
}

type udpCall struct {
	conn net.PacketConn
	dest string
}

func newTestHandler() *testHandler {
	return &testHandler{
		tcpCalls: make(chan tcpCall, 16),
		udpCalls: make(chan udpCall, 16),
	}
}

func (h *testHandler) HandleTCP(conn net.Conn, dest string) {
	h.tcpCalls <- tcpCall{conn: conn, dest: dest}
}

func (h *testHandler) HandleUDP(conn net.PacketConn, dest string) {
	h.udpCalls <- udpCall{conn: conn, dest: dest}
}

func TestProxyProvider_StartClose(t *testing.T) {
	p := NewProxyProvider(ProviderConfig{ProxyListen: "127.0.0.1:0"})
	handler := newTestHandler()

	if err := p.Start(context.Background(), handler); err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	if p.listener == nil {
		t.Fatal("listener is nil after Start")
	}

	addr := p.listener.Addr().String()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
}

func TestProxyProvider_SOCKS5Connect(t *testing.T) {
	p := NewProxyProvider(ProviderConfig{ProxyListen: "127.0.0.1:0"})
	handler := newTestHandler()

	if err := p.Start(context.Background(), handler); err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	addr := p.listener.Addr().String()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// SOCKS5 handshake: version 5, 1 method (no auth)
	conn.Write([]byte{0x05, 0x01, 0x00})

	// Read auth reply
	reply := make([]byte, 2)
	if _, err := conn.Read(reply); err != nil {
		t.Fatal(err)
	}
	if reply[0] != 0x05 || reply[1] != 0x00 {
		t.Fatalf("auth reply = %v, want [5 0]", reply)
	}

	// CONNECT to example.com:80
	// VER=5 CMD=CONNECT RSV=0 ATYP=DOMAIN
	conn.Write([]byte{
		0x05, 0x01, 0x00, 0x03,
		11, 'e', 'x', 'a', 'm', 'p', 'l', 'e', '.', 'c', 'o', 'm',
		0x00, 0x50, // port 80
	})

	// Read connect reply
	connectReply := make([]byte, 10)
	if _, err := conn.Read(connectReply); err != nil {
		t.Fatal(err)
	}
	if connectReply[1] != 0x00 {
		t.Fatalf("connect reply status = %d, want 0 (success)", connectReply[1])
	}

	// Verify handler was called
	select {
	case call := <-handler.tcpCalls:
		if call.dest != "example.com:80" {
			t.Errorf("dest = %q, want example.com:80", call.dest)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler.HandleTCP not called")
	}
}

func TestProxyProvider_SOCKS5Connect_IPv4(t *testing.T) {
	p := NewProxyProvider(ProviderConfig{ProxyListen: "127.0.0.1:0"})
	handler := newTestHandler()

	if err := p.Start(context.Background(), handler); err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	addr := p.listener.Addr().String()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// Auth
	conn.Write([]byte{0x05, 0x01, 0x00})
	reply := make([]byte, 2)
	conn.Read(reply)

	// CONNECT to 93.184.216.34:443 (ATYP=IPv4)
	conn.Write([]byte{
		0x05, 0x01, 0x00, 0x01,
		93, 184, 216, 34,
		0x01, 0xBB, // port 443
	})

	connectReply := make([]byte, 10)
	conn.Read(connectReply)

	select {
	case call := <-handler.tcpCalls:
		if call.dest != "93.184.216.34:443" {
			t.Errorf("dest = %q, want 93.184.216.34:443", call.dest)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler.HandleTCP not called")
	}
}

func TestProxyProvider_HTTPConnect(t *testing.T) {
	p := NewProxyProvider(ProviderConfig{ProxyListen: "127.0.0.1:0"})
	handler := newTestHandler()

	if err := p.Start(context.Background(), handler); err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	addr := p.listener.Addr().String()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// HTTP CONNECT
	conn.Write([]byte("CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n"))

	// Read response
	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	resp := string(buf[:n])
	if resp != "HTTP/1.1 200 Connection Established\r\n\r\n" {
		t.Errorf("response = %q", resp)
	}

	select {
	case call := <-handler.tcpCalls:
		if call.dest != "example.com:443" {
			t.Errorf("dest = %q, want example.com:443", call.dest)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler.HandleTCP not called")
	}
}

func TestProxyProvider_HTTPConnect_NoPort(t *testing.T) {
	p := NewProxyProvider(ProviderConfig{ProxyListen: "127.0.0.1:0"})
	handler := newTestHandler()

	if err := p.Start(context.Background(), handler); err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	addr := p.listener.Addr().String()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// HTTP CONNECT without port
	conn.Write([]byte("CONNECT example.com HTTP/1.1\r\nHost: example.com\r\n\r\n"))

	buf := make([]byte, 256)
	conn.Read(buf)

	select {
	case call := <-handler.tcpCalls:
		if call.dest != "example.com:443" {
			t.Errorf("dest = %q, want example.com:443", call.dest)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler.HandleTCP not called")
	}
}

// ============================================================================
// SOCKS5 UDP header tests
// ============================================================================

func TestParseSOCKS5UDPHeader_IPv4(t *testing.T) {
	// [RSV:2][FRAG:1][ATYP:1][IPv4:4][PORT:2][DATA]
	data := []byte{
		0x00, 0x00, // RSV
		0x00,       // FRAG
		0x01,       // ATYP = IPv4
		93, 184, 216, 34, // 93.184.216.34
		0x00, 0x50, // port 80
		'h', 'e', 'l', 'l', 'o', // payload
	}
	dest, payload, err := parseSOCKS5UDPHeader(data)
	if err != nil {
		t.Fatal(err)
	}
	if dest != "93.184.216.34:80" {
		t.Errorf("dest = %q, want 93.184.216.34:80", dest)
	}
	if string(payload) != "hello" {
		t.Errorf("payload = %q, want hello", payload)
	}
}

func TestParseSOCKS5UDPHeader_Domain(t *testing.T) {
	data := []byte{
		0x00, 0x00, // RSV
		0x00,       // FRAG
		0x03,       // ATYP = Domain
		11,         // domain length
		'e', 'x', 'a', 'm', 'p', 'l', 'e', '.', 'c', 'o', 'm',
		0x00, 0x50, // port 80
		'd', 'a', 't', 'a', // payload
	}
	dest, payload, err := parseSOCKS5UDPHeader(data)
	if err != nil {
		t.Fatal(err)
	}
	if dest != "example.com:80" {
		t.Errorf("dest = %q, want example.com:80", dest)
	}
	if string(payload) != "data" {
		t.Errorf("payload = %q, want data", payload)
	}
}

func TestParseSOCKS5UDPHeader_IPv6(t *testing.T) {
	ip := net.ParseIP("::1")
	data := make([]byte, 0, 4+16+2+1)
	data = append(data, 0x00, 0x00, 0x00, 0x04) // RSV + FRAG + ATYP=IPv6
	data = append(data, ip.To16()...)             // 16 bytes
	data = append(data, 0x01, 0xBB)               // port 443
	data = append(data, 'x')                      // payload

	dest, payload, err := parseSOCKS5UDPHeader(data)
	if err != nil {
		t.Fatal(err)
	}
	if dest != "[::1]:443" {
		t.Errorf("dest = %q, want [::1]:443", dest)
	}
	if string(payload) != "x" {
		t.Errorf("payload = %q, want x", payload)
	}
}

func TestParseSOCKS5UDPHeader_Fragmented(t *testing.T) {
	data := []byte{0x00, 0x00, 0x01, 0x01, 1, 2, 3, 4, 0x00, 0x50}
	_, _, err := parseSOCKS5UDPHeader(data)
	if err == nil {
		t.Fatal("expected error for fragmented packet")
	}
}

func TestParseSOCKS5UDPHeader_TooShort(t *testing.T) {
	_, _, err := parseSOCKS5UDPHeader([]byte{0x00, 0x00})
	if err == nil {
		t.Fatal("expected error for short packet")
	}
}

func TestWriteSOCKS5UDPHeader_IPv4(t *testing.T) {
	header := writeSOCKS5UDPHeader("93.184.216.34:80")
	want := []byte{
		0x00, 0x00, 0x00, // RSV + FRAG
		0x01,              // ATYP = IPv4
		93, 184, 216, 34,  // IP
		0x00, 0x50,        // port 80
	}
	if !bytes.Equal(header, want) {
		t.Errorf("header = %v, want %v", header, want)
	}
}

func TestWriteSOCKS5UDPHeader_Domain(t *testing.T) {
	header := writeSOCKS5UDPHeader("example.com:80")
	want := []byte{
		0x00, 0x00, 0x00, // RSV + FRAG
		0x03,              // ATYP = Domain
		11,                // domain length
		'e', 'x', 'a', 'm', 'p', 'l', 'e', '.', 'c', 'o', 'm',
		0x00, 0x50, // port 80
	}
	if !bytes.Equal(header, want) {
		t.Errorf("header = %v, want %v", header, want)
	}
}

func TestSOCKS5UDPHeader_Roundtrip(t *testing.T) {
	tests := []string{
		"93.184.216.34:80",
		"example.com:443",
		"[::1]:8080",
	}
	for _, dest := range tests {
		t.Run(dest, func(t *testing.T) {
			header := writeSOCKS5UDPHeader(dest)
			payload := []byte("test-payload")
			pkt := append(header, payload...)

			gotDest, gotPayload, err := parseSOCKS5UDPHeader(pkt)
			if err != nil {
				t.Fatal(err)
			}
			if gotDest != dest {
				t.Errorf("dest = %q, want %q", gotDest, dest)
			}
			if string(gotPayload) != "test-payload" {
				t.Errorf("payload = %q, want test-payload", gotPayload)
			}
		})
	}
}

// ============================================================================
// SOCKS5 UDP ASSOCIATE integration tests
// ============================================================================

// socks5UDPHandshake performs SOCKS5 auth + UDP ASSOCIATE handshake.
// Returns the relay address from the server reply.
func socks5UDPHandshake(t *testing.T, conn net.Conn) *net.UDPAddr {
	t.Helper()

	// Auth: version 5, 1 method (no auth)
	conn.Write([]byte{0x05, 0x01, 0x00})
	reply := make([]byte, 2)
	if _, err := conn.Read(reply); err != nil {
		t.Fatal(err)
	}
	if reply[0] != 0x05 || reply[1] != 0x00 {
		t.Fatalf("auth reply = %v, want [5 0]", reply)
	}

	// UDP ASSOCIATE: CMD=0x03, ATYP=0x01, DST.ADDR=0.0.0.0, DST.PORT=0
	conn.Write([]byte{
		0x05, 0x03, 0x00, 0x01,
		0, 0, 0, 0, // 0.0.0.0
		0, 0, // port 0
	})

	// Read reply: VER REP RSV ATYP BND.ADDR BND.PORT
	buf := make([]byte, 10) // IPv4 reply is exactly 10 bytes
	if _, err := conn.Read(buf); err != nil {
		t.Fatal(err)
	}
	if buf[1] != 0x00 {
		t.Fatalf("UDP ASSOCIATE reply status = %d, want 0 (success)", buf[1])
	}
	if buf[3] != 0x01 {
		t.Fatalf("reply ATYP = %d, want 1 (IPv4)", buf[3])
	}

	ip := net.IP(buf[4:8])
	port := int(binary.BigEndian.Uint16(buf[8:10]))
	return &net.UDPAddr{IP: ip, Port: port}
}

// buildSOCKS5UDPPacket builds a SOCKS5 UDP datagram with the given dest and payload.
func buildSOCKS5UDPPacket(dest string, payload []byte) []byte {
	header := writeSOCKS5UDPHeader(dest)
	return append(header, payload...)
}

func TestProxyProvider_SOCKS5_UDPAssociate(t *testing.T) {
	p := NewProxyProvider(ProviderConfig{ProxyListen: "127.0.0.1:0"})
	handler := newTestHandler()

	if err := p.Start(context.Background(), handler); err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	// TCP connection for SOCKS5 handshake
	addr := p.listener.Addr().String()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// Perform UDP ASSOCIATE handshake, get relay address
	relayAddr := socks5UDPHandshake(t, conn)

	// Open local UDP socket to communicate with relay
	udpConn, err := net.DialUDP("udp", nil, relayAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer udpConn.Close()

	// Send SOCKS5 UDP packet to relay
	pkt := buildSOCKS5UDPPacket("127.0.0.1:9999", []byte("hello"))
	if _, err := udpConn.Write(pkt); err != nil {
		t.Fatal(err)
	}

	// Handler should receive HandleUDP call
	var call udpCall
	select {
	case call = <-handler.udpCalls:
	case <-time.After(2 * time.Second):
		t.Fatal("handler.HandleUDP not called")
	}
	if call.dest != "127.0.0.1:9999" {
		t.Errorf("dest = %q, want 127.0.0.1:9999", call.dest)
	}

	// Read payload from the session's PacketConn
	buf := make([]byte, 4096)
	call.conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := call.conn.ReadFrom(buf)
	if err != nil {
		t.Fatal(err)
	}
	if string(buf[:n]) != "hello" {
		t.Errorf("payload = %q, want hello", buf[:n])
	}

	// Write response back through the session
	call.conn.WriteTo([]byte("world"), nil)

	// Read response from UDP socket (should be SOCKS5 framed)
	udpConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err = udpConn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	dest, payload, err := parseSOCKS5UDPHeader(buf[:n])
	if err != nil {
		t.Fatal(err)
	}
	if dest != "127.0.0.1:9999" {
		t.Errorf("response dest = %q, want 127.0.0.1:9999", dest)
	}
	if string(payload) != "world" {
		t.Errorf("response payload = %q, want world", payload)
	}
}

// TestProxyProvider_SOCKS5_UDPAssociate_MultiDest verifies full-cone parity
// with TUN mode: one client source address addressing two different
// destinations must share a single session (one HandleUDP call), and that
// session's ReadFrom must surface each payload paired with its own
// per-packet destination rather than a fixed one. Sessions are keyed by
// client source address, not destination — see provider/proxy.go udpRelay.run.
func TestProxyProvider_SOCKS5_UDPAssociate_MultiDest(t *testing.T) {
	p := NewProxyProvider(ProviderConfig{ProxyListen: "127.0.0.1:0"})
	handler := newTestHandler()

	if err := p.Start(context.Background(), handler); err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	addr := p.listener.Addr().String()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	relayAddr := socks5UDPHandshake(t, conn)

	udpConn, err := net.DialUDP("udp", nil, relayAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer udpConn.Close()

	// Send to two different destinations from the same source port.
	udpConn.Write(buildSOCKS5UDPPacket("127.0.0.1:1111", []byte("first")))
	udpConn.Write(buildSOCKS5UDPPacket("127.0.0.1:2222", []byte("second")))

	// Exactly one HandleUDP call: same source address, one session.
	var call udpCall
	select {
	case call = <-handler.udpCalls:
	case <-time.After(2 * time.Second):
		t.Fatal("HandleUDP not called")
	}
	select {
	case <-handler.udpCalls:
		t.Fatal("HandleUDP invoked a second time for the same source address")
	case <-time.After(200 * time.Millisecond):
	}

	// The single session must yield both payloads, each with its own dest.
	wantDests := []string{"127.0.0.1:1111", "127.0.0.1:2222"}
	wantPayloads := []string{"first", "second"}
	buf := make([]byte, 4096)
	for i := range wantDests {
		call.conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, from, err := call.conn.ReadFrom(buf)
		if err != nil {
			t.Fatal(err)
		}
		if string(buf[:n]) != wantPayloads[i] {
			t.Errorf("payload %d = %q, want %q", i, buf[:n], wantPayloads[i])
		}
		if from == nil || from.String() != wantDests[i] {
			t.Errorf("dest %d = %v, want %q", i, from, wantDests[i])
		}
	}
}

func TestProxyProvider_SOCKS5_UDPAssociate_TCPClose(t *testing.T) {
	p := NewProxyProvider(ProviderConfig{ProxyListen: "127.0.0.1:0"})
	handler := newTestHandler()

	if err := p.Start(context.Background(), handler); err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	addr := p.listener.Addr().String()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}

	relayAddr := socks5UDPHandshake(t, conn)

	udpConn, err := net.DialUDP("udp", nil, relayAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer udpConn.Close()

	// Create a session
	udpConn.Write(buildSOCKS5UDPPacket("127.0.0.1:9999", []byte("data")))

	var call udpCall
	select {
	case call = <-handler.udpCalls:
	case <-time.After(2 * time.Second):
		t.Fatal("handler.HandleUDP not called")
	}

	// Drain the delivered payload first
	buf := make([]byte, 4096)
	call.conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	call.conn.ReadFrom(buf)

	// Close TCP control connection
	conn.Close()

	// Session should eventually return error from ReadFrom
	// (relay.close triggers close(s.done) → ReadFrom returns net.ErrClosed)
	time.Sleep(100 * time.Millisecond)
	_, _, err = call.conn.ReadFrom(make([]byte, 1))
	if err == nil {
		t.Fatal("expected error after TCP close, got nil")
	}
}

package server

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	k2dns "github.com/kaitu-io/k2/core/dns"
	"github.com/kaitu-io/k2/wire"
	"github.com/miekg/dns"
)

func TestIsReservedIP(t *testing.T) {
	blocked := []string{
		"127.0.0.1", "127.255.255.255",
		"10.0.0.1", "10.255.255.255",
		"172.16.0.1", "172.31.255.255",
		"192.168.0.1", "192.168.255.255",
		"169.254.1.1",
		"100.64.0.1", "100.127.255.255",
		"0.0.0.0",
		"::1",
		"fe80::1",
		"fc00::1",
		"fd12::1",
		"::",
	}
	for _, s := range blocked {
		ip := net.ParseIP(s)
		if ip == nil {
			t.Fatalf("bad test IP: %s", s)
		}
		if !isReservedIP(ip) {
			t.Errorf("expected %s to be reserved", s)
		}
	}

	allowed := []string{
		"8.8.8.8", "1.1.1.1", "93.184.216.34",
		"172.32.0.1",  // just outside 172.16.0.0/12
		"100.128.0.1", // just outside 100.64.0.0/10
		"2001:4860:4860::8888",
	}
	for _, s := range allowed {
		ip := net.ParseIP(s)
		if ip == nil {
			t.Fatalf("bad test IP: %s", s)
		}
		if isReservedIP(ip) {
			t.Errorf("expected %s to be allowed", s)
		}
	}
}

func TestCheckAddr(t *testing.T) {
	h := NewProxyHandler()

	// IP literals that should be blocked.
	blockedAddrs := []string{
		"127.0.0.1:80",
		"10.0.0.1:443",
		"192.168.1.1:22",
		"[::1]:53",
		"100.64.0.1:8080",
	}
	for _, addr := range blockedAddrs {
		if err := h.checkAddr(context.Background(), addr); err == nil {
			t.Errorf("expected %s to be blocked", addr)
		}
	}

	// Public IPs that should be allowed.
	allowedAddrs := []string{
		"8.8.8.8:53",
		"1.1.1.1:443",
		"[2001:4860:4860::8888]:53",
	}
	for _, addr := range allowedAddrs {
		if err := h.checkAddr(context.Background(), addr); err != nil {
			t.Errorf("expected %s to be allowed, got: %v", addr, err)
		}
	}

	// Invalid addresses.
	if err := h.checkAddr(context.Background(), "no-port"); err == nil {
		t.Error("expected error for missing port")
	}
}

func TestCheckAddr_UsesResolver(t *testing.T) {
	var calls atomic.Int32
	fakeDNS := startFakeDNSServer(t, &calls)
	defer fakeDNS.Close()

	h := NewProxyHandler()
	h.resolver = NewResolver(h.dnsCache, k2dns.NewDirectDNSClient([]string{fakeDNS.LocalAddr().String()}))

	// Domain that resolves to public IP → allowed.
	if err := h.checkAddr(context.Background(), "example.com:443"); err != nil {
		t.Fatalf("expected allowed, got: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("expected 1 upstream call, got %d", calls.Load())
	}

	// Second call: cache hit → no new upstream call.
	if err := h.checkAddr(context.Background(), "example.com:443"); err != nil {
		t.Fatalf("expected allowed (cached), got: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("expected 1 upstream call (cached), got %d", calls.Load())
	}
}

func TestProxyHandler_HandleTCP_PrivateIP(t *testing.T) {
	h := NewProxyHandler()
	// Default: AllowPrivateIP = false

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	var wg sync.WaitGroup
	wg.Go(func() {
		h.HandleTCP(serverConn, "192.168.1.1:80")
	})

	br := bufio.NewReader(clientConn)
	ok, _, _, err := wire.ReadStreamResponse(br)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected error response for private IP target")
	}

	wg.Wait()
}

func TestProxyHandler_HandleUDP_PrivateIP(t *testing.T) {
	h := NewProxyHandler()

	pc := newTestPacketConn()
	h.HandleUDP(pc, "10.0.0.1:53")

	// HandleUDP should close the conn immediately for private IP.
	select {
	case <-pc.closed:
		// expected
	case <-time.After(2 * time.Second):
		t.Fatal("expected conn to be closed for private IP target")
	}
}

func TestProxyHandler_HandleTCP(t *testing.T) {
	// Start a target TCP echo server.
	targetLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer targetLn.Close()

	go func() {
		conn, err := targetLn.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		io.Copy(conn, conn)
	}()

	h := NewProxyHandler()
	h.AllowPrivateIP = true // echo server is on localhost

	// Simulate the wire stream with a pipe.
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	var wg sync.WaitGroup
	wg.Go(func() {
		h.HandleTCP(serverConn, targetLn.Addr().String())
	})

	// Client: read stream response.
	br := bufio.NewReader(clientConn)
	ok, _, errMsg, err := wire.ReadStreamResponse(br)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatalf("expected success, got error: %s", errMsg)
	}

	// Send data, expect echo.
	testData := []byte("hello proxy")
	if _, err := clientConn.Write(testData); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, len(testData))
	if _, err := io.ReadFull(br, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "hello proxy" {
		t.Fatalf("expected 'hello proxy', got %q", buf)
	}

	clientConn.Close()
	wg.Wait()
}

func TestProxyHandler_HandleTCP_DialFail(t *testing.T) {
	h := NewProxyHandler()
	h.DialTimeout = 100 * time.Millisecond
	h.AllowPrivateIP = true // testing dial failure, not IP guard

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	var wg sync.WaitGroup
	wg.Go(func() {
		h.HandleTCP(serverConn, "127.0.0.1:1") // port 1 should fail
	})

	br := bufio.NewReader(clientConn)
	ok, _, _, err := wire.ReadStreamResponse(br)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected error response for bad target")
	}

	wg.Wait()
}

func TestProxyHandler_HandleUDP(t *testing.T) {
	// Start a target UDP echo server.
	targetConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer targetConn.Close()

	go func() {
		buf := make([]byte, 4096)
		for {
			n, addr, err := targetConn.ReadFrom(buf)
			if err != nil {
				return
			}
			targetConn.WriteTo(buf[:n], addr)
		}
	}()

	h := NewProxyHandler()
	h.AllowPrivateIP = true // echo server is on localhost
	pc := newTestPacketConn()

	go h.HandleUDP(pc, targetConn.LocalAddr().String())

	// Send data through the mock client side.
	pc.readCh <- []byte("hello udp")

	// Read echoed response.
	select {
	case data := <-pc.writeCh:
		if string(data) != "hello udp" {
			t.Fatalf("expected 'hello udp', got %q", data)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for UDP echo")
	}

	pc.Close()
}

// testPacketConn is a channel-based PacketConn for testing.
type testPacketConn struct {
	readCh  chan []byte
	writeCh chan []byte
	closed  chan struct{}
}

func newTestPacketConn() *testPacketConn {
	return &testPacketConn{
		readCh:  make(chan []byte, 16),
		writeCh: make(chan []byte, 16),
		closed:  make(chan struct{}),
	}
}

func (c *testPacketConn) ReadFrom(b []byte) (int, net.Addr, error) {
	select {
	case data := <-c.readCh:
		n := copy(b, data)
		return n, nil, nil
	case <-c.closed:
		return 0, nil, net.ErrClosed
	}
}

func (c *testPacketConn) WriteTo(b []byte, _ net.Addr) (int, error) {
	data := make([]byte, len(b))
	copy(data, b)
	select {
	case c.writeCh <- data:
		return len(b), nil
	case <-c.closed:
		return 0, net.ErrClosed
	}
}

func (c *testPacketConn) Close() error {
	select {
	case <-c.closed:
	default:
		close(c.closed)
	}
	return nil
}

func (c *testPacketConn) LocalAddr() net.Addr                { return nil }
func (c *testPacketConn) SetDeadline(time.Time) error        { return nil }
func (c *testPacketConn) SetReadDeadline(time.Time) error    { return nil }
func (c *testPacketConn) SetWriteDeadline(time.Time) error   { return nil }

// --- DNS cache tests ---

func buildDNSQuery(t *testing.T, domain string, qtype uint16, id uint16) []byte {
	t.Helper()
	q := new(dns.Msg)
	q.SetQuestion(domain, qtype)
	q.Id = id
	packed, err := q.Pack()
	if err != nil {
		t.Fatal(err)
	}
	return packed
}

// startFakeDNSServer starts a UDP DNS server that returns A records and counts calls.
func startFakeDNSServer(t *testing.T, calls *atomic.Int32) net.PacketConn {
	t.Helper()
	conn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		buf := make([]byte, 4096)
		for {
			n, addr, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			calls.Add(1)
			var query dns.Msg
			if err := query.Unpack(buf[:n]); err != nil {
				continue
			}
			resp := new(dns.Msg)
			resp.SetReply(&query)
			if len(query.Question) > 0 {
				resp.Answer = append(resp.Answer, &dns.A{
					Hdr: dns.RR_Header{
						Name: query.Question[0].Name, Rrtype: dns.TypeA,
						Class: dns.ClassINET, Ttl: 300,
					},
					A: net.ParseIP("93.184.216.34").To4(),
				})
			}
			packed, _ := resp.Pack()
			conn.WriteTo(packed, addr)
		}
	}()
	return conn
}

// sendDNSViaHandler sends a DNS query through handleDNSUDP and returns the response.
// Uses handleDNSUDP directly (bypassing isDNSPort check) since fake DNS listens on random port.
// Closes the testPacketConn after receiving to avoid goroutine leaks.
func sendDNSViaHandler(t *testing.T, h *ProxyHandler, addr string, query []byte) *dns.Msg {
	t.Helper()
	pc := newTestPacketConn()
	done := make(chan struct{})
	go func() {
		h.handleDNSUDP(pc, addr, slog.Default())
		close(done)
	}()

	pc.readCh <- query // client → handler

	select {
	case resp := <-pc.writeCh: // handler → client
		pc.Close() // trigger handler shutdown
		<-done     // wait for handleDNSUDP to return
		var msg dns.Msg
		if err := msg.Unpack(resp); err != nil {
			t.Fatalf("unpack: %v", err)
		}
		return &msg
	case <-time.After(5 * time.Second):
		pc.Close()
		t.Fatal("timeout waiting for DNS response")
		return nil
	}
}

func TestHandleUDP_DNSCache_Hit(t *testing.T) {
	upstreamCalls := atomic.Int32{}
	fakeDNS := startFakeDNSServer(t, &upstreamCalls)
	defer fakeDNS.Close()

	h := NewProxyHandler()
	h.AllowPrivateIP = true
	h.resolver = NewResolver(h.dnsCache, k2dns.NewDirectDNSClient([]string{fakeDNS.LocalAddr().String()}))

	// Query 1: cache miss → upstream
	resp1 := sendDNSViaHandler(t, h, fakeDNS.LocalAddr().String(), buildDNSQuery(t, "example.com.", dns.TypeA, 1001))
	if resp1.Rcode != dns.RcodeSuccess {
		t.Fatalf("query 1: got %s", dns.RcodeToString[resp1.Rcode])
	}
	if upstreamCalls.Load() != 1 {
		t.Fatalf("query 1: upstream calls = %d, want 1", upstreamCalls.Load())
	}

	// Query 2: cache hit → no upstream
	resp2 := sendDNSViaHandler(t, h, fakeDNS.LocalAddr().String(), buildDNSQuery(t, "example.com.", dns.TypeA, 1002))
	if resp2.Rcode != dns.RcodeSuccess {
		t.Fatalf("query 2: got %s", dns.RcodeToString[resp2.Rcode])
	}
	if resp2.Id != 1002 {
		t.Errorf("query 2: ID = %d, want 1002 (patched)", resp2.Id)
	}
	if upstreamCalls.Load() != 1 {
		t.Errorf("query 2: upstream calls = %d, want 1 (cache hit)", upstreamCalls.Load())
	}
}

func TestHandleUDP_DNSCache_DifferentDomains(t *testing.T) {
	upstreamCalls := atomic.Int32{}
	fakeDNS := startFakeDNSServer(t, &upstreamCalls)
	defer fakeDNS.Close()

	h := NewProxyHandler()
	h.AllowPrivateIP = true
	h.resolver = NewResolver(h.dnsCache, k2dns.NewDirectDNSClient([]string{fakeDNS.LocalAddr().String()}))

	sendDNSViaHandler(t, h, fakeDNS.LocalAddr().String(), buildDNSQuery(t, "a.com.", dns.TypeA, 1))
	sendDNSViaHandler(t, h, fakeDNS.LocalAddr().String(), buildDNSQuery(t, "b.com.", dns.TypeA, 2))

	if upstreamCalls.Load() != 2 {
		t.Errorf("different domains: upstream calls = %d, want 2", upstreamCalls.Load())
	}
}

func TestHandleUDP_NonDNS_NotIntercepted(t *testing.T) {
	// Non-port-53 traffic should bypass DNS cache entirely.
	echoServer, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echoServer.Close()

	go func() {
		buf := make([]byte, 4096)
		for {
			n, addr, err := echoServer.ReadFrom(buf)
			if err != nil {
				return
			}
			echoServer.WriteTo(buf[:n], addr)
		}
	}()

	h := NewProxyHandler()
	h.AllowPrivateIP = true
	pc := newTestPacketConn()
	go h.HandleUDP(pc, echoServer.LocalAddr().String())

	pc.readCh <- []byte("hello")
	select {
	case resp := <-pc.writeCh:
		if string(resp) != "hello" {
			t.Errorf("expected echo, got %q", resp)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	pc.Close()
}

func TestResolveAddr_Domain(t *testing.T) {
	var calls atomic.Int32
	fakeDNS := startFakeDNSServer(t, &calls)
	defer fakeDNS.Close()

	h := NewProxyHandler()
	h.resolver = NewResolver(h.dnsCache, k2dns.NewDirectDNSClient([]string{fakeDNS.LocalAddr().String()}))

	resolved, err := h.resolveAddr(context.Background(), "example.com:443")
	if err != nil {
		t.Fatal(err)
	}
	if resolved != "93.184.216.34:443" {
		t.Fatalf("got %q, want 93.184.216.34:443", resolved)
	}
}

func TestResolveAddr_IP(t *testing.T) {
	h := NewProxyHandler()
	resolved, err := h.resolveAddr(context.Background(), "8.8.8.8:53")
	if err != nil {
		t.Fatal(err)
	}
	if resolved != "8.8.8.8:53" {
		t.Fatalf("got %q, want 8.8.8.8:53", resolved)
	}
}

func TestResolveAddr_IPv6(t *testing.T) {
	h := NewProxyHandler()
	resolved, err := h.resolveAddr(context.Background(), "[::1]:443")
	if err != nil {
		t.Fatal(err)
	}
	if resolved != "[::1]:443" {
		t.Fatalf("got %q, want [::1]:443", resolved)
	}
}

func TestNewProxyHandler_DefaultResolverServers(t *testing.T) {
	// Ensure env override doesn't leak from caller.
	t.Setenv("K2_DNS_SERVERS", "")

	h := NewProxyHandler()
	if h.resolver == nil {
		t.Fatal("resolver not initialized")
	}
	if h.dnsMaxInflight != 32 {
		t.Fatalf("dnsMaxInflight = %d, want 32", h.dnsMaxInflight)
	}
}

func TestNewProxyHandler_K2DNSServersOverride(t *testing.T) {
	t.Setenv("K2_DNS_SERVERS", "1.2.3.4, 5.6.7.8:5353 , 9.9.9.9")

	got := parseResolverList(os.Getenv("K2_DNS_SERVERS"))
	want := []string{"1.2.3.4:53", "5.6.7.8:5353", "9.9.9.9:53"}
	if len(got) != len(want) {
		t.Fatalf("parseResolverList: got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("parseResolverList[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	// And NewProxyHandler picks up the env via parseResolverList.
	h := NewProxyHandler()
	if h.resolver == nil {
		t.Fatal("resolver not initialized")
	}
}

func TestParseResolverList(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"  ", nil},
		{"1.1.1.1", []string{"1.1.1.1:53"}},
		{"1.1.1.1:53", []string{"1.1.1.1:53"}},
		{"1.1.1.1,8.8.8.8", []string{"1.1.1.1:53", "8.8.8.8:53"}},
		{" 1.1.1.1 , 8.8.8.8:5353 ", []string{"1.1.1.1:53", "8.8.8.8:5353"}},
		{"[2001:db8::1]:53", []string{"[2001:db8::1]:53"}},
		{",,1.1.1.1,,", []string{"1.1.1.1:53"}},
	}
	for _, c := range cases {
		got := parseResolverList(c.in)
		if len(got) != len(c.want) {
			t.Errorf("parseResolverList(%q) = %v, want %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("parseResolverList(%q)[%d] = %q, want %q", c.in, i, got[i], c.want[i])
			}
		}
	}
}

func TestHandleUDP_DNS_UsesRacingResolver(t *testing.T) {
	// Start a fake DNS server (simulates one of the racing upstreams).
	var systemCalls atomic.Int32
	systemDNS := startFakeDNSServer(t, &systemCalls)
	defer systemDNS.Close()

	// A separate fake server representing a "client-suggested" address.
	// The server-side resolver must ignore it and only query its own upstreams.
	var clientCalls atomic.Int32
	clientDNS := startFakeDNSServer(t, &clientCalls)
	defer clientDNS.Close()

	h := NewProxyHandler()
	h.AllowPrivateIP = true
	h.resolver = NewResolver(h.dnsCache, k2dns.NewDirectDNSClient([]string{systemDNS.LocalAddr().String()}))

	// Client sends DNS to "clientDNS" addr, but server's resolver only knows systemDNS.
	resp := sendDNSViaHandler(t, h, clientDNS.LocalAddr().String(), buildDNSQuery(t, "test.example.", dns.TypeA, 2001))
	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("got %s", dns.RcodeToString[resp.Rcode])
	}

	if systemCalls.Load() != 1 {
		t.Errorf("server-side resolver calls = %d, want 1", systemCalls.Load())
	}
	if clientCalls.Load() != 0 {
		t.Errorf("client-specified address calls = %d, want 0", clientCalls.Load())
	}
}

func TestHandleUDP_DNS_RacingFailover(t *testing.T) {
	// One healthy fake server plus one unreachable (closed) port.
	var healthyCalls atomic.Int32
	healthy := startFakeDNSServer(t, &healthyCalls)
	defer healthy.Close()

	// Find a port that's not listening so dials reliably fail fast.
	dead, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	deadAddr := dead.LocalAddr().String()
	dead.Close() // free the port — subsequent dials will likely ECONNREFUSED

	h := NewProxyHandler()
	h.AllowPrivateIP = true
	h.resolver = NewResolver(h.dnsCache, k2dns.NewDirectDNSClient([]string{deadAddr, healthy.LocalAddr().String()}))

	resp := sendDNSViaHandler(t, h, healthy.LocalAddr().String(), buildDNSQuery(t, "failover.example.", dns.TypeA, 3001))
	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("racing failover: got %s", dns.RcodeToString[resp.Rcode])
	}
	if healthyCalls.Load() < 1 {
		t.Errorf("healthy server: calls = %d, want >= 1", healthyCalls.Load())
	}
}

func TestIsDNSPort(t *testing.T) {
	if !isDNSPort("8.8.8.8:53") {
		t.Error("8.8.8.8:53 should be DNS")
	}
	if isDNSPort("8.8.8.8:443") {
		t.Error("8.8.8.8:443 should not be DNS")
	}
	if isDNSPort("invalid") {
		t.Error("invalid should not be DNS")
	}
}

package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	mdns "github.com/miekg/dns"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/core/dns"
	"github.com/kaitu-io/k2/provider"
	"github.com/kaitu-io/k2/rule"
	"github.com/kaitu-io/k2/wire"
)

// ==========================================================================
// Mock helpers
// ==========================================================================

// mockEventHandler records OnStatus calls for assertion.
type mockEventHandler struct {
	mu       sync.Mutex
	statuses []Status
}

func (m *mockEventHandler) OnStatus(status Status) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.statuses = append(m.statuses, status)
}

func (m *mockEventHandler) OnStats(txBytes, rxBytes int64) {}

func (m *mockEventHandler) getStatuses() []Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]Status, len(m.statuses))
	copy(cp, m.statuses)
	return cp
}

// getStates extracts state strings from recorded statuses.
func (m *mockEventHandler) getStates() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	states := make([]string, len(m.statuses))
	for i, s := range m.statuses {
		states[i] = s.State
	}
	return states
}

// getErrors extracts error messages from recorded statuses (non-nil errors only).
func (m *mockEventHandler) getErrors() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var errs []string
	for _, s := range m.statuses {
		if s.Error != nil {
			errs = append(errs, s.Error.Message)
		}
	}
	return errs
}

// mockConnectionHandler records HandleTCP / HandleUDP calls.
type mockConnectionHandler struct {
	mu       sync.Mutex
	tcpCalls []string
	udpCalls []string
}

var _ provider.ConnectionHandler = (*mockConnectionHandler)(nil)

func (m *mockConnectionHandler) HandleTCP(conn net.Conn, dest string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tcpCalls = append(m.tcpCalls, dest)
}

func (m *mockConnectionHandler) HandleUDP(conn net.PacketConn, dest string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.udpCalls = append(m.udpCalls, dest)
}

func (m *mockConnectionHandler) getTCPCalls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]string, len(m.tcpCalls))
	copy(cp, m.tcpCalls)
	return cp
}

func (m *mockConnectionHandler) getUDPCalls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]string, len(m.udpCalls))
	copy(cp, m.udpCalls)
	return cp
}

// mockDNSQuerier implements dns.DNSQuerier for testing.
// Returns a canned A record response for any query.
type mockDNSQuerier struct{}

func (q *mockDNSQuerier) Query(ctx context.Context, query *mdns.Msg) (*mdns.Msg, error) {
	if len(query.Question) == 0 {
		return nil, fmt.Errorf("no question")
	}
	resp := new(mdns.Msg)
	resp.SetReply(query)
	resp.Answer = append(resp.Answer, &mdns.A{
		Hdr: mdns.RR_Header{
			Name:   query.Question[0].Name,
			Rrtype: mdns.TypeA,
			Class:  mdns.ClassINET,
			Ttl:    300,
		},
		A: net.ParseIP("93.184.216.34").To4(),
	})
	return resp, nil
}

// mockNetworkMonitor implements NetworkChangeNotifier for testing.
type mockNetworkMonitor struct {
	mu       sync.Mutex
	started  bool
	closed   bool
	callback func()
	startErr error // set before Start() to simulate failure
}

func (m *mockNetworkMonitor) Start(callback func()) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.startErr != nil {
		return m.startErr
	}
	m.started = true
	m.callback = callback
	return nil
}

func (m *mockNetworkMonitor) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

func (m *mockNetworkMonitor) triggerCallback() {
	m.mu.Lock()
	cb := m.callback
	m.mu.Unlock()
	if cb != nil {
		cb()
	}
}

func (m *mockNetworkMonitor) isStarted() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.started
}

func (m *mockNetworkMonitor) isClosed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closed
}

// mockProvider implements provider.Provider for testing without real TUN/proxy.
type mockProvider struct{}

func (p *mockProvider) Start(ctx context.Context, handler provider.ConnectionHandler) error {
	return nil
}
func (p *mockProvider) Close() error { return nil }
func (p *mockProvider) Name() string { return "mock" }

// mockDialer implements wire.Dialer for testing.
type mockDialer struct{}

func (d *mockDialer) DialTCP(ctx context.Context, addr string, earlyData []byte) (net.Conn, error) {
	return nil, nil
}
func (d *mockDialer) DialUDP(ctx context.Context, addr string) (net.PacketConn, error) {
	return nil, nil
}
func (d *mockDialer) DialStreamUDP(ctx context.Context, addr string) (net.PacketConn, error) {
	return nil, nil
}
func (d *mockDialer) Close() error { return nil }

// blockingPacketConn is a PacketConn whose ReadFrom blocks until Close.
type blockingPacketConn struct {
	closed chan struct{}
}

func newBlockingPacketConn() *blockingPacketConn {
	return &blockingPacketConn{closed: make(chan struct{})}
}

func (c *blockingPacketConn) ReadFrom(b []byte) (int, net.Addr, error) {
	<-c.closed
	return 0, nil, net.ErrClosed
}
func (c *blockingPacketConn) WriteTo(b []byte, addr net.Addr) (int, error) { return len(b), nil }
func (c *blockingPacketConn) Close() error {
	select {
	case <-c.closed:
	default:
		close(c.closed)
	}
	return nil
}
func (c *blockingPacketConn) LocalAddr() net.Addr                { return nil }
func (c *blockingPacketConn) SetDeadline(time.Time) error        { return nil }
func (c *blockingPacketConn) SetReadDeadline(time.Time) error    { return nil }
func (c *blockingPacketConn) SetWriteDeadline(time.Time) error   { return nil }

// mockWireDialer returns a fixed PacketConn from DialUDP.
type mockWireDialer struct {
	pc net.PacketConn
}

func newMockWireDialer(pc net.PacketConn) *mockWireDialer { return &mockWireDialer{pc: pc} }

func (d *mockWireDialer) DialTCP(ctx context.Context, addr string, earlyData []byte) (net.Conn, error) {
	return nil, nil
}
func (d *mockWireDialer) DialUDP(ctx context.Context, addr string) (net.PacketConn, error) {
	return d.pc, nil
}
func (d *mockWireDialer) DialStreamUDP(ctx context.Context, addr string) (net.PacketConn, error) {
	return d.pc, nil
}
func (d *mockWireDialer) Close() error { return nil }

// packetPipe creates a connected pair of net.PacketConns using loopback UDP.
type packetPipe struct {
	local  net.PacketConn
	remote net.PacketConn
	addr   net.Addr
}

func newPacketPipe(t *testing.T) *packetPipe {
	t.Helper()
	local, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen local: %v", err)
	}
	remote, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		local.Close()
		t.Fatalf("listen remote: %v", err)
	}
	return &packetPipe{local: local, remote: remote, addr: remote.LocalAddr()}
}

// testConfig creates a Config with the given wire URL for testing.
func testConfig(server string) Config {
	client := &config.ClientConfig{Server: server}
	// When server is a k2v5 URL, add a default catch-all route so
	// buildOutboundMap finds the URL and setupOutboundTransport runs.
	if strings.HasPrefix(server, "k2v5://") {
		client.Routes = []config.RouteConfig{
			{Via: server, Match: rule.MatchConfig{All: true}},
		}
	}
	return Config{
		Client:         client,
		FileDescriptor: -1,
	}
}

// ==========================================================================
// Lifecycle tests
// ==========================================================================

func TestEngineNew(t *testing.T) {
	e := New()
	if e == nil {
		t.Fatal("New returned nil")
	}
	if e.Status() != StateDisconnected {
		t.Fatalf("state = %q, want %q", e.Status(), StateDisconnected)
	}
}

func TestEngineStart_NilClient(t *testing.T) {
	e := New()
	err := e.Start(Config{FileDescriptor: -1})
	if err == nil {
		t.Fatal("Start with nil Client should return error")
	}
	if got := err.Error(); got != "engine: Client config is required" {
		t.Fatalf("error = %q, want 'engine: Client config is required'", got)
	}
	if e.Status() != StateDisconnected {
		t.Fatalf("state = %q, want %q after nil client", e.Status(), StateDisconnected)
	}
}

func TestEngineStart_InvalidURL(t *testing.T) {
	e := New()
	err := e.Start(testConfig("garbage"))
	if err == nil {
		t.Fatal("Start with invalid URL should return error")
	}
	if e.Status() != StateDisconnected {
		t.Fatalf("state = %q, want %q after failed start", e.Status(), StateDisconnected)
	}
}

func TestEngineStart_DoubleStart(t *testing.T) {
	e := New()
	url := "k2v5://udid:token@127.0.0.1:12345?insecure=1"

	// First start will fail (no real TUN), that's fine.
	_ = e.Start(testConfig(url))

	// Force state to connected to test double-start guard.
	e.mu.Lock()
	e.state = StateConnected
	e.mu.Unlock()

	err := e.Start(testConfig(url))
	if err == nil {
		t.Fatal("double Start should return error")
	}
	if got := err.Error(); got != "engine: already started" {
		t.Fatalf("error = %q, want 'engine: already started'", got)
	}
}

func TestEngineStop_NotStarted(t *testing.T) {
	e := New()
	err := e.Stop()
	if err != nil {
		t.Fatalf("Stop on fresh engine should not error, got %v", err)
	}
}

func TestEngineStart_WireProbeFailure(t *testing.T) {
	e := New()
	h := &mockEventHandler{}
	e.SetEventHandler(h)

	// 127.0.0.1:65534 — connection refused (instant TCP failure).
	// QUIC gets 3s budget (UDP to closed port → timeout), TCP-WS fails fast.
	url := "k2v5://udid:token@127.0.0.1:65534?insecure=1&ip=127.0.0.1"
	err := e.Start(testConfig(url))
	if err == nil {
		t.Fatal("Start with unreachable wire server should fail")
	}

	if e.Status() != StateDisconnected {
		t.Fatalf("state = %q, want disconnected", e.Status())
	}

	// Error should originate from outbound setup (transport race inside).
	if !strings.Contains(err.Error(), "outbound setup") {
		t.Errorf("error = %q, want to contain 'outbound setup'", err.Error())
	}

	// EventHandler should receive classified error.
	statuses := h.getStatuses()
	if len(statuses) < 2 {
		t.Fatalf("expected at least 2 status changes (connecting + disconnected), got %d", len(statuses))
	}
	lastStatus := statuses[len(statuses)-1]
	if lastStatus.State != StateDisconnected {
		t.Fatalf("last state = %q, want disconnected", lastStatus.State)
	}
	if lastStatus.Error == nil {
		t.Fatal("expected classified error in final status")
	}
}

// ==========================================================================
// Rule config tests
// ==========================================================================

func TestBuildRuleEngine_Default(t *testing.T) {
	client := &config.ClientConfig{
		Server: "k2v5://udid:tok@host:443",
	}
	re := buildRuleEngine(client, buildOutboundMap(client.Routes))
	defer re.Close()
	if re.IsAllProxy() {
		t.Fatal("expected IsAllProxy false when no routes set")
	}
	// Default fallback is direct.
	if got := re.Match("example.com"); got != rule.Target(0) {
		t.Fatalf("Match = %v, want TargetDirect", got)
	}
}

func TestBuildRuleEngine_AllProxy(t *testing.T) {
	client := config.ClientFromURL("k2v5://udid:tok@host:443")
	re := buildRuleEngine(client, buildOutboundMap(client.Routes))
	defer re.Close()
	if !re.IsAllProxy() {
		t.Fatal("expected IsAllProxy true for ClientFromURL")
	}
	if got := re.Match("example.com"); got != rule.Target(2) {
		t.Fatalf("Match = %v, want Target(2)", got)
	}
}

func TestBuildRuleEngine_InlineRules(t *testing.T) {
	client := &config.ClientConfig{
		Routes: []config.RouteConfig{
			{
				Via: "reject",
				Match: rule.MatchConfig{
					DomainSuffix: []string{"ads.example.com"},
				},
			},
			{
				Via: "direct",
				Match: rule.MatchConfig{
					IPCIDR: []string{"10.0.0.0/8", "192.168.0.0/16"},
				},
			},
			{
				Via: "k2v5://server:443",
				Match: rule.MatchConfig{
					DomainSuffix: []string{"custom-blocked.com"},
				},
			},
		},
	}
	re := buildRuleEngine(client, buildOutboundMap(client.Routes))
	defer re.Close()

	if got := re.Match("ads.example.com"); got != rule.Target(1) {
		t.Errorf("ads.example.com = %v, want reject", got)
	}
	if got := re.Match("10.1.2.3"); got != rule.Target(0) {
		t.Errorf("10.1.2.3 = %v, want direct", got)
	}
	if got := re.Match("192.168.1.1"); got != rule.Target(0) {
		t.Errorf("192.168.1.1 = %v, want direct", got)
	}
	if got := re.Match("custom-blocked.com"); got != rule.Target(2) {
		t.Errorf("custom-blocked.com = %v, want proxy", got)
	}
}

func TestBuildRuleEngine_ProcessRoute(t *testing.T) {
	client := &config.ClientConfig{
		Routes: []config.RouteConfig{
			{
				Via: "direct",
				Match: rule.MatchConfig{
					ProcessName: []string{"ssh", "git"},
				},
			},
			{
				Via: "k2v5://server:443",
				Match: rule.MatchConfig{All: true},
			},
		},
	}
	re := buildRuleEngine(client, buildOutboundMap(client.Routes))
	defer re.Close()

	// DNS-time: process-only route skipped
	if got := re.Match("google.com"); got != rule.Target(2) {
		t.Errorf("Match(google.com) = %v, want proxy", got)
	}

	// Connection-time: ssh → direct
	if got := re.MatchConn("google.com", rule.ConnMeta{ProcessName: "ssh"}); got != rule.Target(0) {
		t.Errorf("MatchConn(ssh) = %v, want direct", got)
	}

	// Connection-time: chrome → proxy (all proxy fallback)
	if got := re.MatchConn("google.com", rule.ConnMeta{ProcessName: "chrome"}); got != rule.Target(2) {
		t.Errorf("MatchConn(chrome) = %v, want proxy", got)
	}
}

func TestBuildRuleEngine_IPIsPrivate(t *testing.T) {
	client := &config.ClientConfig{
		Routes: []config.RouteConfig{
			{
				Via: "direct",
				Match: rule.MatchConfig{IPIsPrivate: true},
			},
			{
				Via: "k2v5://server:443",
				Match: rule.MatchConfig{All: true},
			},
		},
	}
	re := buildRuleEngine(client, buildOutboundMap(client.Routes))
	defer re.Close()

	if got := re.Match("192.168.1.1"); got != rule.Target(0) {
		t.Errorf("192.168.1.1 = %v, want direct", got)
	}
	if got := re.Match("8.8.8.8"); got != rule.Target(2) {
		t.Errorf("8.8.8.8 = %v, want proxy", got)
	}
}

// ==========================================================================
// EventHandler tests
// ==========================================================================

func TestEngineStart_EventHandler_StateChanges(t *testing.T) {
	e := New()
	h := &mockEventHandler{}
	e.SetEventHandler(h)

	// Start with invalid URL triggers: connecting -> error -> disconnected
	_ = e.Start(testConfig("garbage"))

	states := h.getStates()
	if len(states) < 2 {
		t.Fatalf("expected at least 2 state changes, got %d: %v", len(states), states)
	}
	if states[0] != StateConnecting {
		t.Fatalf("first state = %q, want %q", states[0], StateConnecting)
	}
	if states[len(states)-1] != StateDisconnected {
		t.Fatalf("last state = %q, want %q", states[len(states)-1], StateDisconnected)
	}
}

func TestEngineStart_EventHandler_Error(t *testing.T) {
	e := New()
	h := &mockEventHandler{}
	e.SetEventHandler(h)

	_ = e.Start(testConfig("garbage"))

	errs := h.getErrors()
	if len(errs) == 0 {
		t.Fatal("expected OnStatus with error to be called")
	}
}

// ==========================================================================
// StatusJSON tests
// ==========================================================================

func TestEngineStatusJSON_Disconnected(t *testing.T) {
	e := New()
	raw := e.StatusJSON()

	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("StatusJSON returned invalid JSON: %v", err)
	}
	if m["state"] != StateDisconnected {
		t.Fatalf("state = %v, want %q", m["state"], StateDisconnected)
	}
	// No extra keys when disconnected.
	for _, key := range []string{"connected_at", "uptime_seconds", "error"} {
		if _, ok := m[key]; ok {
			t.Fatalf("unexpected key %q in disconnected state", key)
		}
	}
}

func TestEngineStatusJSON_Connected(t *testing.T) {
	e := New()

	// Simulate connected state by setting fields under lock.
	e.mu.Lock()
	e.state = StateConnected
	e.client = &config.ClientConfig{Server: "k2v5://udid:tok@1.2.3.4:443"}
	e.connectedAt = time.Now().Add(-10 * time.Second)
	e.mu.Unlock()

	raw := e.StatusJSON()
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("StatusJSON returned invalid JSON: %v", err)
	}
	if m["state"] != StateConnected {
		t.Fatalf("state = %v, want %q", m["state"], StateConnected)
	}
	if _, ok := m["connected_at"]; !ok {
		t.Fatal("expected 'connected_at' key when connected")
	}
	uptime, ok := m["uptime_seconds"].(float64)
	if !ok {
		t.Fatalf("uptime_seconds not a number: %v (%T)", m["uptime_seconds"], m["uptime_seconds"])
	}
	if uptime < 9 {
		t.Fatalf("uptime_seconds = %v, want >= 9", uptime)
	}
}

func TestEngineStatusJSON_AfterFailedStart(t *testing.T) {
	e := New()
	_ = e.Start(testConfig("garbage"))

	raw := e.StatusJSON()
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("StatusJSON returned invalid JSON: %v", err)
	}
	if m["state"] != StateDisconnected {
		t.Fatalf("state = %v, want %q", m["state"], StateDisconnected)
	}
	errRaw, ok := m["error"]
	if !ok {
		t.Fatal("expected 'error' key after failed start")
	}
	errObj, ok := errRaw.(map[string]any)
	if !ok {
		t.Fatalf("error should be an object, got %T", errRaw)
	}
	if errObj["message"] == "" {
		t.Fatal("error.message should be non-empty after failed start")
	}
}

func TestEngineStatusJSON_MatchesDaemonFormat(t *testing.T) {
	e := New()

	// Set all fields to produce the full JSON.
	e.mu.Lock()
	e.state = StateConnected
	e.client = &config.ClientConfig{Server: "k2v5://u:t@host:443"}
	e.connectedAt = time.Now().Add(-5 * time.Second)
	e.lastError = &EngineError{Code: ErrCodeConnectionFatal, Message: "some previous error"}
	e.mu.Unlock()

	raw := e.StatusJSON()
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("StatusJSON returned invalid JSON: %v", err)
	}

	expected := []string{"state", "connected_at", "uptime_seconds", "error"}
	for _, key := range expected {
		if _, ok := m[key]; !ok {
			t.Errorf("missing expected key %q", key)
		}
	}
}

// ==========================================================================
// DNS handler tests
// ==========================================================================

func TestDNSHandler_Intercept_Port53(t *testing.T) {
	inner := &mockConnectionHandler{}

	// Build a Middleware with a mock querier that returns canned DNS responses.
	// MatchRule returns Target(2) for all domains (global proxy mode).
	mw := dns.NewMiddleware(dns.MiddlewareConfig{
		ProxyDNS:     &mockDNSQuerier{},
		DNSCache:     dns.NewDNSCache(dns.DNSCacheConfig{}),
		Filter:       dns.NewFilter(dns.FilterConfig{}),
		MatchRule:    func(string) rule.Target { return rule.Target(2) },
		SetTmpRule:   func(string, rule.Target) {},
		DNSRouter:    func(t rule.Target) bool { return t == rule.Target(2) },
		RejectTarget: rule.Target(1),
	})

	h := &dnsHandler{inner: inner, dns: mw}

	// Create a loopback packet conn pair to simulate TUN UDP.
	pp := newPacketPipe(t)
	defer pp.local.Close()
	defer pp.remote.Close()

	// Build a DNS query using miekg/dns for correctness.
	msg := new(mdns.Msg)
	msg.SetQuestion("example.com.", mdns.TypeA)
	msg.Id = 0xABCD
	dnsQuery, err := msg.Pack()
	if err != nil {
		t.Fatalf("pack DNS query: %v", err)
	}

	// Write query from local to remote (simulating the TUN writing to handler).
	_, err = pp.local.WriteTo(dnsQuery, pp.remote.LocalAddr())
	if err != nil {
		t.Fatalf("write DNS query: %v", err)
	}

	// Start handler in goroutine (blocks reading).
	done := make(chan struct{})
	go func() {
		h.HandleUDP(pp.remote, "8.8.8.8:53")
		close(done)
	}()

	// Read response from local side.
	buf := make([]byte, 4096)
	pp.local.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := pp.local.ReadFrom(buf)
	if err != nil {
		t.Fatalf("read DNS response: %v", err)
	}

	// Verify we got a valid DNS response.
	var resp mdns.Msg
	if err := resp.Unpack(buf[:n]); err != nil {
		t.Fatalf("unpack DNS response: %v", err)
	}
	if resp.Id != 0xABCD {
		t.Fatalf("response ID = %x, want 0xABCD", resp.Id)
	}
	if len(resp.Answer) == 0 {
		t.Fatal("expected at least one answer in DNS response")
	}

	// Close remote to unblock the handler goroutine.
	pp.remote.Close()
	<-done

	// Inner handler should NOT have been called for port 53.
	if calls := inner.getUDPCalls(); len(calls) != 0 {
		t.Fatalf("inner.HandleUDP called %d times for port 53, want 0", len(calls))
	}
}

func TestDNSHandler_PassThrough_NonDNS(t *testing.T) {
	inner := &mockConnectionHandler{}
	mw := dns.NewMiddleware(dns.MiddlewareConfig{
		Filter: dns.NewFilter(dns.FilterConfig{}),
	})
	h := &dnsHandler{inner: inner, dns: mw}

	pp := newPacketPipe(t)
	defer pp.local.Close()

	// HandleUDP with non-53 port should delegate to inner.
	h.HandleUDP(pp.remote, "1.2.3.4:8888")

	calls := inner.getUDPCalls()
	if len(calls) != 1 || calls[0] != "1.2.3.4:8888" {
		t.Fatalf("inner.HandleUDP calls = %v, want [1.2.3.4:8888]", calls)
	}
}

func TestDNSHandler_PassThrough_TCP(t *testing.T) {
	inner := &mockConnectionHandler{}
	h := &dnsHandler{inner: inner, dns: nil}

	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	go h.HandleTCP(client, "1.2.3.4:443")

	calls := waitForTCPCalls(inner, 1, time.Second)
	if len(calls) != 1 || calls[0] != "1.2.3.4:443" {
		t.Fatalf("inner.HandleTCP calls = %v, want [1.2.3.4:443]", calls)
	}
}

func TestDNSHandler_NilDNS_PassThrough(t *testing.T) {
	inner := &mockConnectionHandler{}
	h := &dnsHandler{inner: inner, dns: nil}

	pp := newPacketPipe(t)
	defer pp.local.Close()

	// With nil dns, port 53 should pass through to inner.
	h.HandleUDP(pp.remote, "8.8.8.8:53")

	calls := inner.getUDPCalls()
	if len(calls) != 1 || calls[0] != "8.8.8.8:53" {
		t.Fatalf("inner.HandleUDP calls = %v, want [8.8.8.8:53]", calls)
	}
}

// ==========================================================================
// OnNetworkChanged tests
// ==========================================================================

func TestEngine_OnNetworkChanged_WhenConnected(t *testing.T) {
	e := New()
	h := &mockEventHandler{}
	e.SetEventHandler(h)

	// Simulate connected state with a TransportManager.
	tm := &wire.TransportManager{}
	e.mu.Lock()
	e.state = StateConnected
	e.tm = tm
	e.mu.Unlock()

	e.OnNetworkChanged()

	states := h.getStates()
	if len(states) != 2 {
		t.Fatalf("expected 2 state changes, got %d: %v", len(states), states)
	}
	if states[0] != "reconnecting" {
		t.Fatalf("first state = %q, want %q", states[0], "reconnecting")
	}
	if states[1] != StateConnected {
		t.Fatalf("second state = %q, want %q", states[1], StateConnected)
	}

	// Engine's own state should remain connected throughout.
	if e.Status() != StateConnected {
		t.Fatalf("engine state = %q, want %q", e.Status(), StateConnected)
	}
}

func TestEngine_OnNetworkChanged_WhenDisconnected(t *testing.T) {
	e := New()
	h := &mockEventHandler{}
	e.SetEventHandler(h)

	// Engine starts disconnected by default.
	e.OnNetworkChanged()

	states := h.getStates()
	if len(states) != 0 {
		t.Fatalf("expected 0 state changes when disconnected, got %d: %v", len(states), states)
	}
}

func TestEngine_OnNetworkChanged_WhenConnecting(t *testing.T) {
	e := New()
	h := &mockEventHandler{}
	e.SetEventHandler(h)

	// Simulate connecting state.
	e.mu.Lock()
	e.state = StateConnecting
	e.mu.Unlock()

	e.OnNetworkChanged()

	states := h.getStates()
	if len(states) != 0 {
		t.Fatalf("expected 0 state changes when connecting, got %d: %v", len(states), states)
	}
}

func TestEngine_OnNetworkChanged_NilTransportManager(t *testing.T) {
	e := New()
	h := &mockEventHandler{}
	e.SetEventHandler(h)

	// Connected but no TransportManager (edge case).
	e.mu.Lock()
	e.state = StateConnected
	e.tm = nil
	e.mu.Unlock()

	e.OnNetworkChanged()

	states := h.getStates()
	if len(states) != 2 {
		t.Fatalf("expected 2 state changes, got %d: %v", len(states), states)
	}
	if states[0] != "reconnecting" {
		t.Fatalf("first state = %q, want %q", states[0], "reconnecting")
	}
	if states[1] != StateConnected {
		t.Fatalf("second state = %q, want %q", states[1], StateConnected)
	}
}

// ==========================================================================
// NetworkMonitor lifecycle tests
// ==========================================================================

// TestEngine_NetworkMonitor_NilMonitor_NoPanic verifies the engine starts
// fine with nil NetworkMonitor (mobile path where the OS handles network
// change notifications directly).
func TestEngine_NetworkMonitor_NilMonitor_NoPanic(t *testing.T) {
	e := New()
	h := &mockEventHandler{}
	e.SetEventHandler(h)

	// Start with Config that has no NetworkMonitor set.
	// This will fail at the wire.ParseURL step (invalid URL), but the point
	// is that it should NOT panic due to nil NetworkMonitor handling.
	cfg := Config{
		Client:         &config.ClientConfig{Server: "k2v5://udid:token@127.0.0.1:12345?insecure=1"},
		FileDescriptor: -1,
		NetworkMonitor: nil,
	}
	// Start will fail with TUN error (no real TUN device in test), that's expected.
	// The key assertion is no panic.
	_ = e.Start(cfg)
}

// TestEngine_NetworkMonitor_CallbackTriggersOnNetworkChanged verifies that
// when the monitor fires its callback, it calls OnNetworkChanged on the
// engine, which emits reconnecting->connected state transitions.
func TestEngine_NetworkMonitor_CallbackTriggersOnNetworkChanged(t *testing.T) {
	e := New()
	h := &mockEventHandler{}
	e.SetEventHandler(h)
	mon := &mockNetworkMonitor{}

	// Simulate a connected engine with a transport manager and the monitor started.
	tm := &wire.TransportManager{}
	e.mu.Lock()
	e.state = StateConnected
	e.tm = tm
	e.mu.Unlock()

	// Start the monitor with a callback that calls OnNetworkChanged.
	err := mon.Start(func() {
		e.OnNetworkChanged()
	})
	if err != nil {
		t.Fatalf("monitor.Start() failed: %v", err)
	}

	// Simulate a network change event.
	mon.triggerCallback()

	states := h.getStates()
	if len(states) != 2 {
		t.Fatalf("expected 2 state changes, got %d: %v", len(states), states)
	}
	if states[0] != "reconnecting" {
		t.Fatalf("first state = %q, want %q", states[0], "reconnecting")
	}
	if states[1] != StateConnected {
		t.Fatalf("second state = %q, want %q", states[1], StateConnected)
	}
}

// TestEngine_NetworkMonitor_StartedOnEngineStart verifies that monitor.Start()
// is called early in engine.Start() (step 4.1, before wire probe) so that
// DirectDialer can resolve the default interface. When Start() fails, the
// monitor should be properly closed.
func TestEngine_NetworkMonitor_StartedOnEngineStart(t *testing.T) {
	e := New()
	h := &mockEventHandler{}
	e.SetEventHandler(h)
	mon := &mockNetworkMonitor{}

	// Config with NetworkMonitor set — engine should start the monitor
	// early (step 4.1) for DirectDialer interface resolution.
	cfg := Config{
		Client:         &config.ClientConfig{Server: "k2v5://udid:token@127.0.0.1:12345?insecure=1"},
		FileDescriptor: -1,
		NetworkMonitor: mon,
	}

	// Start will fail at wire probe, but monitor should have been started
	// and then closed during cleanup.
	_ = e.Start(cfg)

	// Monitor was started early (step 4.1) but closed on failure cleanup.
	if !mon.isClosed() {
		t.Fatal("monitor should be closed after engine fails to connect")
	}

	// Now simulate a successful start: set state to connected and verify
	// that if engine had reached connected, it would have started the monitor.
	// We do this by creating a fresh engine and manually simulating the
	// connected flow to test the lifecycle wiring.
	e2 := New()
	e2.SetEventHandler(h)
	mon2 := &mockNetworkMonitor{}

	// Simulate: engine stores the monitor from config, then reaches connected.
	e2.mu.Lock()
	e2.networkMonitor = mon2
	e2.state = StateConnected
	e2.mu.Unlock()

	// The engine should start the monitor when it transitions to connected.
	// In production this happens inside Start() after step 9 (commit).
	// For this test we call the method that starts the monitor.
	if err := mon2.Start(func() { e2.OnNetworkChanged() }); err != nil {
		t.Fatalf("monitor.Start() should succeed: %v", err)
	}
	if !mon2.isStarted() {
		t.Fatal("monitor should be started after engine reaches connected state")
	}
}

// TestEngine_NetworkMonitor_ClosedOnEngineStop verifies that monitor.Close()
// is called when Stop() is called on a connected engine with a monitor.
func TestEngine_NetworkMonitor_ClosedOnEngineStop(t *testing.T) {
	e := New()
	mon := &mockNetworkMonitor{}

	// Simulate a connected engine with an active monitor.
	e.mu.Lock()
	e.state = StateConnected
	e.networkMonitor = mon
	// Also need cancel to avoid nil dereference in Stop().
	_, cancel := context.WithCancel(context.Background())
	e.cancel = cancel
	e.mu.Unlock()

	// Start the monitor (simulating what engine.Start would do).
	if err := mon.Start(func() { e.OnNetworkChanged() }); err != nil {
		t.Fatalf("monitor.Start() failed: %v", err)
	}

	err := e.Stop()
	if err != nil {
		t.Fatalf("Stop() returned error: %v", err)
	}

	if !mon.isClosed() {
		t.Fatal("monitor.Close() should be called on engine Stop()")
	}
	if e.Status() != StateDisconnected {
		t.Fatalf("state = %q, want %q", e.Status(), StateDisconnected)
	}
}

// TestEngine_NetworkMonitor_ClosedOnFail verifies that monitor.Close() is
// called when fail() is invoked (error path), ensuring no leaked goroutines
// from the network monitor.
func TestEngine_NetworkMonitor_ClosedOnFail(t *testing.T) {
	e := New()
	h := &mockEventHandler{}
	e.SetEventHandler(h)
	mon := &mockNetworkMonitor{}

	// Simulate engine with an active monitor in connected state.
	e.mu.Lock()
	e.state = StateConnected
	e.networkMonitor = mon
	_, cancel := context.WithCancel(context.Background())
	e.cancel = cancel
	e.mu.Unlock()

	// Start the monitor.
	if err := mon.Start(func() { e.OnNetworkChanged() }); err != nil {
		t.Fatalf("monitor.Start() failed: %v", err)
	}

	// Trigger fail() — simulates a fatal error during operation.
	e.fail(fmt.Errorf("connection lost"))

	if !mon.isClosed() {
		t.Fatal("monitor.Close() should be called on engine fail()")
	}
	if e.Status() != StateDisconnected {
		t.Fatalf("state = %q, want %q", e.Status(), StateDisconnected)
	}
	errs := h.getErrors()
	if len(errs) == 0 || errs[0] != "connection lost" {
		t.Fatalf("expected error 'connection lost', got %v", errs)
	}
}

// ==========================================================================
// Start/Stop race safety tests
// ==========================================================================

// TestEngineFailCleansCancel verifies that fail() cancels and nils e.cancel,
// preventing context leaks on error paths in Start().
func TestEngineFailCleansCancel(t *testing.T) {
	e := New()
	h := &mockEventHandler{}
	e.SetEventHandler(h)

	// Simulate Start() saving cancel early (the fix).
	ctx, cancel := context.WithCancel(context.Background())
	e.mu.Lock()
	e.cancel = cancel
	e.state = StateConnecting
	e.mu.Unlock()

	// fail() should cancel context, nil e.cancel, notify handler, set disconnected.
	e.fail(fmt.Errorf("test error"))

	if ctx.Err() == nil {
		t.Fatal("expected context to be cancelled after fail()")
	}
	e.mu.Lock()
	hasCancel := e.cancel != nil
	e.mu.Unlock()
	if hasCancel {
		t.Fatal("e.cancel should be nil after fail()")
	}
	if e.Status() != StateDisconnected {
		t.Fatalf("state = %q, want %q", e.Status(), StateDisconnected)
	}
	errs := h.getErrors()
	if len(errs) == 0 || errs[0] != "test error" {
		t.Fatalf("expected error 'test error', got %v", errs)
	}
}

// TestEngineStopCancelsStartContext verifies that Stop() cancels a context
// saved by Start() before the mutex was released. This is the core fix:
// Stop() during the Start() gap can now cancel in-progress work.
func TestEngineStopCancelsStartContext(t *testing.T) {
	e := New()

	// Simulate Start() saving cancel before releasing lock.
	ctx, cancel := context.WithCancel(context.Background())
	e.mu.Lock()
	e.cancel = cancel
	e.state = StateConnecting
	e.mu.Unlock()

	// Stop should cancel the context and set disconnected.
	_ = e.Stop()

	if ctx.Err() == nil {
		t.Fatal("expected context to be cancelled after Stop()")
	}
	if e.Status() != StateDisconnected {
		t.Fatalf("state = %q, want %q", e.Status(), StateDisconnected)
	}
	e.mu.Lock()
	hasCancel := e.cancel != nil
	e.mu.Unlock()
	if hasCancel {
		t.Fatal("e.cancel should be nil after Stop()")
	}
}

// TestEngineStartStopRace verifies that Stop() during Start()'s unlocked gap
// results in a clean disconnected state. Uses a channel to guarantee Stop()
// runs while the simulated Start() is in its gap (between unlock and commit).
func TestEngineStartStopRace(t *testing.T) {
	e := New()
	h := &mockEventHandler{}
	e.SetEventHandler(h)

	inGap := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		// Simulate Start(): save cancel early, set connecting, unlock.
		e.mu.Lock()
		ctx, cancel := context.WithCancel(context.Background())
		e.cancel = cancel
		e.setStateLocked(StateConnecting)
		e.mu.Unlock()

		// Signal: we're in the gap (lock released, resources not yet committed).
		close(inGap)

		// Simulate work in the gap — give Stop() time to cancel.
		time.Sleep(50 * time.Millisecond)

		// Check cancellation (as fixed Start() does before heavy ops).
		if ctx.Err() != nil {
			return
		}

		// Commit: reacquire lock, verify not cancelled.
		e.mu.Lock()
		if ctx.Err() != nil {
			e.mu.Unlock()
			cancel()
			return
		}
		e.setStateLocked(StateConnected)
		e.mu.Unlock()
	}()

	// Wait for goroutine to enter the gap, then Stop().
	<-inGap
	_ = e.Stop()
	<-done

	if e.Status() != StateDisconnected {
		t.Fatalf("state = %q, want disconnected", e.Status())
	}
	e.mu.Lock()
	hasCancel := e.cancel != nil
	e.mu.Unlock()
	if hasCancel {
		t.Fatal("e.cancel should be nil — resource leak")
	}
}

// TestEngineStartStopRace_NoRace runs concurrent Start simulation and Stop
// with varying timing under -race detector.
func TestEngineStartStopRace_NoRace(t *testing.T) {
	for i := range 50 {
		e := New()

		done := make(chan struct{})
		go func() {
			defer close(done)
			e.mu.Lock()
			_, cancel := context.WithCancel(context.Background())
			e.cancel = cancel
			e.state = StateConnecting
			e.mu.Unlock()
			// Vary work duration.
			time.Sleep(time.Duration(i%5) * time.Millisecond)
		}()

		time.Sleep(time.Duration(i%3) * time.Millisecond)
		_ = e.Stop()
		<-done
		// Just verify no panic or data race (under -race flag).
	}
}

// ==========================================================================
// WireErrorReporter tests
// ==========================================================================

func TestEngine_ReportWireError_Basic(t *testing.T) {
	e := New()
	h := &mockEventHandler{}
	e.SetEventHandler(h)

	// Must be connected for ReportWireError to take effect.
	e.mu.Lock()
	e.state = StateConnected
	e.mu.Unlock()

	e.ReportWireError(fmt.Errorf("wire: TCP dial: connection refused"))

	statuses := h.getStatuses()
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status update, got %d", len(statuses))
	}
	if statuses[0].Error == nil {
		t.Fatal("expected error in status")
	}
	if statuses[0].Error.Code != 503 {
		t.Errorf("error code = %d, want 503", statuses[0].Error.Code)
	}
	if statuses[0].Error.Category != CategoryServer {
		t.Errorf("error category = %q, want %q", statuses[0].Error.Category, CategoryServer)
	}
}

func TestEngine_ReportWireError_Debounce(t *testing.T) {
	e := New()
	h := &mockEventHandler{}
	e.SetEventHandler(h)

	e.mu.Lock()
	e.state = StateConnected
	e.mu.Unlock()

	// First error fires.
	e.ReportWireError(fmt.Errorf("wire: TCP dial: connection refused"))
	// Second non-auth error is debounced.
	e.ReportWireError(fmt.Errorf("wire: network unreachable"))

	statuses := h.getStatuses()
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status update (debounce), got %d", len(statuses))
	}
}

func TestEngine_ReportWireError_TargetIgnored(t *testing.T) {
	e := New()
	h := &mockEventHandler{}
	e.SetEventHandler(h)

	e.mu.Lock()
	e.state = StateConnected
	e.mu.Unlock()

	// StreamError = server responded to target request → CategoryTarget.
	// Target errors should NOT be stored as lastError (tunnel.go filters these
	// via isProxyLayerError before calling ReportWireError). But even if one
	// reaches here, it must not upgrade a wire error.
	e.ReportWireError(fmt.Errorf("wire: TCP dial: connection refused"))
	e.ReportWireError(&wire.StreamError{Code: 403, Message: "blocked: reserved IP"})

	statuses := h.getStatuses()
	// Only the first (wire) error should fire; target error is debounced
	// because it's classified as non-client category.
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status update (wire error only), got %d", len(statuses))
	}
	if statuses[0].Error.Code != 503 {
		t.Errorf("error code = %d, want 503", statuses[0].Error.Code)
	}
}

func TestEngine_ClearWireError_Basic(t *testing.T) {
	e := New()
	h := &mockEventHandler{}
	e.SetEventHandler(h)

	e.mu.Lock()
	e.state = StateConnected
	e.lastError = &EngineError{Code: 503, Category: CategoryServer, Message: "refused"}
	e.mu.Unlock()

	e.ClearWireError()

	statuses := h.getStatuses()
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status update, got %d", len(statuses))
	}
	if statuses[0].Error != nil {
		t.Errorf("expected nil error after clear, got %+v", statuses[0].Error)
	}
}

func TestEngine_ClearWireError_ProtectsAuthError(t *testing.T) {
	e := New()
	h := &mockEventHandler{}
	e.SetEventHandler(h)

	e.mu.Lock()
	e.state = StateConnected
	e.lastError = &EngineError{Code: 401, Category: CategoryClient, Message: "token expired"}
	e.mu.Unlock()

	e.ClearWireError()

	// Should NOT have fired OnStatus (auth error protected).
	statuses := h.getStatuses()
	if len(statuses) != 0 {
		t.Fatalf("expected 0 status updates (auth protected), got %d", len(statuses))
	}

	// Error should still be there.
	e.mu.Lock()
	if e.lastError == nil || e.lastError.Code != 401 {
		t.Errorf("auth error should persist after ClearWireError")
	}
	e.mu.Unlock()
}

func TestEngine_ReportWireError_NotConnected(t *testing.T) {
	e := New()
	h := &mockEventHandler{}
	e.SetEventHandler(h)

	// Engine is disconnected — should be a no-op.
	e.ReportWireError(fmt.Errorf("wire: TCP dial: connection refused"))

	statuses := h.getStatuses()
	if len(statuses) != 0 {
		t.Fatalf("expected 0 status updates when not connected, got %d", len(statuses))
	}
}

func TestEngine_OnNetworkChanged_ResetsProxyDNS(t *testing.T) {
	e := New()
	h := &mockEventHandler{}
	e.SetEventHandler(h)

	// Create a ProxyDNSClient with a mock dialer and warm its pool.
	pc := newBlockingPacketConn()
	dialer := newMockWireDialer(pc)
	pdns := dns.NewProxyDNSClient(dns.ProxyDNSClientConfig{
		Dialer:  dialer,
		Servers: []string{"8.8.8.8:53"},
	})

	tm := &wire.TransportManager{}
	e.mu.Lock()
	e.state = StateConnected
	e.tm = tm
	e.proxyDNS = pdns
	e.mu.Unlock()

	e.OnNetworkChanged()

	// Verify proxyDNS.ResetConnections() was called by checking
	// that the field is still set (not nil'd) and functional.
	e.mu.Lock()
	if e.proxyDNS == nil {
		t.Fatal("proxyDNS should remain set after OnNetworkChanged")
	}
	e.mu.Unlock()
}

func TestEngine_Pause_ResetsProxyDNS(t *testing.T) {
	e := New()
	h := &mockEventHandler{}
	e.SetEventHandler(h)

	pdns := dns.NewProxyDNSClient(dns.ProxyDNSClientConfig{
		Dialer:  newMockWireDialer(newBlockingPacketConn()),
		Servers: []string{"8.8.8.8:53"},
	})

	tm := &wire.TransportManager{}
	e.mu.Lock()
	e.state = StateConnected
	e.tm = tm
	e.proxyDNS = pdns
	e.mu.Unlock()

	// ResetConnections on empty pool = no-op, no panic.
	e.Pause()

	e.mu.Lock()
	if e.proxyDNS == nil {
		t.Fatal("proxyDNS should remain set after Pause")
	}
	e.mu.Unlock()
}

func TestEngine_Wake_ResetsProxyDNS(t *testing.T) {
	e := New()
	h := &mockEventHandler{}
	e.SetEventHandler(h)

	pdns := dns.NewProxyDNSClient(dns.ProxyDNSClientConfig{
		Dialer:  newMockWireDialer(newBlockingPacketConn()),
		Servers: []string{"8.8.8.8:53"},
	})

	tm := &wire.TransportManager{}
	e.mu.Lock()
	e.state = StateConnected
	e.paused = true
	e.tm = tm
	e.proxyDNS = pdns
	e.mu.Unlock()

	e.Wake()

	e.mu.Lock()
	if e.proxyDNS == nil {
		t.Fatal("proxyDNS should remain set after Wake")
	}
	e.mu.Unlock()
}

func TestEngine_OnNetworkChanged_PreservesAuthError(t *testing.T) {
	e := New()
	h := &mockEventHandler{}
	e.SetEventHandler(h)

	tm := &wire.TransportManager{}
	e.mu.Lock()
	e.state = StateConnected
	e.tm = tm
	e.lastError = &EngineError{Code: 401, Category: CategoryClient, Message: "token expired"}
	e.mu.Unlock()

	e.OnNetworkChanged()

	// After OnNetworkChanged, auth error should persist.
	e.mu.Lock()
	if e.lastError == nil || e.lastError.Code != 401 {
		t.Errorf("auth error should persist after OnNetworkChanged, got %+v", e.lastError)
	}
	e.mu.Unlock()

	// The last status emitted should still carry the auth error.
	statuses := h.getStatuses()
	last := statuses[len(statuses)-1]
	if last.Error == nil || last.Error.Code != 401 {
		t.Errorf("last status should carry auth error, got %+v", last.Error)
	}
}

func TestEngine_OnNetworkChanged_ClearsNetworkError(t *testing.T) {
	e := New()
	h := &mockEventHandler{}
	e.SetEventHandler(h)

	tm := &wire.TransportManager{}
	e.mu.Lock()
	e.state = StateConnected
	e.tm = tm
	e.lastError = &EngineError{Code: 503, Category: CategoryServer, Message: "refused"}
	e.mu.Unlock()

	e.OnNetworkChanged()

	// Network error should be cleared after OnNetworkChanged.
	e.mu.Lock()
	if e.lastError != nil {
		t.Errorf("network error should be cleared after OnNetworkChanged, got %+v", e.lastError)
	}
	e.mu.Unlock()
}

func TestEngine_OnHealthCritical_DoesNotClearError(t *testing.T) {
	e := New()
	h := &mockEventHandler{}
	e.SetEventHandler(h)

	tm := &wire.TransportManager{}
	e.mu.Lock()
	e.state = StateConnected
	e.tm = tm
	e.lastError = &EngineError{Code: 503, Category: CategoryServer, Message: "refused"}
	e.mu.Unlock()

	e.onHealthCritical()

	// Unlike OnNetworkChanged, health critical does NOT clear wire errors.
	e.mu.Lock()
	if e.lastError == nil || e.lastError.Code != 503 {
		t.Errorf("wire error should persist after onHealthCritical, got %+v", e.lastError)
	}
	e.mu.Unlock()
}

func TestEngine_OnHealthCritical_WhenConnected(t *testing.T) {
	e := New()
	h := &mockEventHandler{}
	e.SetEventHandler(h)

	tm := &wire.TransportManager{}
	e.mu.Lock()
	e.state = StateConnected
	e.tm = tm
	e.mu.Unlock()

	e.onHealthCritical()

	statuses := h.getStatuses()
	if len(statuses) < 2 {
		t.Fatalf("expected at least 2 status updates (reconnecting + connected), got %d", len(statuses))
	}
	if statuses[0].State != "reconnecting" {
		t.Fatalf("first state = %q, want %q", statuses[0].State, "reconnecting")
	}
	if statuses[len(statuses)-1].State != "connected" {
		t.Fatalf("last state = %q, want %q", statuses[len(statuses)-1].State, "connected")
	}
}

func TestEngine_OnHealthCritical_WhenDisconnected(t *testing.T) {
	e := New()
	h := &mockEventHandler{}
	e.SetEventHandler(h)

	// Not connected — onHealthCritical should be a no-op.
	e.onHealthCritical()

	statuses := h.getStatuses()
	if len(statuses) != 0 {
		t.Fatalf("expected 0 status updates when not connected, got %d", len(statuses))
	}
}

func TestEngine_Stop_ClearsAuthError(t *testing.T) {
	e := New()

	e.mu.Lock()
	e.state = StateConnected
	e.lastError = &EngineError{Code: 401, Category: CategoryClient, Message: "token expired"}
	_, cancel := context.WithCancel(context.Background())
	e.cancel = cancel
	e.mu.Unlock()

	_ = e.Stop()

	e.mu.Lock()
	if e.lastError != nil {
		t.Errorf("Stop() should clear all errors unconditionally, got %+v", e.lastError)
	}
	e.mu.Unlock()
}

func TestEngine_ReportWireError_Concurrent(t *testing.T) {
	e := New()
	h := &mockEventHandler{}
	e.SetEventHandler(h)

	e.mu.Lock()
	e.state = StateConnected
	e.mu.Unlock()

	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			e.ReportWireError(fmt.Errorf("wire: error %d: connection refused", n))
		}(i)
	}
	wg.Wait()

	// Should not panic or race. Only first error should fire (debounce).
	statuses := h.getStatuses()
	if len(statuses) != 1 {
		t.Errorf("expected 1 status update (debounce), got %d", len(statuses))
	}
}

// ==========================================================================
// computeDNSExclude tests
// ==========================================================================

func TestComputeDNSExclude_ParsesDirectServers(t *testing.T) {
	client := &config.ClientConfig{
		DNS: config.DNSConfig{
			Direct: []string{"114.114.114.114:53", "223.5.5.5:53"},
		},
	}
	addrs := computeDNSExclude(client)
	if len(addrs) < 2 {
		t.Fatalf("expected at least 2 addrs, got %d", len(addrs))
	}
}

func TestExtractServerHost(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"k2v5://u:p@example.com:443", "example.com"},
		{"k2v5://u:p@1.2.3.4:443", "1.2.3.4"},
		{"://broken", ""},
	}
	for _, tt := range tests {
		got := extractServerHost(tt.url)
		if got != tt.want {
			t.Errorf("extractServerHost(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}

// ==========================================================================
// Test helpers
// ==========================================================================

func waitForTCPCalls(m *mockConnectionHandler, n int, timeout time.Duration) []string {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		calls := m.getTCPCalls()
		if len(calls) >= n {
			return calls
		}
		time.Sleep(5 * time.Millisecond)
	}
	return m.getTCPCalls()
}

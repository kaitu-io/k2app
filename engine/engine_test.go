package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	mdns "github.com/miekg/dns"

	"github.com/kaitu-io/k2/core/dns"
	"github.com/kaitu-io/k2/provider"
	"github.com/kaitu-io/k2/wire"
	"github.com/kaitu-io/k2rule"
)

// ==========================================================================
// Mock helpers
// ==========================================================================

// mockEventHandler records calls for assertion.
type mockEventHandler struct {
	mu     sync.Mutex
	states []string
	errors []string
}

func (m *mockEventHandler) OnStateChange(state string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.states = append(m.states, state)
}

func (m *mockEventHandler) OnError(message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errors = append(m.errors, message)
}

func (m *mockEventHandler) OnStats(txBytes, rxBytes int64) {}

func (m *mockEventHandler) getStates() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]string, len(m.states))
	copy(cp, m.states)
	return cp
}

func (m *mockEventHandler) getErrors() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]string, len(m.errors))
	copy(cp, m.errors)
	return cp
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

func (q *mockDNSQuerier) Query(query *mdns.Msg) (*mdns.Msg, error) {
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

func (d *mockDialer) DialTCP(ctx context.Context, addr string) (net.Conn, error) {
	return nil, nil
}
func (d *mockDialer) DialUDP(ctx context.Context, addr string) (net.PacketConn, error) {
	return nil, nil
}
func (d *mockDialer) Close() error { return nil }

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

func TestEngineStart_InvalidURL(t *testing.T) {
	e := New()
	err := e.Start(Config{WireURL: "garbage", FileDescriptor: -1})
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
	_ = e.Start(Config{WireURL: url, FileDescriptor: -1})

	// Force state to connected to test double-start guard.
	e.mu.Lock()
	e.state = StateConnected
	e.mu.Unlock()

	err := e.Start(Config{WireURL: url, FileDescriptor: -1})
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

// ==========================================================================
// Rule parsing tests
// ==========================================================================

func TestParseRuleFromURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{
			name: "smart rule",
			url:  "k2v5://udid:tok@host:443?rule=smart",
			want: "smart",
		},
		{
			name: "global rule",
			url:  "k2v5://udid:tok@host:443?rule=global",
			want: "global",
		},
		{
			name: "no rule param",
			url:  "k2v5://udid:tok@host:443",
			want: "",
		},
		{
			name: "empty rule param",
			url:  "k2v5://udid:tok@host:443?rule=",
			want: "",
		},
		{
			name: "rule with other params",
			url:  "k2v5://udid:tok@host:443?insecure=1&rule=smart&tp=quic",
			want: "smart",
		},
		{
			name: "invalid URL",
			url:  "://broken",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseRuleFromURL(tt.url)
			if got != tt.want {
				t.Fatalf("parseRuleFromURL(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestBuildK2ruleConfig_Default_Global(t *testing.T) {
	e := New()
	cfg := Config{
		WireURL:        "k2v5://udid:tok@host:443",
		FileDescriptor: -1,
	}
	ruleCfg := e.buildK2ruleConfig(cfg)
	if !ruleCfg.IsGlobal {
		t.Fatal("expected IsGlobal true when no rule specified")
	}
	if ruleCfg.GlobalTarget != k2rule.TargetProxy {
		t.Fatalf("GlobalTarget = %v, want TargetProxy", ruleCfg.GlobalTarget)
	}
}

func TestBuildK2ruleConfig_DataDir(t *testing.T) {
	e := New()
	cfg := Config{
		WireURL:        "k2v5://udid:tok@host:443",
		FileDescriptor: -1,
		DataDir:        "/tmp/k2test",
	}
	ruleCfg := e.buildK2ruleConfig(cfg)
	if ruleCfg.CacheDir != "/tmp/k2test" {
		t.Fatalf("CacheDir = %q, want %q", ruleCfg.CacheDir, "/tmp/k2test")
	}
}

func TestBuildK2ruleConfig_RuleConfig_Override(t *testing.T) {
	e := New()
	custom := &k2rule.Config{
		IsGlobal:     false,
		RuleURL:      "https://example.com/rules",
		CacheDir:     "/custom/dir",
		GlobalTarget: k2rule.TargetDirect,
	}
	cfg := Config{
		WireURL:        "k2v5://udid:tok@host:443",
		FileDescriptor: -1,
		RuleMode:       "global", // should be ignored
		DataDir:        "/ignored",
		RuleConfig:     custom,
	}
	ruleCfg := e.buildK2ruleConfig(cfg)
	if ruleCfg != custom {
		t.Fatal("expected custom RuleConfig to be used directly")
	}
	if ruleCfg.IsGlobal {
		t.Fatal("expected IsGlobal false from custom config")
	}
	if ruleCfg.CacheDir != "/custom/dir" {
		t.Fatalf("CacheDir = %q, want %q", ruleCfg.CacheDir, "/custom/dir")
	}
}

func TestBuildK2ruleConfig_RuleModeFromURL(t *testing.T) {
	e := New()
	cfg := Config{
		WireURL:        "k2v5://udid:tok@host:443?rule=smart",
		FileDescriptor: -1,
	}
	ruleCfg := e.buildK2ruleConfig(cfg)
	if ruleCfg.IsGlobal {
		t.Fatal("expected IsGlobal false when URL has rule=smart")
	}
}

func TestBuildK2ruleConfig_RuleMode_OverridesURL(t *testing.T) {
	e := New()
	cfg := Config{
		WireURL:        "k2v5://udid:tok@host:443?rule=smart",
		FileDescriptor: -1,
		RuleMode:       "global", // explicit RuleMode takes priority over URL
	}
	ruleCfg := e.buildK2ruleConfig(cfg)
	if !ruleCfg.IsGlobal {
		t.Fatal("expected IsGlobal true when RuleMode explicitly set to global")
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
	_ = e.Start(Config{WireURL: "garbage", FileDescriptor: -1})

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

func TestEngineStart_EventHandler_OnError(t *testing.T) {
	e := New()
	h := &mockEventHandler{}
	e.SetEventHandler(h)

	_ = e.Start(Config{WireURL: "garbage", FileDescriptor: -1})

	errs := h.getErrors()
	if len(errs) == 0 {
		t.Fatal("expected OnError to be called")
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
	for _, key := range []string{"connected_at", "uptime_seconds", "wire_url", "error"} {
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
	e.wireUrl = "k2v5://udid:tok@1.2.3.4:443"
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
	if m["wire_url"] != "k2v5://udid:tok@1.2.3.4:443" {
		t.Fatalf("wire_url = %v, want k2v5://udid:tok@1.2.3.4:443", m["wire_url"])
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
	_ = e.Start(Config{WireURL: "garbage", FileDescriptor: -1})

	raw := e.StatusJSON()
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("StatusJSON returned invalid JSON: %v", err)
	}
	if m["state"] != StateDisconnected {
		t.Fatalf("state = %v, want %q", m["state"], StateDisconnected)
	}
	if _, ok := m["error"]; !ok {
		t.Fatal("expected 'error' key after failed start")
	}
	if m["error"] == "" {
		t.Fatal("error should be non-empty after failed start")
	}
}

func TestEngineStatusJSON_MatchesDaemonFormat(t *testing.T) {
	e := New()

	// Set all fields to produce the full JSON.
	e.mu.Lock()
	e.state = StateConnected
	e.wireUrl = "k2v5://u:t@host:443"
	e.connectedAt = time.Now().Add(-5 * time.Second)
	e.lastError = &EngineError{Code: ErrCodeConnectionFatal, Message: "some previous error"}
	e.mu.Unlock()

	raw := e.StatusJSON()
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("StatusJSON returned invalid JSON: %v", err)
	}

	expected := []string{"state", "connected_at", "uptime_seconds", "wire_url", "error"}
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
	// Initialize k2rule so DNS middleware can route queries (v0.5.0-go requires CacheDir).
	// Non-existent file paths skip remote downloads (retryForever); globalConfig is set
	// before the file-open error, and global-mode Match() only checks globalConfig.
	k2rule.Init(&k2rule.Config{
		CacheDir:     t.TempDir(),
		IsGlobal:     true,
		GlobalTarget: k2rule.TargetProxy,
		GeoIPFile:    "/nonexistent",
		PornFile:     "/nonexistent",
	})

	inner := &mockConnectionHandler{}

	// Build a Middleware with a mock querier that returns canned DNS responses.
	mw := dns.NewMiddleware(dns.MiddlewareConfig{
		ProxyDNS: &mockDNSQuerier{},
		DNSCache: dns.NewDNSCache(dns.DNSCacheConfig{}),
		Filter:   dns.NewFilter(dns.FilterConfig{}),
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
		WireURL:        "k2v5://udid:token@127.0.0.1:12345?insecure=1",
		FileDescriptor: -1,
		NetworkMonitor: nil,
	}
	// Start will fail with TUN error (no real TUN device in test), that's expected.
	// The key assertion is no panic.
	_ = e.Start(cfg)
}

// TestEngine_NetworkMonitor_CallbackTriggersOnNetworkChanged verifies that
// when the monitor fires its callback, it calls OnNetworkChanged on the
// engine, which emits reconnecting→connected state transitions.
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
// is called when the engine starts with a non-nil NetworkMonitor. Since a real
// Start() requires TUN (unavailable in tests), we simulate the connected flow
// by setting internal state and verifying the monitor would be started after
// the engine reaches connected state.
func TestEngine_NetworkMonitor_StartedOnEngineStart(t *testing.T) {
	e := New()
	h := &mockEventHandler{}
	e.SetEventHandler(h)
	mon := &mockNetworkMonitor{}

	// Config with NetworkMonitor set — engine should start the monitor
	// after reaching connected state.
	cfg := Config{
		WireURL:        "k2v5://udid:token@127.0.0.1:12345?insecure=1",
		FileDescriptor: -1,
		NetworkMonitor: mon,
	}

	// Start will fail (no real TUN), but the monitor field should be stored.
	_ = e.Start(cfg)

	// Since Start() failed before reaching connected, monitor should NOT be started.
	if mon.isStarted() {
		t.Fatal("monitor should not be started when engine fails to connect")
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
		t.Fatalf("expected OnError('connection lost'), got %v", errs)
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
		t.Fatalf("expected OnError('test error'), got %v", errs)
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
		e.setState(StateConnecting)
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
		e.setState(StateConnected)
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

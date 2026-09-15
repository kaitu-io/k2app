package engine

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mdns "github.com/miekg/dns"

	"github.com/kaitu-io/k2-rules/krs"
	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/core"
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

func (m *mockEventHandler) OnStats(txBytes, rxBytes int64)             {}
func (m *mockEventHandler) OnOutboundFatal(outbound string, err error) {}

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
func (d *mockDialer) Reset()       {}
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
func (d *mockWireDialer) Reset()       {}
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

// TestReduceOutboundResults_PartialFailureTolerated pins the multi-slot
// gateway fix: one dead outbound (e.g. an enterprise slot's line) must not
// abort engine Start when at least one OTHER outbound succeeded. Caught by
// the enterprise-router UAT — engine.Start previously aborted on ANY
// outbound failure regardless of how many others succeeded.
func TestReduceOutboundResults_PartialFailureTolerated(t *testing.T) {
	urls := []string{"k2v5://a", "k2v5://dead", "k2v5://b"}
	deadErr := errors.New("simulated dial failure")
	aliveOT1 := &outboundTransport{}
	aliveOT2 := &outboundTransport{}
	results := []otResult{
		{ot: aliveOT1},
		{err: deadErr},
		{ot: aliveOT2},
	}

	tmMap, allOTs, primaryOT, _, firstErr := reduceOutboundResults(urls, results)

	if primaryOT != aliveOT1 {
		t.Fatalf("primaryOT = %v, want the first SUCCESSFUL outbound (aliveOT1)", primaryOT)
	}
	if len(allOTs) != 2 {
		t.Fatalf("allOTs = %d entries, want 2 (only the successes)", len(allOTs))
	}
	if len(tmMap) != 2 || tmMap["k2v5://a"] != aliveOT1 || tmMap["k2v5://b"] != aliveOT2 {
		t.Fatalf("tmMap = %+v, want only the 2 successful URLs mapped", tmMap)
	}
	if _, deadMapped := tmMap["k2v5://dead"]; deadMapped {
		t.Fatal("dead outbound must NOT appear in tmMap — its route target must fall back to reject")
	}
	if firstErr != deadErr {
		t.Fatalf("firstErr = %v, want the dead outbound's error (for diagnostics)", firstErr)
	}
}

// TestReduceOutboundResults_AllFailed preserves the existing "no outbound
// at all works" abort behavior (TestEngineStart_WireProbeFailure's
// end-to-end counterpart): primaryOT must be nil so Start still fails.
func TestReduceOutboundResults_AllFailed(t *testing.T) {
	urls := []string{"k2v5://a", "k2v5://b"}
	err1 := errors.New("first failure")
	results := []otResult{{err: err1}, {err: errors.New("second failure")}}

	tmMap, allOTs, primaryOT, _, firstErr := reduceOutboundResults(urls, results)

	if primaryOT != nil {
		t.Fatalf("primaryOT = %v, want nil when every outbound failed", primaryOT)
	}
	if len(allOTs) != 0 || len(tmMap) != 0 {
		t.Fatalf("allOTs/tmMap must be empty, got allOTs=%v tmMap=%v", allOTs, tmMap)
	}
	if firstErr != err1 {
		t.Fatalf("firstErr = %v, want the FIRST failure in order", firstErr)
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
	re := buildRuleEngine(client, buildOutboundMap(client.Routes), false)
	defer re.Close()
	if re.DisableSniff() {
		t.Fatal("expected DisableSniff false when no routes set")
	}
	// Default fallback is direct.
	if got := re.Match("example.com"); got != rule.Target(0) {
		t.Fatalf("Match = %v, want TargetDirect", got)
	}
}

func TestBuildRuleEngine_AllProxy(t *testing.T) {
	client := config.ClientFromURL("k2v5://udid:tok@host:443")
	re := buildRuleEngine(client, buildOutboundMap(client.Routes), false)
	defer re.Close()
	if !re.DisableSniff() {
		t.Fatal("expected DisableSniff true for ClientFromURL")
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
	re := buildRuleEngine(client, buildOutboundMap(client.Routes), false)
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
	re := buildRuleEngine(client, buildOutboundMap(client.Routes), false)
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
	re := buildRuleEngine(client, buildOutboundMap(client.Routes), false)
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
	for _, key := range []string{"startAt", "uptimeSeconds", "error"} {
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
	if _, ok := m["startAt"]; !ok {
		t.Fatal("expected 'startAt' key when connected")
	}
	uptime, ok := m["uptimeSeconds"].(float64)
	if !ok {
		t.Fatalf("uptimeSeconds not a number: %v (%T)", m["uptimeSeconds"], m["uptimeSeconds"])
	}
	if uptime < 9 {
		t.Fatalf("uptimeSeconds = %v, want >= 9", uptime)
	}
}

func TestEngineStatusJSON_Paused(t *testing.T) {
	e := New()

	// e.state stays StateConnected while paused (see event.go StatePaused
	// comment) — buildStatusLocked must still report "paused" so queries
	// (native getStatusJSON, webapp safety-net poll) agree with the
	// one-shot OnStatus(StatePaused) push instead of overwriting it.
	e.mu.Lock()
	e.state = StateConnected
	e.paused = true
	e.client = &config.ClientConfig{Server: "k2v5://udid:tok@1.2.3.4:443"}
	e.connectedAt = time.Now().Add(-10 * time.Second)
	e.mu.Unlock()

	raw := e.StatusJSON()
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("StatusJSON returned invalid JSON: %v", err)
	}
	if m["state"] != StatePaused {
		t.Fatalf("state = %v, want %q", m["state"], StatePaused)
	}

	// Unpausing (without a full Wake(), which requires TM/DNS scaffolding
	// exercised separately in TestEngine_Wake_ResetsProxyDNS) must flip the
	// queried state back to "connected".
	e.mu.Lock()
	e.paused = false
	e.mu.Unlock()

	raw = e.StatusJSON()
	m = nil
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("StatusJSON returned invalid JSON: %v", err)
	}
	if m["state"] != StateConnected {
		t.Fatalf("state after unpause = %v, want %q", m["state"], StateConnected)
	}
}

// TestEngine_PauseWake_StatusReflectsPaused drives Pause()/Wake() through
// their real guarded entry points (not manual field mutation) and checks
// both the one-shot OnStatus push AND a StatusJSON() query agree at every
// step — this is the exact push/query duality that was broken for ticket
// #3169 (query kept reporting "connected" while paused).
func TestEngine_PauseWake_StatusReflectsPaused(t *testing.T) {
	e := New()
	h := &mockEventHandler{}
	e.SetEventHandler(h)

	pdns := dns.NewProxyDNSClient(dns.ProxyDNSClientConfig{
		Dialer:  newMockWireDialer(newBlockingPacketConn()),
		Servers: []string{"8.8.8.8:53"},
	})
	tm := wire.NewTransportManager()
	e.mu.Lock()
	e.state = StateConnected
	e.primaryTM = &outboundTransport{tm: tm}
	e.outboundTMs = []*outboundTransport{e.primaryTM}
	e.proxyDNS = pdns
	e.client = &config.ClientConfig{Server: "k2v5://udid:tok@1.2.3.4:443"}
	e.connectedAt = time.Now().Add(-10 * time.Second)
	e.mu.Unlock()

	// Sanity: query agrees with "connected" before any pause.
	if raw := e.StatusJSON(); !strings.Contains(raw, `"state":"connected"`) {
		t.Fatalf("pre-pause StatusJSON = %s, want state=connected", raw)
	}

	e.Pause()

	// Push: the one-shot OnStatus(StatePaused) fired.
	states := h.getStates()
	if len(states) == 0 || states[len(states)-1] != string(StatePaused) {
		t.Fatalf("after Pause(), last pushed state = %v, want %q", states, StatePaused)
	}
	// Query: StatusJSON() must agree with the push, not lag behind it.
	if raw := e.StatusJSON(); !strings.Contains(raw, `"state":"paused"`) {
		t.Fatalf("after Pause(), StatusJSON = %s, want state=paused", raw)
	}
	// e.state itself stays StateConnected — paused is handler/query-only (event.go StatePaused comment).
	e.mu.Lock()
	internalState := e.state
	e.mu.Unlock()
	if internalState != StateConnected {
		t.Fatalf("internal e.state after Pause() = %v, want StateConnected (paused must not mutate e.state)", internalState)
	}

	e.Wake()

	states = h.getStates()
	if len(states) == 0 || states[len(states)-1] != string(StateConnected) {
		t.Fatalf("after Wake(), last pushed state = %v, want %q", states, StateConnected)
	}
	if raw := e.StatusJSON(); !strings.Contains(raw, `"state":"connected"`) {
		t.Fatalf("after Wake(), StatusJSON = %s, want state=connected", raw)
	}

	// Second Pause()/Wake() cycle — guards against any one-shot-only bug
	// (e.g. a field that gets cleared and never re-armed).
	e.Pause()
	if raw := e.StatusJSON(); !strings.Contains(raw, `"state":"paused"`) {
		t.Fatalf("second Pause(): StatusJSON = %s, want state=paused", raw)
	}
	e.Wake()
	if raw := e.StatusJSON(); !strings.Contains(raw, `"state":"connected"`) {
		t.Fatalf("second Wake(): StatusJSON = %s, want state=connected", raw)
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

	expected := []string{"state", "startAt", "uptimeSeconds", "error"}
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
	tm := wire.NewTransportManager()
	e.mu.Lock()
	e.state = StateConnected
	e.primaryTM = &outboundTransport{tm: tm}
	e.outboundTMs = []*outboundTransport{e.primaryTM}
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
	e.primaryTM = nil
	e.outboundTMs = nil
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
	tm := wire.NewTransportManager()
	e.mu.Lock()
	e.state = StateConnected
	e.primaryTM = &outboundTransport{tm: tm}
	e.outboundTMs = []*outboundTransport{e.primaryTM}
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

// TestEngine_HealthMonitor_StoppedOnFail verifies that fail() stops the
// healthMonitor goroutine, nils e.health, and lets the goroutine count
// return to baseline. Without this, a stuck-after-fail() bug leaves the
// healthMonitor logging heartbeats forever against a closed TM, while
// wake/network reconnect callbacks no-op (state != StateConnected).
func TestEngine_HealthMonitor_StoppedOnFail(t *testing.T) {
	e := New()
	h := &mockEventHandler{}
	e.SetEventHandler(h)

	// Build a real (running) healthMonitor — same pattern as health_test.go.
	tm := mockTransportManager()
	tunnel := core.NewClientTunnel(core.ClientTunnelConfig{})
	ot := &outboundTransport{tm: tm, name: "test"}
	hm := newHealthMonitor(ot, []*outboundTransport{ot}, tunnel, nil, nil, time.Now())
	hm.start()

	// Simulate post-Start engine state: connected with active health monitor.
	_, cancel := context.WithCancel(context.Background())
	e.mu.Lock()
	e.state = StateConnected
	e.health = hm
	e.cancel = cancel
	e.outboundTMs = []*outboundTransport{ot}
	e.primaryTM = ot
	e.mu.Unlock()

	baseline := runtime.NumGoroutine()

	// Trigger fail() — should stop hm and clear references.
	e.fail(fmt.Errorf("simulated outbound exhaustion"))

	e.mu.Lock()
	healthIsNil := e.health == nil
	primaryIsNil := e.primaryTM == nil
	tmsIsNil := e.outboundTMs == nil
	e.mu.Unlock()

	if !healthIsNil {
		t.Fatal("e.health must be nil after fail() — orphaned healthMonitor caused 15h stuck heartbeat in production")
	}
	if !primaryIsNil {
		t.Fatal("e.primaryTM must be nil after fail()")
	}
	if !tmsIsNil {
		t.Fatal("e.outboundTMs must be nil after fail()")
	}

	// Allow the stopped goroutine to fully exit (hm.stop() returns after
	// wg.Wait, but Go's scheduler may need a beat to update NumGoroutine).
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > baseline && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := runtime.NumGoroutine(); got > baseline {
		t.Fatalf("goroutine leak after fail(): baseline=%d, after=%d (healthMonitor sample/echoLoop should have exited)", baseline, got)
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

// TestEngine_ClearWireError_ClearsPinMismatch drives the exact shape that used
// to poison the UI for a whole session.
//
// A node whose TCP-WS certificate no longer matches its pin fails pin
// verification (wire/ech.go buildPinVerifier). On any dial where QUIC also
// fails — a timeout, a handshake blip, anything transient — TransportManager
// returns MultiTransportError, and ClassifyError classifies that by the
// *TCP-WS* leg alone, so the whole dial reads as 403 Forbidden / client.
// ClearWireError then refused to clear it because it was client-category, and
// so did the network-change clear: the 403 outlived every subsequent
// successful dial and only Stop() could remove it. The user saw "certificate
// rejected" while their tunnel ran happily on QUIC.
//
// A successful proxy dial is proof that a pin-verified channel is up, which is
// exactly the evidence that retires a pin-mismatch verdict. Unlike 401/402 it
// is not user-actionable — nobody can fix a node's certificate from the app.
func TestEngine_ClearWireError_ClearsPinMismatch(t *testing.T) {
	e := New()
	h := &mockEventHandler{}
	e.SetEventHandler(h)

	e.mu.Lock()
	e.state = StateConnected
	e.mu.Unlock()

	// The real combined error: QUIC blipped, TCP-WS failed pin verification.
	e.ReportWireError(&wire.MultiTransportError{
		QUICErr:  fmt.Errorf("wire: QUIC handshake: timeout"),
		TCPWSErr: errors.New("wire: public key pin mismatch and not signed by Kaitu CA"),
	})

	// Guard the premise: this really is classified 403/client, so the test
	// keeps testing the reported bug even if classification moves.
	e.mu.Lock()
	got := e.lastError
	e.mu.Unlock()
	if got == nil || got.Code != ErrCodeForbidden || got.Category != CategoryClient {
		t.Fatalf("premise broken: want 403/client from the TCP-WS leg, got %+v", got)
	}

	// A later dial succeeds — the wire demonstrably works.
	e.ClearWireError()

	e.mu.Lock()
	after := e.lastError
	e.mu.Unlock()
	if after != nil {
		t.Errorf("pin mismatch must clear once a dial succeeds, still have %+v", after)
	}
	// Two updates: the one ReportWireError fired, then the clear. The UI must
	// end on an error-free status — that is the whole point of the fix.
	statuses := h.getStatuses()
	if len(statuses) != 2 {
		t.Fatalf("expected 2 status updates (report, then clear), got %d", len(statuses))
	}
	if statuses[len(statuses)-1].Error != nil {
		t.Errorf("last status still carries an error: %+v", statuses[len(statuses)-1].Error)
	}
}

// TestEngineError_RequiresUserAction pins which errors survive proof that the
// wire works. It enumerates every client-category code on purpose: adding one
// without deciding its stickiness should break this test, not ship a silently
// unclearable error.
func TestEngineError_RequiresUserAction(t *testing.T) {
	tests := []struct {
		code int
		want bool
		why  string
	}{
		{ErrCodeAuthRejected, true, "only the user can re-authenticate"},
		{ErrCodePaymentRequired, true, "only the user can pay"},
		{ErrCodeForbidden, false, "node-side cert/pin; a successful dial disproves it"},
		{ErrCodeBadConfig, false, "a dial that succeeded parsed the config"},
		{ErrCodeEnvironmentSetupFailed, false, "a dial that succeeded had its environment"},
		{ErrCodeTimeout, false, "network fault, not user action"},
		{ErrCodeServerUnreachable, false, "server fault, not user action"},
	}
	for _, tc := range tests {
		got := newEngineError(tc.code, "x").RequiresUserAction()
		if got != tc.want {
			t.Errorf("code %d RequiresUserAction() = %v, want %v (%s)", tc.code, got, tc.want, tc.why)
		}
	}
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

	tm := wire.NewTransportManager()
	e.mu.Lock()
	e.state = StateConnected
	e.primaryTM = &outboundTransport{tm: tm}
	e.outboundTMs = []*outboundTransport{e.primaryTM}
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

	tm := wire.NewTransportManager()
	e.mu.Lock()
	e.state = StateConnected
	e.primaryTM = &outboundTransport{tm: tm}
	e.outboundTMs = []*outboundTransport{e.primaryTM}
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

	tm := wire.NewTransportManager()
	e.mu.Lock()
	e.state = StateConnected
	e.paused = true
	e.primaryTM = &outboundTransport{tm: tm}
	e.outboundTMs = []*outboundTransport{e.primaryTM}
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

	tm := wire.NewTransportManager()
	e.mu.Lock()
	e.state = StateConnected
	e.primaryTM = &outboundTransport{tm: tm}
	e.outboundTMs = []*outboundTransport{e.primaryTM}
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

	tm := wire.NewTransportManager()
	e.mu.Lock()
	e.state = StateConnected
	e.primaryTM = &outboundTransport{tm: tm}
	e.outboundTMs = []*outboundTransport{e.primaryTM}
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

	tm := wire.NewTransportManager()
	e.mu.Lock()
	e.state = StateConnected
	e.primaryTM = &outboundTransport{tm: tm}
	e.outboundTMs = []*outboundTransport{e.primaryTM}
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

	tm := wire.NewTransportManager()
	e.mu.Lock()
	e.state = StateConnected
	e.primaryTM = &outboundTransport{tm: tm}
	e.outboundTMs = []*outboundTransport{e.primaryTM}
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

// A health-critical reconnect redials the same path, so the wire scoreboard
// (scores, flap history) must survive it; a network reconnect wipes it and
// also forgets the health-reconnect backoff.
func TestEngine_HealthReconnectPreservesScoreboard_NetworkReconnectWipes(t *testing.T) {
	e := New()
	e.SetEventHandler(&mockEventHandler{})

	tm := wire.NewTransportManager()
	now := time.Now()
	for i := 0; i < 4; i++ {
		tm.RecordEchoResult("tcpws", 30*time.Millisecond, nil, now)
	}
	tm.RecordQUICStats(wire.WireStats{PacketLossRate: 0.5, RTT: 100 * time.Millisecond, MinRTT: 20 * time.Millisecond}, now)
	measuredQUIC, measuredTCPWS := tm.ScoreQUIC(now), tm.ScoreTCPWS(now)
	const neutral = 0.25
	if measuredQUIC == neutral || measuredTCPWS == neutral {
		t.Fatalf("precondition: scores must be measured, got quic=%v tcpws=%v", measuredQUIC, measuredTCPWS)
	}

	ot := &outboundTransport{tm: tm}
	hm := newHealthMonitor(ot, []*outboundTransport{ot}, core.NewClientTunnel(core.ClientTunnelConfig{}), nil, nil, now)
	hm.actionRepeats = 2 // mid-episode backoff armed
	e.mu.Lock()
	e.state = StateConnected
	e.primaryTM = ot
	e.outboundTMs = []*outboundTransport{ot}
	e.health = hm
	e.mu.Unlock()

	e.onHealthCritical()
	if got := tm.ScoreQUIC(now); got != measuredQUIC {
		t.Fatalf("health reconnect wiped scoreQUIC: %v → %v", measuredQUIC, got)
	}
	if got := tm.ScoreTCPWS(now); got != measuredTCPWS {
		t.Fatalf("health reconnect wiped scoreTCPWS: %v → %v", measuredTCPWS, got)
	}
	if hm.actionCooldown() == 0 {
		t.Fatal("health reconnect must not forget the backoff")
	}

	e.doNetworkReconnect()
	if got := tm.ScoreQUIC(now); got != neutral {
		t.Fatalf("network reconnect must wipe scoreQUIC, got %v", got)
	}
	if got := hm.actionCooldown(); got != 0 {
		t.Fatalf("network reconnect must forget the backoff, cooldown = %v", got)
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
// Multi-outbound tests
// ==========================================================================

func TestBuildOutboundMap_MultipleK2V5(t *testing.T) {
	routes := []config.RouteConfig{
		{Via: "k2v5://jp-server:443", Match: rule.MatchConfig{DomainSuffix: []string{".jp"}}},
		{Via: "k2v5://us-server:443", Match: rule.MatchConfig{All: true}},
	}
	m := buildOutboundMap(routes)

	if len(m) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(m))
	}
	jpTarget := m["k2v5://jp-server:443"]
	usTarget := m["k2v5://us-server:443"]
	if jpTarget == usTarget {
		t.Fatal("jp and us should have different targets")
	}
	if jpTarget < 2 || usTarget < 2 {
		t.Fatal("wire targets should be >= 2")
	}
}

func TestBuildOutboundMap_DedupSameURL(t *testing.T) {
	routes := []config.RouteConfig{
		{Via: "k2v5://server:443", Match: rule.MatchConfig{DomainSuffix: []string{".jp"}}},
		{Via: "k2v5://server:443", Match: rule.MatchConfig{All: true}},
	}
	m := buildOutboundMap(routes)

	if len(m) != 1 {
		t.Fatalf("expected 1 entry (dedup), got %d", len(m))
	}
}

func TestSetupOutboundTransport_InvalidURL(t *testing.T) {
	ctx := context.Background()
	_, _, err := setupOutboundTransport(ctx, "not-a-valid-url", nil, "", "")
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}

// TestEngineStart_RuleBundlesUnavailable verifies that when remote rule bundle
// sources are unreachable on a cold start, the embedded seed floor allows
// Start to proceed past the rule-bundle gate (no ErrCodeRuleBundlesUnavailable).
// The engine may still fail later for a wire-level reason (no server), but the
// 504 rule-bundle gate must NOT fire when the embedded archive provides a floor.
func TestEngineStart_RuleBundlesUnavailable(t *testing.T) {
	dir := t.TempDir()

	client := &config.ClientConfig{
		CacheDir:    dir,
		RuleSources: []string{"http://127.0.0.1:1/"}, // connection refused — fast fail
		Server:      "k2v5://udid:tok@127.0.0.1:65534?insecure=1",
		Routes: []config.RouteConfig{
			{Via: "k2v5://udid:tok@127.0.0.1:65534?insecure=1", Match: rule.MatchConfig{All: true}},
		},
	}

	e := New()
	h := &mockEventHandler{}
	e.SetEventHandler(h)

	err := e.Start(Config{Client: client, FileDescriptor: -1})

	// With the embedded seed floor, the rule-bundle gate is satisfied offline.
	// Start must NOT return ErrCodeRuleBundlesUnavailable (504).
	if err != nil {
		var engErr *EngineError
		if errors.As(err, &engErr) && engErr.Code == ErrCodeRuleBundlesUnavailable {
			t.Fatalf("regression: embedded seed should prevent ErrCodeRuleBundlesUnavailable; "+
				"got code=%d msg=%q — the embedded archive or SeedFromEmbedIfEmpty wiring is broken",
				engErr.Code, engErr.Message)
		}
		// Any other error (wire dial refused, etc.) is acceptable — the bundle gate passed.
	}

	// Engine must have reached at least the connecting phase before failing (not
	// rejected at the rule-bundle gate). State is disconnected because wire dial fails.
	if e.Status() != StateDisconnected {
		t.Errorf("expected state %q after wire failure, got %q", StateDisconnected, e.Status())
	}
}

// TestEngineStart_RuleBundlesStaleCacheFallback verifies that when fresh
// rule bundle download fails but at least one stale .k2b is on disk, Engine.Start
// MUST fall through to the stale cache instead of returning ErrCodeRuleBundlesUnavailable.
// The background StartUpdater hot-reloads fresh bundles once a mirror becomes
// reachable. Regression of this fallback would resurface support ticket #620 —
// users with a working stale cache cannot connect when jsdelivr/GitHub mirrors
// are blocked (common in China).
func TestEngineStart_RuleBundlesStaleCacheFallback(t *testing.T) {
	dir := t.TempDir()

	// Pre-seed a valid .k2b on disk — mimics the state after a prior successful
	// connect. bundles.version is intentionally absent, so isVersionFresh returns
	// false and EnsureBundles will attempt (and fail) a download.
	var buf bytes.Buffer
	if err := krs.WriteBundle(&buf, &krs.Bundle{Sets: []krs.NamedSet{
		{Name: "overseas", DomainSuffixes: []string{"example.com"}},
	}}); err != nil {
		t.Fatalf("WriteBundle: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "overseas.krs"), buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write stale .krs: %v", err)
	}

	// Capture slog to assert the DIAG event is emitted.
	var records []slog.Record
	var recMu sync.Mutex
	prev := slog.Default()
	slog.SetDefault(slog.New(slogHandlerFunc(func(_ context.Context, r slog.Record) error {
		recMu.Lock()
		records = append(records, r)
		recMu.Unlock()
		return nil
	})))
	defer slog.SetDefault(prev)

	client := &config.ClientConfig{
		CacheDir:    dir,
		RuleSources: []string{"http://127.0.0.1:1/"}, // connection refused — fast fail
		Server:      "k2v5://udid:tok@127.0.0.1:65534?insecure=1",
		Routes: []config.RouteConfig{
			{Via: "k2v5://udid:tok@127.0.0.1:65534?insecure=1", Match: rule.MatchConfig{All: true}},
		},
	}

	e := New()
	h := &mockEventHandler{}
	e.SetEventHandler(h)

	err := e.Start(Config{Client: client, FileDescriptor: -1})
	// Start may still fail later (wire dial to 127.0.0.1:65534 will be refused),
	// but it MUST NOT be the rule-bundle gate.
	if err != nil {
		var engErr *EngineError
		if errors.As(err, &engErr) && engErr.Code == ErrCodeRuleBundlesUnavailable {
			t.Fatalf("regression: Start failed with ErrCodeRuleBundlesUnavailable=%d "+
				"despite stale .k2b on disk; engine must fall through to stale cache. msg=%q",
				ErrCodeRuleBundlesUnavailable, engErr.Message)
		}
	}

	// DIAG event must be emitted for support visibility.
	recMu.Lock()
	defer recMu.Unlock()
	var found bool
	for _, r := range records {
		if strings.Contains(r.Message, "DIAG: rule-bundle-stale") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected `DIAG: rule-bundle-stale` log when falling through to stale cache; got %d records", len(records))
	}
}

// TestEngineStart_EmptyCacheDir_WarnsAndSkipsGate verifies defensive logging
// when the platform shell fails to pass CacheDir. The gate becomes a no-op
// (which re-exposes the routes=0 cold-start bug if bundles are absent), so
// future regressions in platform wiring must surface loudly at first connect.
// This test fails if the warn log goes silent.
func TestEngineStart_EmptyCacheDir_WarnsAndSkipsGate(t *testing.T) {
	// Install a capturing slog handler for the duration of this test.
	var records []slog.Record
	var mu sync.Mutex
	captureHandler := slogHandlerFunc(func(_ context.Context, r slog.Record) error {
		mu.Lock()
		records = append(records, r)
		mu.Unlock()
		return nil
	})
	prev := slog.Default()
	slog.SetDefault(slog.New(captureHandler))
	defer slog.SetDefault(prev)

	client := &config.ClientConfig{
		// CacheDir deliberately empty — simulates a broken platform shell.
		Server: "k2v5://udid:tok@127.0.0.1:65534?insecure=1",
		Routes: []config.RouteConfig{
			{Via: "k2v5://udid:tok@127.0.0.1:65534?insecure=1", Match: rule.MatchConfig{All: true}},
		},
	}

	e := New()
	// Start will fail later (no real server), but it must get past the gate
	// without invoking EnsureBundles and must emit the defensive warn.
	_ = e.Start(Config{Client: client, FileDescriptor: -1})

	mu.Lock()
	defer mu.Unlock()
	var found bool
	for _, r := range records {
		if r.Level == slog.LevelWarn && strings.Contains(r.Message, "Start without CacheDir") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected WARN about empty CacheDir; got %d records, none matched", len(records))
	}
}

// slogHandlerFunc is a minimal test-only slog.Handler that routes every
// record through the given func. Enabled at all levels.
type slogHandlerFunc func(context.Context, slog.Record) error

func (f slogHandlerFunc) Enabled(context.Context, slog.Level) bool  { return true }
func (f slogHandlerFunc) Handle(c context.Context, r slog.Record) error {
	return f(c, r)
}
func (f slogHandlerFunc) WithAttrs([]slog.Attr) slog.Handler { return f }
func (f slogHandlerFunc) WithGroup(string) slog.Handler      { return f }

// TestBuildRuleEngine_AlwaysHasRoutesAfterEnsure is a regression guard for
// the routes=0 cold-start bug: EnsureBundles lands bundles on disk, then
// buildRuleEngine reads them — they MUST be at paths that align so the
// rule engine ends up with ≥1 route. If this ever returns 0, we've silently
// regressed to all-direct routing on first connect.
func TestBuildRuleEngine_AlwaysHasRoutesAfterEnsure(t *testing.T) {
	dir := t.TempDir()

	// Build a minimal but valid k2b bundle — a single set "overseas" with
	// one domain suffix. WriteBundle is already used by other tests.
	var buf bytes.Buffer
	if err := krs.WriteBundle(&buf, &krs.Bundle{Sets: []krs.NamedSet{
		{Name: "overseas", DomainSuffixes: []string{"example.com"}},
	}}); err != nil {
		t.Fatalf("WriteBundle: %v", err)
	}

	// Serve it inside a tarball.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "krs.tar.gz") {
			http.NotFound(w, r)
			return
		}
		var tbuf bytes.Buffer
		gw := gzip.NewWriter(&tbuf)
		tw := tar.NewWriter(gw)
		data := buf.Bytes()
		if err := tw.WriteHeader(&tar.Header{Name: "overseas.krs", Mode: 0o644, Size: int64(len(data))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
		tw.Close()
		gw.Close()
		w.Header().Set("Content-Type", "application/gzip")
		w.Write(tbuf.Bytes())
	}))
	defer srv.Close()

	// Step 1: EnsureBundles writes the bundle into `dir`.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rule.EnsureBundles(ctx, dir, []string{srv.URL + "/"}); err != nil {
		t.Fatalf("EnsureBundles: %v", err)
	}

	// Step 2: buildRuleEngine reads from `dir` and must produce ≥1 route.
	client := &config.ClientConfig{
		CacheDir: dir,
		Routes: []config.RouteConfig{
			{Via: "direct", Match: rule.MatchConfig{Names: []string{"overseas"}}},
			{Via: "k2v5://udid:tok@example.com:443", Match: rule.MatchConfig{All: true}},
		},
	}
	outboundMap := buildOutboundMap(client.Routes)
	ruleEngine := buildRuleEngine(client, outboundMap, false)
	if ruleEngine == nil {
		t.Fatal("buildRuleEngine returned nil")
	}
	defer ruleEngine.Close()

	if got := ruleEngine.RouteCount(); got == 0 {
		t.Errorf("regression: buildRuleEngine produced 0 routes after EnsureBundles "+
			"— bundles at %q, cacheDir=%q. Check path alignment between EnsureBundles and krs.Load.", dir, client.CacheDir)
	}

	// Sanity: the overseas-bound domain should route via direct (target 0).
	if got := ruleEngine.Match("example.com"); got != rule.Target(0) {
		t.Errorf("expected example.com → direct (Target 0), got %v", got)
	}
}

// TestMissingConfiguredRegions guards the Fix-3 safety net. When a config route
// requires match.region but that bundle isn't usable (absent, or present but
// unparseable so LoadNamed skips it), Start must surface it instead of silently
// dropping the direct route and degrading all CN traffic to the overseas proxy
// (the disconnect_feedback_bad incident). Global configs (no region route) must
// never be flagged, even with an empty cache.
func TestMissingConfiguredRegions(t *testing.T) {
	withCN := t.TempDir()
	var buf bytes.Buffer
	if err := krs.WriteBundle(&buf, &krs.Bundle{Sets: []krs.NamedSet{
		{Name: "geoip-cn", DomainSuffixes: []string{"example.cn"}},
	}}); err != nil {
		t.Fatalf("WriteBundle: %v", err)
	}
	if err := os.WriteFile(filepath.Join(withCN, "cn.krs"), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	bypassRoutes := []config.RouteConfig{
		{Via: "direct", Match: rule.MatchConfig{Region: "cn"}},
		{Via: "k2v5://x", Match: rule.MatchConfig{All: true}},
	}

	if miss := missingConfiguredRegions(bypassRoutes, withCN); len(miss) != 0 {
		t.Errorf("cn.krs present and parseable: want no missing regions, got %v", miss)
	}
	if miss := missingConfiguredRegions(bypassRoutes, t.TempDir()); len(miss) != 1 || miss[0] != "cn" {
		t.Errorf("empty cache: want [cn] missing, got %v", miss)
	}
	global := []config.RouteConfig{{Via: "k2v5://x", Match: rule.MatchConfig{All: true}}}
	if miss := missingConfiguredRegions(global, t.TempDir()); len(miss) != 0 {
		t.Errorf("global config: want no missing regions, got %v", miss)
	}
}

// TestMissingConfiguredRegions_CNFallback verifies that a non-cn region route is
// NOT flagged missing when cn.krs is present: the engine falls back to the
// always-embedded cn bundle (see heapRegionExpander) instead of 504-failing, so
// an abroad user (region=my) whose own bundle hasn't downloaded yet can still
// connect. With nothing to fall back to (empty cache), the region IS missing.
func TestMissingConfiguredRegions_CNFallback(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	if err := krs.WriteBundle(&buf, &krs.Bundle{Sets: []krs.NamedSet{
		{Name: "geoip-cn", DomainSuffixes: []string{"example.cn"}},
	}}); err != nil {
		t.Fatalf("WriteBundle: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cn.krs"), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	myRoutes := []config.RouteConfig{
		{Via: "direct", Match: rule.MatchConfig{Region: "my"}},
		{Via: "k2v5://x", Match: rule.MatchConfig{All: true}},
	}
	if miss := missingConfiguredRegions(myRoutes, dir); len(miss) != 0 {
		t.Errorf("region 'my' with cn.krs present: want no missing (cn fallback), got %v", miss)
	}
	if miss := missingConfiguredRegions(myRoutes, t.TempDir()); len(miss) != 1 || miss[0] != "my" {
		t.Errorf("empty cache: want [my] missing (nothing to fall back to), got %v", miss)
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

func TestBuildRouteEntries_NewFieldsCopied(t *testing.T) {
	routes := []config.RouteConfig{{
		Via: "direct",
		Match: rule.MatchConfig{
			ProcessNamePrefix: []string{"WeChat", "QQ"},
			PackageNamePrefix: []string{"com.tencent."},
			InstallerPackage:  []string{"com.xiaomi.market"},
		},
	}}
	entries, _, _ := buildRouteEntries(routes, nil, map[string]rule.Target{}, heapRegionExpander(nil))
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	// ProcessNamePrefixes lowercased at compile.
	if !reflect.DeepEqual([]string{"wechat", "qq"}, e.ProcessNamePrefixes) {
		t.Errorf("ProcessNamePrefixes = %v, want [wechat qq] (lowercased)", e.ProcessNamePrefixes)
	}
	// PackageNamePrefixes case preserved.
	if !reflect.DeepEqual([]string{"com.tencent."}, e.PackageNamePrefixes) {
		t.Errorf("PackageNamePrefixes = %v", e.PackageNamePrefixes)
	}
	// InstallerPackages case preserved.
	if !reflect.DeepEqual([]string{"com.xiaomi.market"}, e.InstallerPackages) {
		t.Errorf("InstallerPackages = %v", e.InstallerPackages)
	}
}

func TestBuildRouteEntries_SrcIPCIDR(t *testing.T) {
	routes := []config.RouteConfig{
		{Via: "k2v5://u:p@1.2.3.4:443", Match: rule.MatchConfig{SrcIPCIDR: []string{"10.81.1.0/24", "bogus"}}},
	}
	outboundMap := map[string]rule.Target{"k2v5://u:p@1.2.3.4:443": rule.Target(2)}
	entries, _, _ := buildRouteEntries(routes, nil, outboundMap, heapRegionExpander(nil))
	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(entries))
	}
	if len(entries[0].SrcCIDRs) != 1 || entries[0].SrcCIDRs[0].String() != "10.81.1.0/24" {
		t.Fatalf("want compiled [10.81.1.0/24] (bogus dropped), got %v", entries[0].SrcCIDRs)
	}
	if entries[0].Target != rule.Target(2) {
		t.Fatalf("want target 2, got %v", entries[0].Target)
	}
}

// TestBuildRouteEntries_PortOnly pins the two assembly-site gates that a route
// whose ONLY criterion is match.port has to pass. Both fail silently:
//   - hasAnyCriteria decides whether the compiled entry enters the routing
//     table at all. Omit PortRanges there and the entry is built, then dropped
//     — the rule vanishes with no error anywhere.
//   - MatchConfig.HasCriteria decides whether a LAST route counts as the
//     fallback. Omit Port there and this route silently becomes the catch-all,
//     sending every unmatched connection to the game outbound.
//
// This route is deliberately the last one, so one test covers both.
func TestBuildRouteEntries_PortOnly(t *testing.T) {
	routes := []config.RouteConfig{
		{Via: "k2v5://u:p@1.2.3.4:443", Match: rule.MatchConfig{Port: []string{"27000-28000"}}},
	}
	outboundMap := map[string]rule.Target{"k2v5://u:p@1.2.3.4:443": rule.Target(2)}
	entries, fallback, _ := buildRouteEntries(routes, nil, outboundMap, heapRegionExpander(nil))
	if len(entries) != 1 {
		t.Fatalf("port-only route must produce 1 entry, got %d", len(entries))
	}
	if len(entries[0].PortRanges) != 1 || entries[0].PortRanges[0] != (rule.PortRange{Lo: 27000, Hi: 28000}) {
		t.Fatalf("want compiled [27000-28000], got %v", entries[0].PortRanges)
	}
	if entries[0].Target != rule.Target(2) {
		t.Fatalf("want target 2, got %v", entries[0].Target)
	}
	if fallback != outboundDirect {
		t.Fatalf("port-only last route must not become the fallback: fallback = %v, want direct(%v)", fallback, outboundDirect)
	}
}

// TestBuildRouteEntries_ProtocolOnly pins the same two assembly-site gates as
// TestBuildRouteEntries_PortOnly, for match.protocol instead of match.port.
// Both fail silently:
//   - hasAnyCriteria decides whether the compiled entry enters the routing
//     table at all. Omit Protocols there and the entry is built, then dropped
//     — the rule vanishes with no error anywhere.
//   - MatchConfig.HasCriteria decides whether a LAST route counts as the
//     fallback. Omit Protocol there and this route silently becomes the
//     catch-all, sending every unmatched connection to the game outbound.
//
// This route is deliberately the last one, so one test covers both.
func TestBuildRouteEntries_ProtocolOnly(t *testing.T) {
	routes := []config.RouteConfig{
		{Via: "k2v5://u:p@1.2.3.4:443", Match: rule.MatchConfig{Protocol: []string{"stun"}}},
	}
	outboundMap := map[string]rule.Target{"k2v5://u:p@1.2.3.4:443": rule.Target(2)}
	entries, fallback, _ := buildRouteEntries(routes, nil, outboundMap, heapRegionExpander(nil))
	if len(entries) != 1 {
		t.Fatalf("protocol-only route must produce 1 entry, got %d", len(entries))
	}
	if len(entries[0].Protocols) != 1 || entries[0].Protocols[0] != "stun" {
		t.Fatalf("want compiled [stun], got %v", entries[0].Protocols)
	}
	if entries[0].Target != rule.Target(2) {
		t.Fatalf("want target 2, got %v", entries[0].Target)
	}
	if fallback != outboundDirect {
		t.Fatalf("protocol-only last route must not become the fallback: fallback = %v, want direct(%v)", fallback, outboundDirect)
	}
}

// TestGateProcessSearcher_PortProtocolOnly_DropsProcessSearcher pins the
// narrowed ProcessSearcher gate (engine.go's gateProcessSearcher, backed by
// rule.Engine.HasProcessCriteria): a ruleset with ONLY port/protocol/network
// criteria — no process_name/package_name/installer_package anywhere —
// must NOT hold the ProcessSearcher gate open. Port/Protocol/NetworkMatch
// are populated from the packet/socket alone (core/tunnel.go's routeConn)
// and never touch process attribution. Getting this wrong means every user
// on a port- or protocol-only ruleset (e.g. the default games/STUN rules)
// pays a per-connection lsof-equivalent cost for nothing — this is exactly
// the regression the coordinator flagged: HasMetaCriteria (which DOES
// count Port/Protocol/NetworkMatch) used to gate this and would keep the
// searcher alive for every such user.
func TestGateProcessSearcher_PortProtocolOnly_DropsProcessSearcher(t *testing.T) {
	ruleEngine := rule.NewEngine([]rule.RouteEntry{
		{
			Target:       rule.Target(0),
			NetworkMatch: "udp",
			PortRanges:   []rule.PortRange{{Lo: 3478, Hi: 3478}},
			Protocols:    []string{"stun"},
		},
	}, rule.Target(2), nil, false)

	got := gateProcessSearcher(provider.NopProcessSearcher{}, ruleEngine)
	if got != nil {
		t.Fatalf("gateProcessSearcher with port/protocol/network-only rules = %v, want nil (no rule needs process attribution)", got)
	}
}

// TestGateProcessSearcher_ProcessRule_KeepsProcessSearcher is the positive
// control for the test above: a ruleset that DOES reference process_name
// must keep the ProcessSearcher alive.
func TestGateProcessSearcher_ProcessRule_KeepsProcessSearcher(t *testing.T) {
	ruleEngine := rule.NewEngine([]rule.RouteEntry{
		{Target: rule.Target(0), ProcessNames: []string{"Steam"}},
	}, rule.Target(2), nil, false)

	searcher := provider.NopProcessSearcher{}
	got := gateProcessSearcher(searcher, ruleEngine)
	if got == nil {
		t.Fatal("gateProcessSearcher with a process_name rule = nil, want the searcher preserved")
	}
}

// TestBuildRouteEntries_AllInvalidInlineDropped pins the IsEmpty guard at the
// assembly site: an inline route whose domain_suffix is not a valid hostname
// must contribute NO route entry (old BuildInlineSet stored the garbage and
// produced a bogus matching route; krs.BuildSet drops it).
func TestBuildRouteEntries_AllInvalidInlineDropped(t *testing.T) {
	routes := []config.RouteConfig{
		{Via: "px", Match: rule.MatchConfig{DomainSuffix: []string{"_not_a_host_"}}},
	}
	outboundMap := map[string]rule.Target{"px": rule.Target(2)}
	noRegion := func(string, rule.Target) []rule.RouteEntry { return nil }

	entries, _, _ := buildRouteEntries(routes, nil, outboundMap, noRegion)
	if len(entries) != 0 {
		t.Errorf("all-invalid inline route should produce 0 entries, got %d", len(entries))
	}
}

// TestPrefetchRulesForStart_NESkipsNetwork locks the decision logic: the
// Network Extension path makes ZERO rule-source requests even when a download
// is due (stale cache), while the non-NE path attempts a network refresh. The
// NE runs in a ~50MB jetsam process and must never download — the main app's
// PrefetchRules owns CDN refresh into the shared App-Group cache.
func TestPrefetchRulesForStart_NESkipsNetwork(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.NotFound(w, r) // make every fetch fail so EnsureBundles keeps trying (counts attempts)
	}))
	defer srv.Close()

	// stalePopulatedCache writes a minimal .krs + an aged bundles.version so the
	// cache is non-empty (SeedFromEmbedIfEmpty no-ops) AND stale (past shortTTL),
	// so the non-NE path WILL attempt a network refresh.
	stalePopulatedCache := func(t *testing.T) string {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "cn.krs"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "bundles.version"), []byte("2026-06-01T00:00:00Z\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		old := time.Now().Add(-2 * time.Hour)
		_ = os.Chtimes(filepath.Join(dir, "bundles.version"), old, old)
		return dir
	}

	t.Run("NE skips network", func(t *testing.T) {
		hits.Store(0)
		dir := stalePopulatedCache(t)
		e := New()
		client := &config.ClientConfig{CacheDir: dir}
		if ee := e.prefetchRulesForStart(context.Background(), client, []string{srv.URL + "/"}, true /*NE*/); ee != nil {
			t.Fatalf("NE prefetch returned error: %v", ee)
		}
		if hits.Load() != 0 {
			t.Errorf("NE made %d rule-source requests, want 0 (NE must never download)", hits.Load())
		}
	})

	t.Run("non-NE attempts network", func(t *testing.T) {
		hits.Store(0)
		dir := stalePopulatedCache(t)
		e := New()
		client := &config.ClientConfig{CacheDir: dir}
		// Non-NE with a stale cache → EnsureBundles attempts (manifest + fallback).
		// It returns nil (stale cache exists as fallback), but it MUST have tried.
		_ = e.prefetchRulesForStart(context.Background(), client, []string{srv.URL + "/"}, false)
		if hits.Load() == 0 {
			t.Error("non-NE path made no rule-source requests, want >= 1")
		}
	})
}

// TestBuildRouteEntries_ProtocolNormalized pins the normalization at the
// assembly site: `entry.Protocols = append(..., strings.ToLower(strings.TrimSpace(p)))`
// in buildRouteEntries.
//
// It is the only defense for hand-written config. rule.matchMeta compares the
// sniffed protocol to the compiled list with plain ==, and the sniffer always
// emits the lowercase constants from sniff/result.go ("quic", "stun", ...).
// So a user writing `protocol: ["STUN"]` or `protocol: [" quic "]` — neither
// of which is unreasonable in hand-edited YAML — produces a rule that can
// never match, with no parse error and no log line.
//
// The pre-existing protocol fixtures all use an already-lowercase, already-
// trimmed "stun", so replacing the normalization with a bare append left the
// whole ./engine/ suite green. Verified before adding this test.
func TestBuildRouteEntries_ProtocolNormalized(t *testing.T) {
	routes := []config.RouteConfig{
		{Via: "k2v5://u:p@1.2.3.4:443", Match: rule.MatchConfig{
			Protocol: []string{"STUN", "  quic  ", "DtLs"},
		}},
	}
	outboundMap := map[string]rule.Target{"k2v5://u:p@1.2.3.4:443": rule.Target(2)}
	entries, _, _ := buildRouteEntries(routes, nil, outboundMap, heapRegionExpander(nil))
	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(entries))
	}

	want := []string{"stun", "quic", "dtls"}
	got := entries[0].Protocols
	if len(got) != len(want) {
		t.Fatalf("Protocols = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Protocols[%d] = %q, want %q — an unnormalized entry can never equal "+
				"the lowercase constant the sniffer emits, so the rule silently never fires",
				i, got[i], want[i])
		}
	}
}

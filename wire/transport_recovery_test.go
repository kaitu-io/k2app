package wire

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/apernet/quic-go"
)

// mockStatsTransport implements Dialer + StatsProvider for testing WireStats fallback.
type mockStatsTransport struct {
	mockTransport
	transport string
}

func (m *mockStatsTransport) WireStats() WireStats {
	return WireStats{Transport: m.transport}
}

var _ StatsProvider = (*mockStatsTransport)(nil)

// TestTransportManager_FallbackTracking verifies that when SetFallbackActive(true)
// is called, WireStats reflects TCP-WS with Fallback flag, and DialTCP skips QUIC.
func TestTransportManager_FallbackTracking(t *testing.T) {
	var quicCalled bool
	quic := &mockStatsTransport{
		mockTransport: mockTransport{
			available: true,
			dialTCPFn: func(ctx context.Context, addr string, earlyData []byte) (net.Conn, error) {
				quicCalled = true
				return nil, errors.New("QUIC broken")
			},
		},
		transport: "quic",
	}
	tcpws := &mockStatsTransport{
		mockTransport: mockTransport{
			available: true,
			dialTCPFn: func(ctx context.Context, addr string, earlyData []byte) (net.Conn, error) {
				c, _ := net.Pipe()
				return c, nil
			},
		},
		transport: "tcpws",
	}

	tm := NewTransportManager()
	tm.SetPrimary(quic, "quic-hop")
	tm.SetTCPWS(tcpws)

	// Before fallback: QUIC stats
	if tm.FallbackActive() {
		t.Fatal("should not be in fallback initially")
	}
	ws := tm.WireStats()
	if ws.Transport != "quic" {
		t.Fatalf("WireStats.Transport = %q, want quic", ws.Transport)
	}

	// Activate fallback via SetFallbackActive
	tm.SetFallbackActive(true)

	ws = tm.WireStats()
	if ws.Transport != "tcpws" {
		t.Fatalf("WireStats.Transport = %q, want tcpws in fallback", ws.Transport)
	}
	if !ws.WireFallback {
		t.Fatal("WireStats.WireFallback should be true")
	}

	// DialTCP should skip QUIC
	conn, err := tm.DialTCP(context.Background(), "8.8.8.8:53", nil)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	if quicCalled {
		t.Fatal("QUIC should be skipped when fallback is active")
	}
}

// TestTransportManager_QuicRecovery verifies that a bare QUIC handshake
// success (recordQuicSuccess) does NOT clear fallback on its own — recovery
// requires echo verification via evaluateSelector (root cause A fix; see
// TestProbe_RollbackRequiresEchoNotJustHandshake for the full mutation
// test). recordQuicSuccess only resets the handshake failure streak.
func TestTransportManager_QuicRecovery(t *testing.T) {
	qc := NewQUICClient(Config{}, nil)
	qc.handshakeFailStreak.Store(2)

	tcpws := &mockTransport{
		available: true,
		dialTCPFn: func(ctx context.Context, addr string, earlyData []byte) (net.Conn, error) {
			c, _ := net.Pipe()
			return c, nil
		},
	}

	tm := NewTransportManager()
	tm.SetPrimary(qc, "quic-hop")
	tm.SetTCPWS(tcpws)

	// Set fallback, then a bare handshake success must NOT clear it.
	tm.SetFallbackActive(true)
	if !tm.FallbackActive() {
		t.Fatal("fallback should be active")
	}

	tm.recordQuicSuccess()
	if !tm.FallbackActive() {
		t.Fatal("fallback must stay active after a bare handshake success (echo not verified)")
	}
	if got := qc.ConsecConnFails(); got != 0 {
		t.Fatalf("handshakeFailStreak = %d after recordQuicSuccess, want 0", got)
	}

	// Explicit clear (compat shim, used by engine's race-lost install path
	// via SetFallbackActiveWithReason) still works.
	tm.SetFallbackActive(false)
	if tm.FallbackActive() {
		t.Fatal("fallback should be cleared after SetFallbackActive(false)")
	}
}

// startHalfDeadQUICServer boots a real QUIC listener that completes the k2
// metadata handshake (so QUICClient.connect() succeeds exactly like a real
// server) but never responds to any stream opened afterward. An EchoProbe
// against it blocks until its own read deadline — simulating a black-holed
// data path where the handshake succeeds but Short Header packets are
// silently dropped (root cause A's failure mode).
func startHalfDeadQUICServer(t *testing.T) (*net.UDPAddr, func()) {
	t.Helper()
	cert := generateTestCert(t)

	serverUDP, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	serverTransport := &quic.Transport{Conn: serverUDP}
	ln, err := serverTransport.Listen(&tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"h3"},
	}, DefaultQUICConfig())
	if err != nil {
		t.Fatal(err)
	}

	go func() {
		for {
			conn, err := ln.Accept(context.Background())
			if err != nil {
				return
			}
			go serveHalfDeadConn(conn)
		}
	}()

	addr := serverUDP.LocalAddr().(*net.UDPAddr)
	cleanup := func() {
		ln.Close()
		serverTransport.Close()
		serverUDP.Close()
	}
	return addr, cleanup
}

// serveHalfDeadConn answers exactly one stream — the metadata handshake —
// then goes silent. A further stream (e.g. an echo probe) is accepted by
// quic-go locally on the client side regardless of server action, but the
// server here never reads or writes it, so the client blocks on its own
// read deadline.
func serveHalfDeadConn(conn *quic.Conn) {
	stream, err := conn.AcceptStream(context.Background())
	if err != nil {
		return
	}
	br := bufio.NewReader(stream)
	frameType, _, err := ReadVarint(br)
	if err != nil || frameType != H3FrameTypeK2Proxy {
		return
	}
	streamType, _, err := ReadStreamHeader(br)
	if err != nil || streamType != StreamTypeMeta {
		return
	}
	if _, err := ReadMetadataFrame(br); err != nil {
		return
	}
	_ = WriteStreamResponse(stream, nil)
	_ = WriteMetadataResponse(stream, "")
	stream.Close()
	// Deliberately do not accept any further streams — echo probes black-hole.
}

// TestProbe_RollbackRequiresEchoNotJustHandshake pins root cause A: a probe
// that only completes the QUIC handshake must NOT clear fallback / switch
// back to QUIC. It drives a REAL *QUICClient (the same concrete type
// production uses, so the mutation genuinely exercises quicProbeLoop's
// production code path — not a parallel mock implementation) against a
// server whose handshake succeeds but whose data path is black-holed.
//
// This test FAILS against the pre-Task-3 quicProbeLoop (which called
// recordQuicSuccess()/cleared fallback right after connect() succeeded,
// with no data-path check) and PASSES after the echo-verified rewrite
// (runProbeOnce, which requires 3 passing EchoProbe rounds before it will
// ever set quicEchoRecovered).
func TestProbe_RollbackRequiresEchoNotJustHandshake(t *testing.T) {
	addr, cleanup := startHalfDeadQUICServer(t)
	defer cleanup()

	qc := NewQUICClient(Config{Host: "127.0.0.1", Port: addr.Port}, NewCookieJar())
	qc.SetTLSConfig(&tls.Config{InsecureSkipVerify: true, NextProtos: []string{"h3"}})
	defer qc.Close()

	tm := NewTransportManager()
	tm.SetPrimary(qc, "quic-443")
	tm.SetTCPWS(&mockTransport{available: true})
	tm.forceActiveForTest("tcpws")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tm.runProbeOnceForTest(ctx)

	if tm.ActiveChannel() == "quic" {
		t.Fatal("half-dead QUIC (handshake ok, echo dead) must NOT trigger rollback to QUIC")
	}
	if tm.quicEchoRecovered.Load() {
		t.Fatal("quicEchoRecovered must stay false when echo probes fail")
	}
}

// TestTransportManager_ResetClearsFallback verifies that Reset()
// clears fallback state.
func TestTransportManager_ResetClearsFallback(t *testing.T) {
	tm := NewTransportManager()
	tm.SetPrimary(&mockTransport{available: true}, "quic-hop")
	tm.SetTCPWS(&mockTransport{available: true})

	tm.SetFallbackActive(true)
	if !tm.FallbackActive() {
		t.Fatal("fallback should be active")
	}

	tm.Reset()

	if tm.FallbackActive() {
		t.Fatal("fallback should be cleared after reset")
	}
}

// TestReset_ClearsConsecConnFails verifies that Reset()
// clears the consecutive connection failure counter.
func TestReset_ClearsConsecConnFails(t *testing.T) {
	qc := NewQUICClient(Config{}, nil)
	qc.handshakeFailStreak.Store(2)

	tm := NewTransportManager()
	tm.SetPrimary(qc, "quic-hop")

	tm.Reset()

	if got := qc.ConsecConnFails(); got != 0 {
		t.Fatalf("handshakeFailStreak = %d after Reset, want 0", got)
	}
}

// TestTransportManager_RelayUDPUnhealthyNotFallback verifies that
// ErrDatagramRelayUnhealthy does not trigger transport-level fallback.
func TestTransportManager_RelayUDPUnhealthyNotFallback(t *testing.T) {
	quic := &mockTransport{
		available: true,
		dialUDPFn: func(ctx context.Context, addr string) (net.PacketConn, error) {
			return nil, ErrDatagramRelayUnhealthy
		},
	}
	tcpws := &mockTransport{
		available: true,
		dialUDPFn: func(ctx context.Context, addr string) (net.PacketConn, error) {
			return newWorkingPacketConn(), nil
		},
	}

	tm := NewTransportManager()
	tm.SetPrimary(quic, "quic-hop")
	tm.SetTCPWS(tcpws)

	conn, err := tm.DialUDP(context.Background(), "8.8.8.8:53")
	if err != nil {
		t.Fatalf("DialUDP: %v", err)
	}
	conn.Close()

	if tm.FallbackActive() {
		t.Fatal("ErrDatagramRelayUnhealthy should not set fallbackActive")
	}
}

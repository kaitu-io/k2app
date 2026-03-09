package wire

import (
	"context"
	"errors"
	"net"
	"testing"
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
			dialTCPFn: func(ctx context.Context, addr string) (net.Conn, error) {
				quicCalled = true
				return nil, errors.New("QUIC broken")
			},
		},
		transport: "quic",
	}
	tcpws := &mockStatsTransport{
		mockTransport: mockTransport{
			available: true,
			dialTCPFn: func(ctx context.Context, addr string) (net.Conn, error) {
				c, _ := net.Pipe()
				return c, nil
			},
		},
		transport: "tcpws",
	}

	tm := NewTransportManager()
	tm.SetQUIC(quic)
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
	if !ws.Fallback {
		t.Fatal("WireStats.Fallback should be true")
	}

	// DialTCP should skip QUIC
	conn, err := tm.DialTCP(context.Background(), "8.8.8.8:53")
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	if quicCalled {
		t.Fatal("QUIC should be skipped when fallback is active")
	}
}

// TestTransportManager_QuicRecovery verifies that QUIC success clears fallback.
func TestTransportManager_QuicRecovery(t *testing.T) {
	quic := &mockTransport{
		available: true,
		dialTCPFn: func(ctx context.Context, addr string) (net.Conn, error) {
			c, _ := net.Pipe()
			return c, nil
		},
	}
	tcpws := &mockTransport{
		available: true,
		dialTCPFn: func(ctx context.Context, addr string) (net.Conn, error) {
			c, _ := net.Pipe()
			return c, nil
		},
	}

	tm := NewTransportManager()
	tm.SetQUIC(quic)
	tm.SetTCPWS(tcpws)

	// Set fallback, then QUIC succeeds → should clear fallback
	tm.SetFallbackActive(true)
	if !tm.FallbackActive() {
		t.Fatal("fallback should be active")
	}

	// recordQuicSuccess clears fallback
	tm.recordQuicSuccess()

	if tm.FallbackActive() {
		t.Fatal("fallback should be cleared after QUIC recovery")
	}
}

// TestTransportManager_ResetClearsFallback verifies that ResetConnections()
// clears fallback state.
func TestTransportManager_ResetClearsFallback(t *testing.T) {
	tm := NewTransportManager()
	tm.SetQUIC(&mockTransport{available: true})
	tm.SetTCPWS(&mockTransport{available: true})

	tm.SetFallbackActive(true)
	if !tm.FallbackActive() {
		t.Fatal("fallback should be active")
	}

	tm.ResetConnections()

	if tm.FallbackActive() {
		t.Fatal("fallback should be cleared after reset")
	}
}

// TestResetConnections_ClearsConsecConnFails verifies that ResetConnections()
// clears the consecutive connection failure counter.
func TestResetConnections_ClearsConsecConnFails(t *testing.T) {
	qc := NewQUICClient(Config{}, nil)
	qc.consecConnFails.Store(2)

	tm := NewTransportManager()
	tm.SetQUIC(qc)

	tm.ResetConnections()

	if got := qc.ConsecConnFails(); got != 0 {
		t.Fatalf("consecConnFails = %d after ResetConnections, want 0", got)
	}
}

// TestTransportManager_DatagramUnhealthyNotFallback verifies that
// ErrDatagramUnhealthy does not trigger transport-level fallback.
func TestTransportManager_DatagramUnhealthyNotFallback(t *testing.T) {
	quic := &mockTransport{
		available: true,
		dialUDPFn: func(ctx context.Context, addr string) (net.PacketConn, error) {
			return nil, ErrDatagramUnhealthy
		},
	}
	tcpws := &mockTransport{
		available: true,
		dialUDPFn: func(ctx context.Context, addr string) (net.PacketConn, error) {
			return newWorkingPacketConn(), nil
		},
	}

	tm := NewTransportManager()
	tm.SetQUIC(quic)
	tm.SetTCPWS(tcpws)

	conn, err := tm.DialUDP(context.Background(), "8.8.8.8:53")
	if err != nil {
		t.Fatalf("DialUDP: %v", err)
	}
	conn.Close()

	if tm.FallbackActive() {
		t.Fatal("ErrDatagramUnhealthy should not set fallbackActive")
	}
}

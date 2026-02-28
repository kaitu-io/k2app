package wire

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
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

// TestTransportManager_FallbackTracking verifies that when QUIC fails and TCP-WS
// succeeds, fallbackActive is set and WireStats reflects TCP-WS with Fallback flag.
func TestTransportManager_FallbackTracking(t *testing.T) {
	quic := &mockStatsTransport{
		mockTransport: mockTransport{
			available: true,
			dialTCPFn: func(ctx context.Context, addr string) (net.Conn, error) {
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

	// Before any dial: no fallback
	if tm.FallbackActive() {
		t.Fatal("should not be in fallback initially")
	}
	ws := tm.WireStats()
	if ws.Transport != "quic" {
		t.Fatalf("WireStats.Transport = %q, want quic", ws.Transport)
	}

	// QUIC fails → TCP-WS succeeds → fallback
	conn, err := tm.DialTCP(context.Background(), "8.8.8.8:53")
	if err != nil {
		t.Fatalf("DialTCP: %v", err)
	}
	conn.Close()

	if !tm.FallbackActive() {
		t.Fatal("should be in fallback after QUIC failure")
	}

	ws = tm.WireStats()
	if ws.Transport != "tcpws" {
		t.Fatalf("WireStats.Transport = %q, want tcpws in fallback", ws.Transport)
	}
	if !ws.Fallback {
		t.Fatal("WireStats.Fallback should be true")
	}
}

// TestTransportManager_QuicBackoff verifies that after QUIC failure, subsequent
// dials skip QUIC (no 3s budget waste) during the backoff period.
func TestTransportManager_QuicBackoff(t *testing.T) {
	var quicAttempts int
	quic := &mockTransport{
		available: true,
		dialTCPFn: func(ctx context.Context, addr string) (net.Conn, error) {
			quicAttempts++
			return nil, errors.New("QUIC broken")
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

	// First dial: QUIC tried and fails
	conn, _ := tm.DialTCP(context.Background(), "8.8.8.8:53")
	conn.Close()
	if quicAttempts != 1 {
		t.Fatalf("quicAttempts = %d, want 1", quicAttempts)
	}

	// Second dial: should skip QUIC (in backoff)
	conn, _ = tm.DialTCP(context.Background(), "8.8.8.8:53")
	conn.Close()
	if quicAttempts != 1 {
		t.Fatalf("quicAttempts = %d, want 1 (should skip during backoff)", quicAttempts)
	}

	if !tm.inQuicBackoff() {
		t.Fatal("should be in QUIC backoff")
	}
}

// TestTransportManager_QuicRecovery verifies that when QUIC recovers after
// backoff expires, fallback state is cleared.
func TestTransportManager_QuicRecovery(t *testing.T) {
	quicWorks := false
	quic := &mockTransport{
		available: true,
		dialTCPFn: func(ctx context.Context, addr string) (net.Conn, error) {
			if !quicWorks {
				return nil, errors.New("QUIC broken")
			}
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

	// QUIC fails → fallback
	conn, _ := tm.DialTCP(context.Background(), "8.8.8.8:53")
	conn.Close()
	if !tm.FallbackActive() {
		t.Fatal("should be in fallback")
	}

	// Simulate backoff expiry
	tm.quicBackoffUntil.Store(time.Now().Add(-time.Second).UnixNano())

	// QUIC recovers
	quicWorks = true
	conn, err := tm.DialTCP(context.Background(), "8.8.8.8:53")
	if err != nil {
		t.Fatalf("DialTCP: %v", err)
	}
	conn.Close()

	if tm.FallbackActive() {
		t.Fatal("fallback should be cleared after QUIC recovery")
	}
	if tm.quicFailCount.Load() != 0 {
		t.Fatalf("quicFailCount = %d, want 0", tm.quicFailCount.Load())
	}
}

// TestTransportManager_ResetClearsFallback verifies that ResetConnections()
// clears all fallback and backoff state.
func TestTransportManager_ResetClearsFallback(t *testing.T) {
	quic := &resettableMockTransport{
		mockTransport: mockTransport{
			available: true,
			dialTCPFn: func(ctx context.Context, addr string) (net.Conn, error) {
				return nil, errors.New("QUIC broken")
			},
		},
	}
	tcpws := &resettableMockTransport{
		mockTransport: mockTransport{
			available: true,
			dialTCPFn: func(ctx context.Context, addr string) (net.Conn, error) {
				c, _ := net.Pipe()
				return c, nil
			},
		},
	}

	tm := NewTransportManager()
	tm.SetQUIC(quic)
	tm.SetTCPWS(tcpws)

	// Trigger fallback
	conn, _ := tm.DialTCP(context.Background(), "8.8.8.8:53")
	conn.Close()
	if !tm.FallbackActive() {
		t.Fatal("should be in fallback")
	}

	// Reset clears everything
	tm.ResetConnections()

	if tm.FallbackActive() {
		t.Fatal("fallback should be cleared after reset")
	}
	if tm.inQuicBackoff() {
		t.Fatal("backoff should be cleared after reset")
	}
	if tm.quicFailCount.Load() != 0 {
		t.Fatalf("quicFailCount = %d, want 0 after reset", tm.quicFailCount.Load())
	}
}

// TestTransportManager_DatagramUnhealthyNotFallback verifies that
// ErrDatagramUnhealthy does not trigger transport-level fallback.
// F1 hop logic handles datagram issues — QUIC TCP path may still work.
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

	// ErrDatagramUnhealthy should NOT trigger transport-level fallback
	if tm.FallbackActive() {
		t.Fatal("ErrDatagramUnhealthy should not set fallbackActive")
	}
	if tm.quicFailCount.Load() != 0 {
		t.Fatalf("quicFailCount = %d, want 0", tm.quicFailCount.Load())
	}
}

// TestTransportManager_ExponentialBackoff verifies the backoff schedule: 30s → 60s → 120s.
// Each call simulates a separate failure event by clearing backoff first (as would
// happen in production when the backoff period expires naturally).
func TestTransportManager_ExponentialBackoff(t *testing.T) {
	tm := NewTransportManager()

	// 1st failure: 30s backoff
	tm.recordQuicFailure()
	b1 := time.Duration(tm.quicBackoffUntil.Load()-time.Now().UnixNano()) * time.Nanosecond
	if b1 < 29*time.Second || b1 > 31*time.Second {
		t.Fatalf("1st backoff = %v, want ~30s", b1)
	}

	// 2nd failure: simulate backoff expiry, then new failure event → 60s backoff
	tm.quicBackoffUntil.Store(0)
	tm.recordQuicFailure()
	b2 := time.Duration(tm.quicBackoffUntil.Load()-time.Now().UnixNano()) * time.Nanosecond
	if b2 < 59*time.Second || b2 > 61*time.Second {
		t.Fatalf("2nd backoff = %v, want ~60s", b2)
	}

	// 3rd failure: 120s backoff (cap)
	tm.quicBackoffUntil.Store(0)
	tm.recordQuicFailure()
	b3 := time.Duration(tm.quicBackoffUntil.Load()-time.Now().UnixNano()) * time.Nanosecond
	if b3 < 119*time.Second || b3 > 121*time.Second {
		t.Fatalf("3rd backoff = %v, want ~120s", b3)
	}

	// 4th failure: still 120s (cap)
	tm.quicBackoffUntil.Store(0)
	tm.recordQuicFailure()
	b4 := time.Duration(tm.quicBackoffUntil.Load()-time.Now().UnixNano()) * time.Nanosecond
	if b4 < 119*time.Second || b4 > 121*time.Second {
		t.Fatalf("4th backoff = %v, want ~120s (cap)", b4)
	}
}

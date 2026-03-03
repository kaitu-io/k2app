package wire

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// mockTransport implements a minimal transport for testing TransportManager.
type mockTransport struct {
	available bool
	dialTCPFn func(ctx context.Context, addr string) (net.Conn, error)
	dialUDPFn func(ctx context.Context, addr string) (net.PacketConn, error)
	closed    bool
}

func (m *mockTransport) DialTCP(ctx context.Context, addr string) (net.Conn, error) {
	if !m.available {
		return nil, ErrNotConnected
	}
	if m.dialTCPFn != nil {
		return m.dialTCPFn(ctx, addr)
	}
	c, _ := net.Pipe()
	return c, nil
}

func (m *mockTransport) DialUDP(ctx context.Context, addr string) (net.PacketConn, error) {
	if !m.available {
		return nil, ErrNotConnected
	}
	if m.dialUDPFn != nil {
		return m.dialUDPFn(ctx, addr)
	}
	return nil, ErrUDPNotSupported
}

func (m *mockTransport) Close() error {
	m.closed = true
	return nil
}

func TestTransportManager_Create(t *testing.T) {
	tm := NewTransportManager()
	if tm == nil {
		t.Fatal("NewTransportManager returned nil")
	}
}

func TestTransportManager_DialTCP_QUICPreferred(t *testing.T) {
	quicCalled := false
	tcpwsCalled := false

	quic := &mockTransport{
		available: true,
		dialTCPFn: func(ctx context.Context, addr string) (net.Conn, error) {
			quicCalled = true
			c, _ := net.Pipe()
			return c, nil
		},
	}
	tcpws := &mockTransport{
		available: true,
		dialTCPFn: func(ctx context.Context, addr string) (net.Conn, error) {
			tcpwsCalled = true
			c, _ := net.Pipe()
			return c, nil
		},
	}

	tm := NewTransportManager()
	tm.SetQUIC(quic)
	tm.SetTCPWS(tcpws)

	conn, err := tm.DialTCP(context.Background(), "8.8.8.8:53")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	if !quicCalled {
		t.Error("QUIC should be preferred")
	}
	if tcpwsCalled {
		t.Error("TCP-WS should not be called when QUIC is available")
	}
}

func TestTransportManager_DialTCP_FallbackToTCPWS(t *testing.T) {
	tcpwsCalled := false

	quic := &mockTransport{available: false}
	tcpws := &mockTransport{
		available: true,
		dialTCPFn: func(ctx context.Context, addr string) (net.Conn, error) {
			tcpwsCalled = true
			c, _ := net.Pipe()
			return c, nil
		},
	}

	tm := NewTransportManager()
	tm.SetQUIC(quic)
	tm.SetTCPWS(tcpws)

	conn, err := tm.DialTCP(context.Background(), "8.8.8.8:53")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	if !tcpwsCalled {
		t.Error("TCP-WS should be used as fallback")
	}
}

func TestTransportManager_DialTCP_BothDown(t *testing.T) {
	tm := NewTransportManager()
	tm.SetQUIC(&mockTransport{available: false})
	tm.SetTCPWS(&mockTransport{available: false})

	_, err := tm.DialTCP(context.Background(), "8.8.8.8:53")
	if !errors.Is(err, ErrNotConnected) {
		t.Errorf("err = %v, want ErrNotConnected", err)
	}
}

func TestTransportManager_DialTCP_NoTransports(t *testing.T) {
	tm := NewTransportManager()

	_, err := tm.DialTCP(context.Background(), "8.8.8.8:53")
	if !errors.Is(err, ErrNotConnected) {
		t.Errorf("err = %v, want ErrNotConnected", err)
	}
}

func TestTransportManager_DialTCP_QUICFailsFallback(t *testing.T) {
	tcpwsCalled := false

	quic := &mockTransport{
		available: true,
		dialTCPFn: func(ctx context.Context, addr string) (net.Conn, error) {
			return nil, errors.New("quic stream error")
		},
	}
	tcpws := &mockTransport{
		available: true,
		dialTCPFn: func(ctx context.Context, addr string) (net.Conn, error) {
			tcpwsCalled = true
			c, _ := net.Pipe()
			return c, nil
		},
	}

	tm := NewTransportManager()
	tm.SetQUIC(quic)
	tm.SetTCPWS(tcpws)

	conn, err := tm.DialTCP(context.Background(), "8.8.8.8:53")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	if !tcpwsCalled {
		t.Error("TCP-WS should be used when QUIC fails")
	}
}

func TestTransportManager_Close(t *testing.T) {
	quic := &mockTransport{available: true}
	tcpws := &mockTransport{available: true}

	tm := NewTransportManager()
	tm.SetQUIC(quic)
	tm.SetTCPWS(tcpws)

	err := tm.Close()
	if err != nil {
		t.Fatal(err)
	}

	if !quic.closed {
		t.Error("QUIC should be closed")
	}
	if !tcpws.closed {
		t.Error("TCP-WS should be closed")
	}
}

func TestTransportManager_ImplementsDialer(t *testing.T) {
	// TransportManager should implement wire.Dialer at compile time
	var _ Dialer = (*TransportManager)(nil)
}

func TestMultiTransportError_Unwrap(t *testing.T) {
	inner := errors.New("connection refused")
	mte := &MultiTransportError{
		QUICErr:  errors.New("handshake failed"),
		TCPWSErr: inner,
	}
	if !errors.Is(mte, inner) {
		t.Fatal("Unwrap should return TCPWSErr")
	}
}

func TestMultiTransportError_Format(t *testing.T) {
	mte := &MultiTransportError{
		QUICErr:  errors.New("QUIC fail"),
		TCPWSErr: errors.New("TCP fail"),
	}
	got := mte.Error()
	want := "QUIC: QUIC fail; TCP-WS: TCP fail"
	if got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func TestTransportManager_DialUDP_BothDown(t *testing.T) {
	quic := &mockTransport{
		available: true,
		dialUDPFn: func(ctx context.Context, addr string) (net.PacketConn, error) {
			return nil, errors.New("QUIC fail")
		},
	}
	tcpws := &mockTransport{
		available: true,
		dialUDPFn: func(ctx context.Context, addr string) (net.PacketConn, error) {
			return nil, errors.New("TCP fail")
		},
	}

	tm := NewTransportManager()
	tm.SetQUIC(quic)
	tm.SetTCPWS(tcpws)

	_, err := tm.DialUDP(context.Background(), "8.8.8.8:53")
	var mte *MultiTransportError
	if !errors.As(err, &mte) {
		t.Fatalf("expected MultiTransportError, got %T", err)
	}
}

// ==========================================================================
// ResetConnections tests
// ==========================================================================

func TestTransportManager_ResetConnections(t *testing.T) {
	tests := []struct {
		name  string
		quic  Dialer
		tcpws Dialer
	}{
		{
			name:  "both transports",
			quic:  NewQUICClient(Config{}, nil),
			tcpws: NewTCPWSClient(Config{}, nil),
		},
		{
			name: "only quic",
			quic: NewQUICClient(Config{}, nil),
		},
		{
			name:  "only tcpws",
			tcpws: NewTCPWSClient(Config{}, nil),
		},
		{
			name: "no transports",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tm := NewTransportManager()
			if tt.quic != nil {
				tm.SetQUIC(tt.quic)
			}
			if tt.tcpws != nil {
				tm.SetTCPWS(tt.tcpws)
			}

			// Should not panic with any combination.
			tm.ResetConnections()

			// Verify fallback state cleared.
			if tm.fallbackActive.Load() {
				t.Error("fallbackActive should be false after reset")
			}
			if tm.quicBackoffUntil.Load() != 0 {
				t.Error("quicBackoffUntil should be 0 after reset")
			}
		})
	}
}

// ==========================================================================
// Network change recovery tests
// ==========================================================================

func TestResetConnections_ClearsDatagramHopAttempts(t *testing.T) {
	client := NewQUICClient(Config{HopStart: 1000, HopEnd: 2000}, nil)
	client.datagramHopAttempts.Store(3) // exhausted

	tm := NewTransportManager()
	tm.SetQUIC(client)

	tm.ResetConnections()

	if got := client.datagramHopAttempts.Load(); got != 0 {
		t.Fatalf("datagramHopAttempts = %d after ResetConnections, want 0", got)
	}
}

func TestRecordQuicFailure_ConcurrentDedup(t *testing.T) {
	tm := NewTransportManager()

	// Simulate 50 concurrent goroutines all calling recordQuicFailure.
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tm.recordQuicFailure()
		}()
	}
	wg.Wait()

	// With dedup, count should be very small (1-2, due to nanosecond race window).
	count := tm.quicFailCount.Load()
	if count > 3 {
		t.Fatalf("quicFailCount = %d after 50 concurrent failures, want ≤ 3 (dedup)", count)
	}
	if !tm.fallbackActive.Load() {
		t.Fatal("fallbackActive should be true")
	}
}

func TestRecordQuicFailure_EscalatesAcrossEvents(t *testing.T) {
	tm := NewTransportManager()

	// First failure event: count=1, backoff=30s.
	tm.recordQuicFailure()
	if tm.quicFailCount.Load() != 1 {
		t.Fatalf("count = %d, want 1", tm.quicFailCount.Load())
	}

	// Simulate backoff expiry.
	tm.quicBackoffUntil.Store(0)

	// Second failure event: count=2, backoff=60s.
	tm.recordQuicFailure()
	if tm.quicFailCount.Load() != 2 {
		t.Fatalf("count = %d, want 2", tm.quicFailCount.Load())
	}
}

func TestQuicContext_ExtendedBudgetAfterReset(t *testing.T) {
	tm := NewTransportManager()
	tm.ResetConnections() // sets freshAfterReset

	// First call after reset: should get extended budget (~8s).
	ctx := context.Background()
	qCtx, cancel := tm.quicContext(ctx, true)
	defer cancel()
	deadline, ok := qCtx.Deadline()
	if !ok {
		t.Fatal("expected deadline on context")
	}
	budget := time.Until(deadline)
	if budget < 7*time.Second || budget > 9*time.Second {
		t.Fatalf("post-reset budget = %v, want ~8s", budget)
	}

	// Second call: should be back to normal (~3s).
	qCtx2, cancel2 := tm.quicContext(ctx, true)
	defer cancel2()
	deadline2, ok := qCtx2.Deadline()
	if !ok {
		t.Fatal("expected deadline on context")
	}
	budget2 := time.Until(deadline2)
	if budget2 < 2*time.Second || budget2 > 4*time.Second {
		t.Fatalf("normal budget = %v, want ~3s", budget2)
	}
}

// TestTransportManager_DialUDP_DoesNotClearTCPBackoff verifies that QUIC
// datagram success does not clear transport-level fallback state.
// Scenario: DialTCP fails → fallback+backoff set → backoff expires → next
// DialUDP goes through QUIC (datagrams work). The fallback flag must persist
// because datagram success doesn't prove the TCP stream path is fixed.
func TestTransportManager_DialUDP_DoesNotClearTCPBackoff(t *testing.T) {
	quic := &mockTransport{
		available: true,
		dialTCPFn: func(ctx context.Context, addr string) (net.Conn, error) {
			return nil, errors.New("QUIC TCP broken")
		},
		dialUDPFn: func(ctx context.Context, addr string) (net.PacketConn, error) {
			return newWorkingPacketConn(), nil
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

	// Trigger TCP fallback: QUIC TCP fails, TCP-WS succeeds → backoff set
	conn, err := tm.DialTCP(context.Background(), "1.1.1.1:443")
	if err != nil {
		t.Fatalf("DialTCP: %v", err)
	}
	conn.Close()
	if !tm.FallbackActive() {
		t.Fatal("should be in fallback after QUIC TCP failure")
	}
	if tm.quicFailCount.Load() != 1 {
		t.Fatalf("quicFailCount = %d, want 1", tm.quicFailCount.Load())
	}

	// Simulate backoff expiry so QUIC is tried again for UDP
	tm.quicBackoffUntil.Store(time.Now().Add(-time.Second).UnixNano())

	// DialUDP succeeds through QUIC datagrams
	pconn, err := tm.DialUDP(context.Background(), "8.8.8.8:53")
	if err != nil {
		t.Fatalf("DialUDP: %v", err)
	}
	pconn.Close()

	// fallbackActive and failCount must NOT be cleared
	if !tm.FallbackActive() {
		t.Fatal("DialUDP success should not clear TCP fallback state")
	}
	if tm.quicFailCount.Load() != 1 {
		t.Fatalf("quicFailCount = %d, want 1 (DialUDP should not reset)", tm.quicFailCount.Load())
	}
}

// TestTransportManager_ConcurrentSuccessFailure exercises concurrent DialTCP
// where QUIC alternates success/failure. Two goroutines may interleave
// recordQuicSuccess() and recordQuicFailure() — both use atomics without
// mutex. This verifies no panic/race and final state is valid.
func TestTransportManager_ConcurrentSuccessFailure(t *testing.T) {
	// QUIC: alternates success and failure
	var calls atomic.Int32
	quic := &mockTransport{
		available: true,
		dialTCPFn: func(ctx context.Context, addr string) (net.Conn, error) {
			if calls.Add(1)%2 == 0 {
				c, _ := net.Pipe()
				return c, nil
			}
			return nil, errors.New("QUIC flaky")
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

	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn, err := tm.DialTCP(context.Background(), "1.1.1.1:443")
			if err == nil {
				conn.Close()
			}
		}()
	}
	wg.Wait()

	// No panic, no race = pass. Final state is valid if:
	count := tm.quicFailCount.Load()
	if count < 0 {
		t.Fatalf("negative quicFailCount: %d", count)
	}
}

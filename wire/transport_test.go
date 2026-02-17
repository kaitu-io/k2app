package wire

import (
	"context"
	"errors"
	"net"
	"testing"
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

// ==========================================================================
// ResetConnections tests
// ==========================================================================

// resettableMockTransport implements Dialer and Resettable for testing.
type resettableMockTransport struct {
	mockTransport
	resetCalled bool
}

func (m *resettableMockTransport) resetConnection() {
	m.resetCalled = true
}

func TestTransportManager_ResetConnections(t *testing.T) {
	tests := []struct {
		name          string
		quic          Dialer
		tcpws         Dialer
		wantQUICReset bool
		wantTCPWSReset bool
	}{
		{
			name:          "both resettable",
			quic:          &resettableMockTransport{mockTransport: mockTransport{available: true}},
			tcpws:         &resettableMockTransport{mockTransport: mockTransport{available: true}},
			wantQUICReset: true,
			wantTCPWSReset: true,
		},
		{
			name:          "only quic set",
			quic:          &resettableMockTransport{mockTransport: mockTransport{available: true}},
			tcpws:         nil,
			wantQUICReset: true,
			wantTCPWSReset: false,
		},
		{
			name:          "only tcpws set",
			quic:          nil,
			tcpws:         &resettableMockTransport{mockTransport: mockTransport{available: true}},
			wantQUICReset: false,
			wantTCPWSReset: true,
		},
		{
			name:          "non-resettable transports",
			quic:          &mockTransport{available: true},
			tcpws:         &mockTransport{available: true},
			wantQUICReset: false,
			wantTCPWSReset: false,
		},
		{
			name:          "no transports",
			quic:          nil,
			tcpws:         nil,
			wantQUICReset: false,
			wantTCPWSReset: false,
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

			tm.ResetConnections()

			if tt.wantQUICReset {
				r := tt.quic.(*resettableMockTransport)
				if !r.resetCalled {
					t.Error("expected QUIC resetConnection to be called")
				}
			}
			if tt.wantTCPWSReset {
				r := tt.tcpws.(*resettableMockTransport)
				if !r.resetCalled {
					t.Error("expected TCP-WS resetConnection to be called")
				}
			}
		})
	}
}

package wire

import (
	"context"
	"errors"
	"net"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

// ==========================================================================
// Reproduce: DialUDP fallback gap
//
// Root cause: QUICClient.DialUDP always "succeeds" by creating a lightweight
// wrapper. TransportManager.DialUDP never triggers TCP-WS fallback because
// the QUIC path appears healthy at dial time. The actual failure surfaces
// later as a ReadFrom timeout when datagrams are silently lost (e.g. ISP QoS).
//
// Contrast with DialTCP where QUIC stream open requires a bidirectional
// handshake — failures are detected immediately, triggering TCP-WS fallback.
// ==========================================================================

// brokenPacketConn simulates a QUIC datagram PacketConn where the path is
// broken: WriteTo "succeeds" (SendDatagram accepted by QUIC) but responses
// never arrive (ISP dropped the UDP mapping). ReadFrom blocks until deadline.
type brokenPacketConn struct {
	closed chan struct{}
	rd     time.Time
}

func (c *brokenPacketConn) ReadFrom(b []byte) (int, net.Addr, error) {
	if c.rd.IsZero() {
		// No deadline: block forever (would hang test without timeout)
		<-c.closed
		return 0, nil, net.ErrClosed
	}
	wait := time.Until(c.rd)
	if wait <= 0 {
		return 0, nil, os.ErrDeadlineExceeded
	}
	select {
	case <-time.After(wait):
		return 0, nil, os.ErrDeadlineExceeded
	case <-c.closed:
		return 0, nil, net.ErrClosed
	}
}

func (c *brokenPacketConn) WriteTo(b []byte, _ net.Addr) (int, error) {
	// SendDatagram succeeds — QUIC accepted the datagram, but ISP drops it
	return len(b), nil
}

func (c *brokenPacketConn) Close() error                       { close(c.closed); return nil }
func (c *brokenPacketConn) LocalAddr() net.Addr                { return &net.UDPAddr{} }
func (c *brokenPacketConn) SetDeadline(t time.Time) error      { c.rd = t; return nil }
func (c *brokenPacketConn) SetReadDeadline(t time.Time) error  { c.rd = t; return nil }
func (c *brokenPacketConn) SetWriteDeadline(t time.Time) error { return nil }

// workingPacketConn echoes back whatever is written to it.
type workingPacketConn struct {
	ch     chan []byte
	closed chan struct{}
	rd     time.Time
}

func newWorkingPacketConn() *workingPacketConn {
	return &workingPacketConn{
		ch:     make(chan []byte, 16),
		closed: make(chan struct{}),
	}
}

func (c *workingPacketConn) ReadFrom(b []byte) (int, net.Addr, error) {
	if c.rd.IsZero() {
		select {
		case data := <-c.ch:
			n := copy(b, data)
			return n, &net.UDPAddr{IP: net.IPv4(8, 8, 8, 8), Port: 53}, nil
		case <-c.closed:
			return 0, nil, net.ErrClosed
		}
	}
	wait := time.Until(c.rd)
	if wait <= 0 {
		return 0, nil, os.ErrDeadlineExceeded
	}
	select {
	case data := <-c.ch:
		n := copy(b, data)
		return n, &net.UDPAddr{IP: net.IPv4(8, 8, 8, 8), Port: 53}, nil
	case <-time.After(wait):
		return 0, nil, os.ErrDeadlineExceeded
	case <-c.closed:
		return 0, nil, net.ErrClosed
	}
}

func (c *workingPacketConn) WriteTo(b []byte, _ net.Addr) (int, error) {
	// Echo back to simulate working DNS response
	data := make([]byte, len(b))
	copy(data, b)
	select {
	case c.ch <- data:
	case <-c.closed:
	}
	return len(b), nil
}

func (c *workingPacketConn) Close() error {
	select {
	case <-c.closed:
	default:
		close(c.closed)
	}
	return nil
}

func (c *workingPacketConn) LocalAddr() net.Addr                { return &net.UDPAddr{} }
func (c *workingPacketConn) SetDeadline(t time.Time) error      { c.rd = t; return nil }
func (c *workingPacketConn) SetReadDeadline(t time.Time) error  { c.rd = t; return nil }
func (c *workingPacketConn) SetWriteDeadline(t time.Time) error { return nil }

// TestTransportManager_DialTCP_FallbackWorks demonstrates that TCP fallback
// works correctly: QUIC DialTCP fails → TCP-WS is used.
func TestTransportManager_DialTCP_FallbackWorks(t *testing.T) {
	var tcpwsUsed atomic.Bool

	quic := &mockTransport{
		available: true,
		dialTCPFn: func(ctx context.Context, addr string, earlyData []byte) (net.Conn, error) {
			return nil, errors.New("QUIC stream broken")
		},
	}
	tcpws := &mockTransport{
		available: true,
		dialTCPFn: func(ctx context.Context, addr string, earlyData []byte) (net.Conn, error) {
			tcpwsUsed.Store(true)
			c, _ := net.Pipe()
			return c, nil
		},
	}

	tm := NewTransportManager()
	tm.SetPrimary(quic, "quic-443")
	tm.SetTCPWS(tcpws)

	conn, err := tm.DialTCP(context.Background(), "1.1.1.1:443", nil)
	if err != nil {
		t.Fatalf("DialTCP should succeed via fallback, got: %v", err)
	}
	conn.Close()

	if !tcpwsUsed.Load() {
		t.Fatal("TCP-WS should have been used as fallback when QUIC failed")
	}
}

// TestTransportManager_DialUDP_FallbackAfterHealthDegradation demonstrates the fix:
// After relayTimeoutThreshold consecutive ReadFrom timeouts, QUICClient.DialUDP
// returns ErrDatagramRelayUnhealthy, triggering TransportManager fallback to TCP-WS.
//
// Sequence:
//  1. First DialUDP → QUIC returns broken conn (silent success — first query may timeout)
//  2. ReadFrom timeouts accumulate on the mux health tracker
//  3. Subsequent DialUDP → QUIC returns ErrDatagramRelayUnhealthy → TransportManager falls back to TCP-WS
func TestTransportManager_DialUDP_FallbackAfterHealthDegradation(t *testing.T) {
	var tcpwsUsed atomic.Bool
	var dialCount atomic.Int32

	// QUIC: first calls succeed (broken conn), after threshold return health error
	quic := &mockTransport{
		available: true,
		dialUDPFn: func(ctx context.Context, addr string) (net.PacketConn, error) {
			n := dialCount.Add(1)
			if n <= relayTimeoutThreshold {
				// Silent success — broken path not detectable at dial time
				return &brokenPacketConn{closed: make(chan struct{})}, nil
			}
			// Health gate kicks in after threshold timeouts detected
			return nil, ErrDatagramRelayUnhealthy
		},
	}

	// TCP-WS: works perfectly
	tcpws := &mockTransport{
		available: true,
		dialUDPFn: func(ctx context.Context, addr string) (net.PacketConn, error) {
			tcpwsUsed.Store(true)
			return newWorkingPacketConn(), nil
		},
	}

	tm := NewTransportManager()
	tm.SetPrimary(quic, "quic-443")
	tm.SetTCPWS(tcpws)

	// Simulate threshold broken connections (these succeed but are broken)
	for i := int32(0); i < relayTimeoutThreshold; i++ {
		conn, err := tm.DialUDP(context.Background(), "8.8.8.8:53")
		if err != nil {
			t.Fatalf("early DialUDP #%d should succeed: %v", i+1, err)
		}
		conn.Close()
	}

	// Next DialUDP should trigger fallback to TCP-WS
	conn, err := tm.DialUDP(context.Background(), "8.8.8.8:53")
	if err != nil {
		t.Fatalf("DialUDP should succeed via TCP-WS fallback: %v", err)
	}
	defer conn.Close()

	if !tcpwsUsed.Load() {
		t.Fatal("TCP-WS should have been used after QUIC datagram health degradation")
	}

	// Verify the fallback connection actually works
	payload := []byte("dns query")
	if _, err := conn.WriteTo(payload, nil); err != nil {
		t.Fatalf("WriteTo failed: %v", err)
	}
	conn.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 1024)
	n, _, err := conn.ReadFrom(buf)
	if err != nil {
		t.Fatalf("ReadFrom failed: %v", err)
	}
	if string(buf[:n]) != string(payload) {
		t.Errorf("got %q, want %q", buf[:n], payload)
	}
}

// TestTransportManager_DialUDP_FallbackOnDialError shows that when QUIC DialUDP
// returns an actual error (not the silent success case), fallback works fine.
// This is the "easy" case that already works.
func TestTransportManager_DialUDP_FallbackOnDialError(t *testing.T) {
	var tcpwsUsed atomic.Bool

	quic := &mockTransport{
		available: true,
		dialUDPFn: func(ctx context.Context, addr string) (net.PacketConn, error) {
			return nil, errors.New("datagram not supported")
		},
	}
	tcpws := &mockTransport{
		available: true,
		dialUDPFn: func(ctx context.Context, addr string) (net.PacketConn, error) {
			tcpwsUsed.Store(true)
			return newWorkingPacketConn(), nil
		},
	}

	tm := NewTransportManager()
	tm.SetPrimary(quic, "quic-443")
	tm.SetTCPWS(tcpws)

	conn, err := tm.DialUDP(context.Background(), "8.8.8.8:53")
	if err != nil {
		t.Fatalf("DialUDP should succeed via fallback: %v", err)
	}
	defer conn.Close()

	if !tcpwsUsed.Load() {
		t.Fatal("TCP-WS should have been used when QUIC DialUDP returned error")
	}

	// Verify the connection actually works
	payload := []byte("dns query")
	_, err = conn.WriteTo(payload, nil)
	if err != nil {
		t.Fatalf("WriteTo failed: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 1024)
	n, _, err := conn.ReadFrom(buf)
	if err != nil {
		t.Fatalf("ReadFrom failed: %v", err)
	}
	if string(buf[:n]) != string(payload) {
		t.Errorf("got %q, want %q", buf[:n], payload)
	}
}

// TestTransportManager_DialUDP_Asymmetry documents the fundamental asymmetry
// between TCP and UDP fallback behavior. This test passes with current code,
// demonstrating the design gap.
func TestTransportManager_DialUDP_Asymmetry(t *testing.T) {
	// TCP: QUIC failure at dial time → immediate fallback
	t.Run("TCP_fallback_immediate", func(t *testing.T) {
		var fallbackUsed atomic.Bool

		tm := NewTransportManager()
		tm.SetPrimary(&mockTransport{
			available: true,
			dialTCPFn: func(ctx context.Context, addr string, earlyData []byte) (net.Conn, error) {
				return nil, errors.New("QUIC broken")
			},
		}, "quic-443")
		tm.SetTCPWS(&mockTransport{
			available: true,
			dialTCPFn: func(ctx context.Context, addr string, earlyData []byte) (net.Conn, error) {
				fallbackUsed.Store(true)
				c, _ := net.Pipe()
				return c, nil
			},
		})

		conn, err := tm.DialTCP(context.Background(), "1.1.1.1:443", nil)
		if err != nil {
			t.Fatalf("DialTCP: %v", err)
		}
		conn.Close()

		if !fallbackUsed.Load() {
			t.Error("TCP-WS fallback should fire on QUIC failure")
		}
	})

	// UDP: After health degradation, QUIC returns error → fallback fires
	t.Run("UDP_fallback_after_health_degradation", func(t *testing.T) {
		var fallbackUsed atomic.Bool
		var dialCount atomic.Int32

		tm := NewTransportManager()
		tm.SetPrimary(&mockTransport{
			available: true,
			dialUDPFn: func(ctx context.Context, addr string) (net.PacketConn, error) {
				if dialCount.Add(1) > relayTimeoutThreshold {
					return nil, ErrDatagramRelayUnhealthy
				}
				return &brokenPacketConn{closed: make(chan struct{})}, nil
			},
		}, "quic-443")
		tm.SetTCPWS(&mockTransport{
			available: true,
			dialUDPFn: func(ctx context.Context, addr string) (net.PacketConn, error) {
				fallbackUsed.Store(true)
				return newWorkingPacketConn(), nil
			},
		})

		// Exhaust QUIC health budget
		for i := int32(0); i < relayTimeoutThreshold; i++ {
			conn, err := tm.DialUDP(context.Background(), "8.8.8.8:53")
			if err != nil {
				t.Fatalf("early DialUDP: %v", err)
			}
			conn.Close()
		}

		// Next call should fallback
		conn, err := tm.DialUDP(context.Background(), "8.8.8.8:53")
		if err != nil {
			t.Fatalf("DialUDP with fallback: %v", err)
		}
		defer conn.Close()

		if !fallbackUsed.Load() {
			t.Error("TCP-WS fallback should fire after QUIC health degradation")
		}
	})
}

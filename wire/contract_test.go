package wire

// Transport Behavior Contract Tests
//
// These tests anchor the observable transport behavior that users depend on.
// Each test maps to a user scenario (e.g., "QUIC blocked at airport WiFi")
// and asserts the contract that the transport layer must uphold.
//
// Contract tests are NOT implementation tests. They don't test internal
// data structures or EWMA math. They test: given this network condition,
// what does the user experience?

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
)

// contractTimeoutError simulates a slow upstream (not a transport failure).
type contractTimeoutError struct{ msg string }

func (e *contractTimeoutError) Error() string   { return e.msg }
func (e *contractTimeoutError) Timeout() bool   { return true }
func (e *contractTimeoutError) Temporary() bool { return false }

// TestContract_StartupQUICFirst verifies that on a fresh connection with both
// transports available, traffic always goes through QUIC (preferred transport).
// User scenario: normal startup on unrestricted network.
func TestContract_StartupQUICFirst(t *testing.T) {
	quicCalled := false
	tcpwsCalled := false

	quic := &mockTransport{
		available: true,
		dialTCPFn: func(ctx context.Context, addr string, earlyData []byte) (net.Conn, error) {
			quicCalled = true
			c, _ := net.Pipe()
			return c, nil
		},
	}
	tcpws := &mockTransport{
		available: true,
		dialTCPFn: func(ctx context.Context, addr string, earlyData []byte) (net.Conn, error) {
			tcpwsCalled = true
			c, _ := net.Pipe()
			return c, nil
		},
	}

	tm := NewTransportManager()
	tm.SetPrimary(quic, "quic-hop")
	tm.SetTCPWS(tcpws)

	conn, err := tm.DialTCP(context.Background(), "1.1.1.1:443", nil)
	if err != nil {
		t.Fatalf("DialTCP: %v", err)
	}
	conn.Close()

	if !quicCalled {
		t.Fatal("contract: QUIC must be the preferred transport on startup")
	}
	if tcpwsCalled {
		t.Fatal("contract: TCP-WS must not be used when QUIC succeeds")
	}
}

// TestContract_NormalTCPviaQUIC_UDPviaDatagram verifies that both TCP and UDP
// traffic go through QUIC when it is healthy.
// User scenario: browsing + video call on a normal connection.
func TestContract_NormalTCPviaQUIC_UDPviaDatagram(t *testing.T) {
	quicTCPCalled := false
	quicUDPCalled := false
	tcpwsTCPCalled := false
	tcpwsUDPCalled := false

	quic := &mockTransport{
		available: true,
		dialTCPFn: func(ctx context.Context, addr string, earlyData []byte) (net.Conn, error) {
			quicTCPCalled = true
			c, _ := net.Pipe()
			return c, nil
		},
		dialUDPFn: func(ctx context.Context, addr string) (net.PacketConn, error) {
			quicUDPCalled = true
			return newWorkingPacketConn(), nil
		},
	}
	tcpws := &mockTransport{
		available: true,
		dialTCPFn: func(ctx context.Context, addr string, earlyData []byte) (net.Conn, error) {
			tcpwsTCPCalled = true
			c, _ := net.Pipe()
			return c, nil
		},
		dialUDPFn: func(ctx context.Context, addr string) (net.PacketConn, error) {
			tcpwsUDPCalled = true
			return newWorkingPacketConn(), nil
		},
	}

	tm := NewTransportManager()
	tm.SetPrimary(quic, "quic-hop")
	tm.SetTCPWS(tcpws)

	conn, err := tm.DialTCP(context.Background(), "1.1.1.1:443", nil)
	if err != nil {
		t.Fatalf("DialTCP: %v", err)
	}
	conn.Close()

	pconn, err := tm.DialUDP(context.Background(), "8.8.8.8:53")
	if err != nil {
		t.Fatalf("DialUDP: %v", err)
	}
	pconn.Close()

	if !quicTCPCalled {
		t.Fatal("contract: TCP must go through QUIC")
	}
	if !quicUDPCalled {
		t.Fatal("contract: UDP must go through QUIC datagrams")
	}
	if tcpwsTCPCalled {
		t.Fatal("contract: TCP-WS must not be used for TCP when QUIC is healthy")
	}
	if tcpwsUDPCalled {
		t.Fatal("contract: TCP-WS must not be used for UDP when QUIC is healthy")
	}
}

// TestContract_QUICBlocked_FallbackTCPWS verifies that when QUIC handshakes
// fail repeatedly (UDP blocked by firewall), traffic falls back to TCP-WS.
// User scenario: airport/hotel WiFi blocks UDP, user expects connectivity.
func TestContract_QUICBlocked_FallbackTCPWS(t *testing.T) {
	tcpwsCalled := false

	qc := NewQUICClient(Config{}, nil)
	qc.closed = true // force DialTCP to fail
	qc.handshakeFailStreak.Store(quicConnFailThreshold)

	tcpws := &mockTransport{
		available: true,
		dialTCPFn: func(ctx context.Context, addr string, earlyData []byte) (net.Conn, error) {
			tcpwsCalled = true
			c, _ := net.Pipe()
			return c, nil
		},
	}

	tm := NewTransportManager()
	tm.SetPrimary(qc, "quic-hop")
	tm.SetTCPWS(tcpws)
	tm.SetFallbackActive(true)

	conn, err := tm.DialTCP(context.Background(), "1.1.1.1:443", nil)
	if err != nil {
		t.Fatalf("DialTCP: %v", err)
	}
	conn.Close()

	if !tcpwsCalled {
		t.Fatal("contract: TCP-WS must serve traffic when QUIC is blocked")
	}
	if !tm.FallbackActive() {
		t.Fatal("contract: fallback flag must remain active until QUIC recovers")
	}
}

// TestContract_QUICRecover_ProbeSuccess verifies that when QUIC recovers
// (e.g., user leaves airport WiFi), traffic switches back to QUIC.
// User scenario: user returns to normal network after being on restricted one.
func TestContract_QUICRecover_ProbeSuccess(t *testing.T) {
	quicCalled := false

	quic := &mockTransport{
		available: true,
		dialTCPFn: func(ctx context.Context, addr string, earlyData []byte) (net.Conn, error) {
			quicCalled = true
			c, _ := net.Pipe()
			return c, nil
		},
	}
	tcpws := &mockTransport{
		available: true,
		dialTCPFn: func(ctx context.Context, addr string, earlyData []byte) (net.Conn, error) {
			c, _ := net.Pipe()
			return c, nil
		},
	}

	tm := NewTransportManager()
	tm.SetPrimary(quic, "quic-hop")
	tm.SetTCPWS(tcpws)

	// Start in fallback state (QUIC was blocked).
	tm.SetFallbackActive(true)

	// Simulate QUIC probe success.
	tm.recordQuicSuccess()

	if tm.FallbackActive() {
		t.Fatal("contract: fallback must be cleared after QUIC recovery")
	}

	// Next DialTCP should go through QUIC.
	conn, err := tm.DialTCP(context.Background(), "1.1.1.1:443", nil)
	if err != nil {
		t.Fatalf("DialTCP: %v", err)
	}
	conn.Close()

	if !quicCalled {
		t.Fatal("contract: QUIC must be preferred after recovery")
	}
}

// TestContract_DatagramLossHigh_DegradeToStream verifies that when QUIC datagram
// relay has high loss, UDP falls back to TCP-WS per-request WITHOUT switching
// the entire transport. TCP traffic continues through QUIC.
// User scenario: lossy mobile network drops datagrams but TCP streams are fine.
func TestContract_DatagramLossHigh_DegradeToStream(t *testing.T) {
	quicTCPCalled := false

	quic := &mockTransport{
		available: true,
		dialTCPFn: func(ctx context.Context, addr string, earlyData []byte) (net.Conn, error) {
			quicTCPCalled = true
			c, _ := net.Pipe()
			return c, nil
		},
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

	// UDP should succeed via TCP-WS fallback.
	pconn, err := tm.DialUDP(context.Background(), "8.8.8.8:53")
	if err != nil {
		t.Fatalf("DialUDP: %v", err)
	}
	pconn.Close()

	// Transport-level fallback must NOT be activated (only datagram relay is bad).
	if tm.FallbackActive() {
		t.Fatal("contract: datagram relay loss must not trigger transport-level fallback")
	}

	// TCP should still go through QUIC (transport is fine).
	conn, err := tm.DialTCP(context.Background(), "1.1.1.1:443", nil)
	if err != nil {
		t.Fatalf("DialTCP: %v", err)
	}
	conn.Close()

	if !quicTCPCalled {
		t.Fatal("contract: TCP must still use QUIC when only datagram relay is degraded")
	}
}

// TestContract_NetworkSwitch_ResetAll verifies that switching networks (e.g.,
// WiFi to cellular) resets all transport state and gives QUIC a fresh chance.
// User scenario: user walks out of office WiFi to cellular.
func TestContract_NetworkSwitch_ResetAll(t *testing.T) {
	qc := NewQUICClient(Config{}, nil)
	qc.handshakeFailStreak.Store(5)

	tm := NewTransportManager()
	tm.SetPrimary(qc, "quic-hop")
	tm.SetFallbackActive(true)

	// Simulate network change.
	tm.Reset()

	if tm.FallbackActive() {
		t.Fatal("contract: fallback must be cleared after network switch")
	}
	if qc.ConsecConnFails() != 0 {
		t.Fatalf("contract: handshake fail streak must be reset, got %d", qc.ConsecConnFails())
	}
	if !tm.freshAfterReset.Load() {
		t.Fatal("contract: freshAfterReset must be set for extended QUIC budget")
	}
}

// TestContract_SlowUpstream_NoFallback verifies that a raw timeout (not wrapped
// in ProxyLayerError) does NOT trigger persistent transport-level fallback.
// This simulates a transport-layer timeout (e.g., connect timeout) — ambiguous
// whether transport or proxy. Per-request TCP-WS fallback IS allowed (the error
// falls through). Only errors wrapped in ProxyLayerError by the Dialer skip
// fallback entirely (see TestContract_ReadResponseTimeout_NoFallback).
// User scenario: QUIC connect timeout, TCP-WS might work via different path.
func TestContract_SlowUpstream_NoFallback(t *testing.T) {
	quic := &mockTransport{
		available: true,
		dialTCPFn: func(ctx context.Context, addr string, earlyData []byte) (net.Conn, error) {
			return nil, &contractTimeoutError{msg: "dial tcp 93.184.216.34:443: i/o timeout"}
		},
	}
	tcpws := &mockTransport{
		available: true,
		dialTCPFn: func(ctx context.Context, addr string, earlyData []byte) (net.Conn, error) {
			c, _ := net.Pipe()
			return c, nil
		},
	}

	tm := NewTransportManager()
	tm.SetPrimary(quic, "quic-hop")
	tm.SetTCPWS(tcpws)

	// DialTCP may succeed via TCP-WS per-request (QUIC error falls through).
	conn, err := tm.DialTCP(context.Background(), "93.184.216.34:443", nil)
	if err != nil {
		t.Fatalf("DialTCP: %v", err)
	}
	conn.Close()

	// Transport-level fallback must NOT be activated.
	// A timeout is a destination problem, not a QUIC transport problem.
	if tm.FallbackActive() {
		t.Fatal("contract: upstream timeout must not trigger transport-level fallback")
	}
}

// TestContract_ServerProxyError_NoFallback verifies that when the server
// responds with a proxy error (e.g., 502 — target unreachable), TCP-WS
// fallback is NOT attempted. The transport worked; the problem is server-side.
// User scenario: server can't reach target.com but QUIC transport is fine.
func TestContract_ServerProxyError_NoFallback(t *testing.T) {
	tcpwsCalled := false
	quic := &mockTransport{
		available: true,
		dialTCPFn: func(ctx context.Context, addr string, earlyData []byte) (net.Conn, error) {
			return nil, &StreamError{Code: 502, Message: "dial tcp4 target.com:443: connection refused"}
		},
	}
	tcpws := &mockTransport{
		available: true,
		dialTCPFn: func(ctx context.Context, addr string, earlyData []byte) (net.Conn, error) {
			tcpwsCalled = true
			c, _ := net.Pipe()
			return c, nil
		},
	}

	tm := NewTransportManager()
	tm.SetPrimary(quic, "quic-hop")
	tm.SetTCPWS(tcpws)

	_, err := tm.DialTCP(context.Background(), "target.com:443", nil)
	if err == nil {
		t.Fatal("expected error from proxy layer")
	}

	var se *StreamError
	if !errors.As(err, &se) {
		t.Fatalf("expected StreamError, got %T: %v", err, err)
	}
	if se.Code != 502 {
		t.Fatalf("expected code 502, got %d", se.Code)
	}
	if tcpwsCalled {
		t.Fatal("contract: server proxy error must NOT trigger TCP-WS fallback")
	}
	if tm.FallbackActive() {
		t.Fatal("contract: proxy error must not activate persistent fallback")
	}
}

// TestContract_ServerRejected_NoFallback verifies that ErrStreamRejected
// (server rejects the stream without a coded error) also skips TCP-WS fallback.
// User scenario: server policy rejects the connection target.
func TestContract_ServerRejected_NoFallback(t *testing.T) {
	tcpwsCalled := false
	quic := &mockTransport{
		available: true,
		dialTCPFn: func(ctx context.Context, addr string, earlyData []byte) (net.Conn, error) {
			// Dialer marks proxy-layer errors — simulates real QUICClient/TCPWSClient behavior.
			return nil, &ProxyLayerError{Err: fmt.Errorf("%w: policy violation", ErrStreamRejected)}
		},
	}
	tcpws := &mockTransport{
		available: true,
		dialTCPFn: func(ctx context.Context, addr string, earlyData []byte) (net.Conn, error) {
			tcpwsCalled = true
			c, _ := net.Pipe()
			return c, nil
		},
	}

	tm := NewTransportManager()
	tm.SetPrimary(quic, "quic-hop")
	tm.SetTCPWS(tcpws)

	_, err := tm.DialTCP(context.Background(), "blocked.com:443", nil)
	if err == nil {
		t.Fatal("expected error from stream rejection")
	}
	if !errors.Is(err, ErrStreamRejected) {
		t.Fatalf("expected ErrStreamRejected, got: %v", err)
	}
	if tcpwsCalled {
		t.Fatal("contract: stream rejection must NOT trigger TCP-WS fallback")
	}
	if tm.FallbackActive() {
		t.Fatal("contract: stream rejection must not activate persistent fallback")
	}
}

// TestContract_ReadResponseTimeout_NoFallback verifies that when
// ReadStreamResponse times out (request sent, server processing),
// TCP-WS fallback is NOT attempted. The transport worked (stream opened,
// header written); the timeout is server-side.
// User scenario: server's upstream net.Dial to target is slow.
func TestContract_ReadResponseTimeout_NoFallback(t *testing.T) {
	tcpwsCalled := false
	quic := &mockTransport{
		available: true,
		dialTCPFn: func(ctx context.Context, addr string, earlyData []byte) (net.Conn, error) {
			// Simulate: stream opened, header written, response timed out.
			// Real QUICClient wraps this in ProxyLayerError after writes succeed.
			return nil, &ProxyLayerError{Err: fmt.Errorf("wire: read response: %w",
				&contractTimeoutError{msg: "i/o timeout"})}
		},
	}
	tcpws := &mockTransport{
		available: true,
		dialTCPFn: func(ctx context.Context, addr string, earlyData []byte) (net.Conn, error) {
			tcpwsCalled = true
			c, _ := net.Pipe()
			return c, nil
		},
	}

	tm := NewTransportManager()
	tm.SetPrimary(quic, "quic-hop")
	tm.SetTCPWS(tcpws)

	_, err := tm.DialTCP(context.Background(), "slow.example.com:443", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if tcpwsCalled {
		t.Fatal("contract: read-response timeout must NOT trigger TCP-WS fallback")
	}
	if tm.FallbackActive() {
		t.Fatal("contract: read-response timeout must not activate persistent fallback")
	}
}

// Verify contractTimeoutError satisfies net.Error for the timeout check.
var _ net.Error = (*contractTimeoutError)(nil)

func init() {
	// Compile-time assertion: contractTimeoutError must be a net.Error
	// so that TransportManager's error classification can detect it.
	var e error = &contractTimeoutError{}
	if _, ok := e.(net.Error); !ok {
		panic("contractTimeoutError must implement net.Error")
	}
}

// Verify mockTransport satisfies Dialer.
var _ Dialer = (*mockTransport)(nil)

// TestContract_SlowUpstream_ErrorClassification is a sub-test verifying
// that the timeout error is properly recognized as a timeout by Go's
// net.Error interface, confirming the test's precondition.
func TestContract_SlowUpstream_ErrorClassification(t *testing.T) {
	err := &contractTimeoutError{msg: "i/o timeout"}
	var netErr net.Error
	if !errors.As(err, &netErr) {
		t.Fatal("contractTimeoutError must satisfy net.Error")
	}
	if !netErr.Timeout() {
		t.Fatal("contractTimeoutError.Timeout() must return true")
	}
}

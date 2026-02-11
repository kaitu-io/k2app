package wire

import (
	"context"
	"net"
	"sync"
)

// TransportManager manages QUIC and TCP-WS transports.
// QUIC is preferred; TCP-WS is used as fallback.
// Implements the Dialer interface.
type TransportManager struct {
	mu    sync.RWMutex
	quic  Dialer
	tcpws Dialer
}

// NewTransportManager creates a new transport manager.
func NewTransportManager() *TransportManager {
	return &TransportManager{}
}

// SetQUIC sets the QUIC transport.
func (tm *TransportManager) SetQUIC(d Dialer) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.quic = d
}

// SetTCPWS sets the TCP-WS transport.
func (tm *TransportManager) SetTCPWS(d Dialer) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.tcpws = d
}

// DialTCP opens a TCP proxy connection.
// Prefers QUIC, falls back to TCP-WS.
func (tm *TransportManager) DialTCP(ctx context.Context, addr string) (net.Conn, error) {
	tm.mu.RLock()
	quic := tm.quic
	tcpws := tm.tcpws
	tm.mu.RUnlock()

	// Try QUIC first (preferred)
	if quic != nil {
		conn, err := quic.DialTCP(ctx, addr)
		if err == nil {
			return conn, nil
		}
	}

	// Fall back to TCP-WS
	if tcpws != nil {
		conn, err := tcpws.DialTCP(ctx, addr)
		if err == nil {
			return conn, nil
		}
	}

	return nil, ErrNotConnected
}

// DialUDP opens a UDP proxy connection.
// Prefers QUIC (datagram), falls back to TCP-WS (smux stream).
func (tm *TransportManager) DialUDP(ctx context.Context, addr string) (net.PacketConn, error) {
	tm.mu.RLock()
	quic := tm.quic
	tcpws := tm.tcpws
	tm.mu.RUnlock()

	if quic != nil {
		conn, err := quic.DialUDP(ctx, addr)
		if err == nil {
			return conn, nil
		}
	}

	if tcpws != nil {
		conn, err := tcpws.DialUDP(ctx, addr)
		if err == nil {
			return conn, nil
		}
	}

	return nil, ErrNotConnected
}

// Close closes both transports.
func (tm *TransportManager) Close() error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	var firstErr error
	if tm.quic != nil {
		if err := tm.quic.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if tm.tcpws != nil {
		if err := tm.tcpws.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

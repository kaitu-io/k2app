package wire

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"
)

// MultiTransportError is returned when both QUIC and TCP-WS transports fail.
// Unwrap returns the TCP-WS error (last-resort transport) for classification.
type MultiTransportError struct {
	QUICErr  error
	TCPWSErr error
}

func (e *MultiTransportError) Error() string {
	return fmt.Sprintf("QUIC: %v; TCP-WS: %v", e.QUICErr, e.TCPWSErr)
}

func (e *MultiTransportError) Unwrap() []error {
	return []error{e.QUICErr, e.TCPWSErr}
}

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

	slog.Debug("TransportManager: DialTCP", "addr", addr, "hasQUIC", quic != nil, "hasTCPWS", tcpws != nil)

	var quicErr, tcpwsErr error

	// Try QUIC first (preferred), with time budget so TCP-WS gets a fair chance.
	if quic != nil {
		quicCtx, quicCancel := tm.quicContext(ctx, tcpws != nil)
		slog.Debug("TransportManager: trying QUIC first")
		conn, err := quic.DialTCP(quicCtx, addr)
		quicCancel()
		if err == nil {
			return conn, nil
		}
		slog.Debug("TransportManager: QUIC failed", "err", err)
		quicErr = err
	}

	// Fall back to TCP-WS
	if tcpws != nil {
		slog.Debug("TransportManager: trying TCP-WS fallback")
		conn, err := tcpws.DialTCP(ctx, addr)
		if err == nil {
			return conn, nil
		}
		slog.Debug("TransportManager: TCP-WS failed", "err", err)
		tcpwsErr = err
	}

	if quicErr != nil && tcpwsErr != nil {
		return nil, &MultiTransportError{QUICErr: quicErr, TCPWSErr: tcpwsErr}
	}
	if tcpwsErr != nil {
		return nil, fmt.Errorf("TCP-WS: %w", tcpwsErr)
	}
	if quicErr != nil {
		return nil, fmt.Errorf("QUIC: %w", quicErr)
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

	slog.Debug("TransportManager: DialUDP", "addr", addr, "hasQUIC", quic != nil, "hasTCPWS", tcpws != nil)

	var quicErr, tcpwsErr error

	if quic != nil {
		quicCtx, quicCancel := tm.quicContext(ctx, tcpws != nil)
		slog.Debug("TransportManager: trying QUIC for UDP")
		conn, err := quic.DialUDP(quicCtx, addr)
		quicCancel()
		if err == nil {
			return conn, nil
		}
		slog.Debug("TransportManager: QUIC UDP failed", "err", err)
		quicErr = err
	}

	if tcpws != nil {
		slog.Debug("TransportManager: trying TCP-WS for UDP fallback")
		conn, err := tcpws.DialUDP(ctx, addr)
		if err == nil {
			return conn, nil
		}
		slog.Debug("TransportManager: TCP-WS UDP failed", "err", err)
		tcpwsErr = err
	}

	if quicErr != nil && tcpwsErr != nil {
		return nil, &MultiTransportError{QUICErr: quicErr, TCPWSErr: tcpwsErr}
	}
	if tcpwsErr != nil {
		return nil, fmt.Errorf("TCP-WS: %w", tcpwsErr)
	}
	if quicErr != nil {
		return nil, fmt.Errorf("QUIC: %w", quicErr)
	}
	return nil, ErrNotConnected
}

// quicContext returns a sub-context for QUIC with a time budget.
// When TCP-WS fallback exists, QUIC gets at most half the remaining time
// (capped at 3s) so TCP-WS always has a fair chance to connect.
// When QUIC is the only transport, it gets the full parent context.
func (tm *TransportManager) quicContext(parent context.Context, hasFallback bool) (context.Context, context.CancelFunc) {
	if !hasFallback {
		return context.WithCancel(parent)
	}

	const maxQUICBudget = 3 * time.Second

	if deadline, ok := parent.Deadline(); ok {
		budget := time.Until(deadline) / 2
		if budget > maxQUICBudget {
			budget = maxQUICBudget
		}
		if budget < time.Second {
			budget = time.Second
		}
		return context.WithTimeout(parent, budget)
	}

	return context.WithTimeout(parent, maxQUICBudget)
}

// Resettable is an optional interface for transports that support connection reset.
type Resettable interface {
	resetConnection()
}

// ResetConnections resets cached connections on all transports.
// Transports that implement Resettable will have their connections cleared,
// forcing new connections on the next dial attempt.
func (tm *TransportManager) ResetConnections() {
	tm.mu.RLock()
	quic := tm.quic
	tcpws := tm.tcpws
	tm.mu.RUnlock()

	if r, ok := quic.(Resettable); ok {
		r.resetConnection()
	}
	if r, ok := tcpws.(Resettable); ok {
		r.resetConnection()
	}
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

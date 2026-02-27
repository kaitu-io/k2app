package wire

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
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

// QUIC backoff constants for fallback recovery.
const (
	quicBackoffBase = 30 * time.Second  // initial backoff after first QUIC failure
	quicBackoffMax  = 120 * time.Second // maximum backoff cap
)

// TransportManager manages QUIC and TCP-WS transports.
// QUIC is preferred; TCP-WS is used as fallback.
// Implements the Dialer interface.
type TransportManager struct {
	mu    sync.RWMutex
	quic  Dialer
	tcpws Dialer

	// Fallback state: tracks QUIC→TCP-WS transport-level fallback.
	// Set when QUIC fails and TCP-WS successfully serves traffic.
	// Cleared on QUIC recovery or ResetConnections().
	fallbackActive   atomic.Bool
	quicBackoffUntil atomic.Int64 // UnixNano: skip QUIC until this time
	quicFailCount    atomic.Int32 // consecutive QUIC dial failures
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

// inQuicBackoff returns true if QUIC should be skipped (backoff period active).
func (tm *TransportManager) inQuicBackoff() bool {
	until := tm.quicBackoffUntil.Load()
	return until > 0 && time.Now().UnixNano() < until
}

// recordQuicSuccess clears all fallback state when QUIC succeeds.
func (tm *TransportManager) recordQuicSuccess() {
	tm.fallbackActive.Store(false)
	tm.quicBackoffUntil.Store(0)
	tm.quicFailCount.Store(0)
}

// recordQuicFailure sets fallback state and exponential backoff.
// Backoff schedule: 30s → 60s → 120s (cap).
func (tm *TransportManager) recordQuicFailure() {
	tm.fallbackActive.Store(true)
	count := tm.quicFailCount.Add(1)
	backoff := quicBackoffBase
	for i := int32(1); i < count && backoff < quicBackoffMax; i++ {
		backoff *= 2
	}
	if backoff > quicBackoffMax {
		backoff = quicBackoffMax
	}
	tm.quicBackoffUntil.Store(time.Now().Add(backoff).UnixNano())
	slog.Info("TransportManager: QUIC backoff active",
		"failCount", count, "backoff", backoff)
}

// FallbackActive returns whether QUIC has failed and TCP-WS is serving traffic.
func (tm *TransportManager) FallbackActive() bool {
	return tm.fallbackActive.Load()
}

// DialTCP opens a TCP proxy connection.
// Prefers QUIC, falls back to TCP-WS. Tracks fallback state for recovery.
func (tm *TransportManager) DialTCP(ctx context.Context, addr string) (net.Conn, error) {
	tm.mu.RLock()
	quic := tm.quic
	tcpws := tm.tcpws
	tm.mu.RUnlock()

	slog.Debug("TransportManager: DialTCP", "addr", addr, "hasQUIC", quic != nil, "hasTCPWS", tcpws != nil)

	var quicErr error

	// Try QUIC first (preferred), skip if in backoff period.
	if quic != nil && !tm.inQuicBackoff() {
		quicCtx, quicCancel := tm.quicContext(ctx, tcpws != nil)
		slog.Debug("TransportManager: trying QUIC first")
		conn, err := quic.DialTCP(quicCtx, addr)
		quicCancel()
		if err == nil {
			tm.recordQuicSuccess()
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
			if quicErr != nil {
				tm.recordQuicFailure()
			}
			return conn, nil
		}
		slog.Debug("TransportManager: TCP-WS failed", "err", err)
		if quicErr != nil {
			return nil, &MultiTransportError{QUICErr: quicErr, TCPWSErr: err}
		}
		return nil, fmt.Errorf("TCP-WS: %w", err)
	}

	if quicErr != nil {
		return nil, fmt.Errorf("QUIC: %w", quicErr)
	}
	return nil, ErrNotConnected
}

// DialUDP opens a UDP proxy connection.
// Prefers QUIC (datagram), falls back to TCP-WS (smux stream).
// ErrDatagramUnhealthy is not counted as a QUIC transport failure (F1 handles it).
func (tm *TransportManager) DialUDP(ctx context.Context, addr string) (net.PacketConn, error) {
	tm.mu.RLock()
	quic := tm.quic
	tcpws := tm.tcpws
	tm.mu.RUnlock()

	slog.Debug("TransportManager: DialUDP", "addr", addr, "hasQUIC", quic != nil, "hasTCPWS", tcpws != nil)

	var quicErr error

	if quic != nil && !tm.inQuicBackoff() {
		quicCtx, quicCancel := tm.quicContext(ctx, tcpws != nil)
		slog.Debug("TransportManager: trying QUIC for UDP")
		conn, err := quic.DialUDP(quicCtx, addr)
		quicCancel()
		if err == nil {
			tm.recordQuicSuccess()
			return conn, nil
		}
		slog.Debug("TransportManager: QUIC UDP failed", "err", err)
		quicErr = err
	}

	if tcpws != nil {
		slog.Debug("TransportManager: trying TCP-WS for UDP fallback")
		conn, err := tcpws.DialUDP(ctx, addr)
		if err == nil {
			// ErrDatagramUnhealthy means QUIC works but datagrams are bad.
			// F1 hop logic handles this — don't count as transport failure.
			if quicErr != nil && !errors.Is(quicErr, ErrDatagramUnhealthy) {
				tm.recordQuicFailure()
			}
			return conn, nil
		}
		slog.Debug("TransportManager: TCP-WS UDP failed", "err", err)
		if quicErr != nil {
			return nil, &MultiTransportError{QUICErr: quicErr, TCPWSErr: err}
		}
		return nil, fmt.Errorf("TCP-WS: %w", err)
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

// ResetConnections resets cached connections on all transports and clears
// fallback state, giving QUIC a fresh chance on the next dial.
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

	// Clear fallback state — give QUIC a fresh chance.
	tm.fallbackActive.Store(false)
	tm.quicBackoffUntil.Store(0)
	tm.quicFailCount.Store(0)
}

// WireStats returns stats from the active transport.
// When in fallback mode, reports TCP-WS stats with Fallback flag set.
func (tm *TransportManager) WireStats() WireStats {
	tm.mu.RLock()
	q, t := tm.quic, tm.tcpws
	tm.mu.RUnlock()

	if tm.fallbackActive.Load() {
		if sp, ok := t.(StatsProvider); ok {
			ws := sp.WireStats()
			ws.Fallback = true
			return ws
		}
	}

	if sp, ok := q.(StatsProvider); ok {
		return sp.WireStats()
	}
	if sp, ok := t.(StatsProvider); ok {
		return sp.WireStats()
	}
	return WireStats{}
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

package wire

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync/atomic"
	"time"

	"github.com/sasha-s/go-deadlock"
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

const (
	// quicProbeInterval is how often the background probe attempts QUIC
	// recovery while TCP-WS fallback is active.
	quicProbeInterval = 30 * time.Second

	// quicConnFailThreshold is how many consecutive QUIC connection (handshake)
	// failures to different hop ports before activating TCP-WS fallback.
	quicConnFailThreshold int32 = 3
)

// TransportManager manages QUIC and TCP-WS transports.
// QUIC is strongly preferred; TCP-WS is used as last-resort fallback only
// when QUIC is genuinely unavailable (UDP blocked). ISPs rarely block UDP,
// so even with 40%+ packet loss, QUIC with port hopping outperforms TCP-WS.
type TransportManager struct {
	mu    deadlock.RWMutex
	quic  Dialer
	tcpws Dialer

	// Fallback state: tracks QUIC→TCP-WS transport-level fallback.
	// Set when consecutive QUIC handshake failures reach quicConnFailThreshold.
	// Cleared on QUIC recovery, ResetConnections(), or successful probe.
	fallbackActive atomic.Bool

	// Post-reset grace: first QUIC dial after ResetConnections gets extended budget.
	freshAfterReset atomic.Bool

	// Background QUIC probe: actively tests QUIC recovery during fallback.
	probeCancel context.CancelFunc
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

// recordQuicSuccess clears fallback state when QUIC succeeds.
func (tm *TransportManager) recordQuicSuccess() {
	tm.mu.RLock()
	if qc, ok := tm.quic.(*QUICClient); ok {
		qc.consecConnFails.Store(0)
	}
	tm.mu.RUnlock()

	wasFallback := tm.fallbackActive.Swap(false)
	if wasFallback {
		slog.Info("wire: QUIC recovered from fallback")
	}
}

// SetFallbackActive sets or clears the QUIC→TCP-WS fallback flag.
func (tm *TransportManager) SetFallbackActive(active bool) {
	if active {
		if tm.fallbackActive.CompareAndSwap(false, true) {
			slog.Warn("wire: QUIC fallback activated")
		}
	} else {
		tm.fallbackActive.Store(false)
	}
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

	// Try QUIC first (preferred), skip if fallback is active.
	if quic != nil && !tm.fallbackActive.Load() {
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
		if qc, ok := quic.(*QUICClient); ok && qc.ConsecConnFails() >= quicConnFailThreshold {
			tm.SetFallbackActive(true)
		}
	} else if quic != nil {
		slog.Debug("TransportManager: QUIC skipped, fallback active")
	}

	// Fall back to TCP-WS
	if tcpws != nil {
		slog.Debug("TransportManager: trying TCP-WS fallback")
		conn, err := tcpws.DialTCP(ctx, addr)
		if err == nil {
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

	if quic != nil && !tm.fallbackActive.Load() {
		quicCtx, quicCancel := tm.quicContext(ctx, tcpws != nil)
		slog.Debug("TransportManager: trying QUIC for UDP")
		conn, err := quic.DialUDP(quicCtx, addr)
		quicCancel()
		if err == nil {
			// Don't call recordQuicSuccess() here. QUIC datagram success
			// doesn't prove the TCP stream path works. Let DialTCP manage
			// its own recovery when TCP actually succeeds.
			slog.Debug("TransportManager: QUIC UDP success", "addr", addr)
			return conn, nil
		}
		slog.Debug("TransportManager: QUIC UDP failed", "err", err)
		quicErr = err
		if qc, ok := quic.(*QUICClient); ok && qc.ConsecConnFails() >= quicConnFailThreshold {
			tm.SetFallbackActive(true)
		}
	} else if quic != nil {
		slog.Debug("TransportManager: QUIC UDP skipped, fallback active")
	}

	if tcpws != nil {
		slog.Debug("TransportManager: trying TCP-WS for UDP fallback")
		conn, err := tcpws.DialUDP(ctx, addr)
		if err == nil {
			slog.Debug("TransportManager: TCP-WS UDP success", "addr", addr)
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
// (capped at 5s) so TCP-WS still has a chance if QUIC is truly dead.
// Budget is generous because we trust QUIC — even a congested QUIC path
// is better than falling back to TCP-WS.
func (tm *TransportManager) quicContext(parent context.Context, hasFallback bool) (context.Context, context.CancelFunc) {
	if !hasFallback {
		return context.WithCancel(parent)
	}

	const (
		maxQUICBudget   = 5 * time.Second // steady-state: trust QUIC, give it time
		reconnectBudget = 8 * time.Second // post-reset: full handshake needs more time
	)

	// First QUIC dial after ResetConnections gets extended budget for
	// DNS + TLS/ECH + QUIC handshake on a potentially new network path.
	budget := maxQUICBudget
	if tm.freshAfterReset.CompareAndSwap(true, false) {
		budget = reconnectBudget
	}

	if deadline, ok := parent.Deadline(); ok {
		half := time.Until(deadline) / 2
		if half > budget {
			half = budget
		}
		if half < time.Second {
			half = time.Second
		}
		return context.WithTimeout(parent, half)
	}

	return context.WithTimeout(parent, budget)
}

// StartQuicProbe starts a background goroutine that actively probes QUIC
// recovery while TCP-WS fallback is active. The probe resets the cached
// QUIC connection and attempts a fresh handshake every quicProbeInterval.
// Call this after SetQUIC/SetTCPWS. Stopped by Close() or ResetConnections().
func (tm *TransportManager) StartQuicProbe(ctx context.Context) {
	// Cancel any existing probe.
	if tm.probeCancel != nil {
		tm.probeCancel()
	}
	probeCtx, cancel := context.WithCancel(ctx)
	tm.probeCancel = cancel
	go tm.quicProbeLoop(probeCtx)
}

func (tm *TransportManager) quicProbeLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(quicProbeInterval):
		}

		if !tm.fallbackActive.Load() {
			continue
		}

		tm.mu.RLock()
		q := tm.quic
		tm.mu.RUnlock()

		qc, ok := q.(*QUICClient)
		if !ok || qc == nil {
			continue
		}

		// Reset stale QUIC connection before probing — existing conn is dead.
		qc.resetConnection()

		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		_, err := qc.connect(probeCtx)
		cancel()

		if err == nil {
			slog.Info("wire: QUIC probe succeeded, clearing fallback")
			tm.recordQuicSuccess()
		} else {
			slog.Debug("wire: QUIC probe failed", "err", err)
			// connect() increments consecConnFails, but that's harmless —
			// fallbackActive is already true, so the threshold check is a no-op.
		}
	}
}

// ResetConnections resets cached connections on all transports and clears
// fallback state, giving QUIC a fresh chance on the next dial.
func (tm *TransportManager) ResetConnections() {
	slog.Debug("wire: connections reset",
		"wasFallback", tm.fallbackActive.Load(),
	)
	tm.mu.RLock()
	q := tm.quic
	t := tm.tcpws
	tm.mu.RUnlock()

	if qc, ok := q.(*QUICClient); ok {
		qc.resetConnection()
		qc.datagramHopAttempts.Store(0)
		qc.datagramHopExhausted.Store(0)
		qc.consecConnFails.Store(0)
	}
	if tc, ok := t.(*TCPWSClient); ok {
		tc.resetConnection()
	}

	tm.fallbackActive.Store(false)
	tm.freshAfterReset.Store(true)
}

// TCPRecentlyHealthy returns true if a TCP stream succeeded within the given window.
// Used by health monitor to skip destructive port hops when TCP is working fine.
func (tm *TransportManager) TCPRecentlyHealthy(window time.Duration) bool {
	tm.mu.RLock()
	q := tm.quic
	tm.mu.RUnlock()
	if qc, ok := q.(*QUICClient); ok {
		return qc.tcpRecentlyHealthy(window)
	}
	return false
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

// Close closes both transports and stops the probe goroutine.
func (tm *TransportManager) Close() error {
	if tm.probeCancel != nil {
		tm.probeCancel()
		tm.probeCancel = nil
	}

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

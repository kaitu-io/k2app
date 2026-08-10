package wire

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync/atomic"
	"time"

	"github.com/sasha-s/go-deadlock"
)

// proxyLayerErr is implemented by errors where the wire transport
// (k2 ↔ k2s) was proven working. TCP-WS fallback connects to the
// same server — it won't help with proxy-layer problems.
type proxyLayerErr interface {
	IsProxyLayer() bool
}

// ProxyLayerError wraps any error that occurred after the request
// was successfully sent to the server (stream opened + header written).
// The wire transport worked; TCP-WS fallback to the same server won't help.
type ProxyLayerError struct {
	Err error
}

func (e *ProxyLayerError) Error() string      { return e.Err.Error() }
func (e *ProxyLayerError) Unwrap() error      { return e.Err }
func (e *ProxyLayerError) IsProxyLayer() bool  { return true }

// isProxyLayerError returns true when the Dialer has marked the error
// as proxy-layer (server responded, transport worked). Checked via
// interface — no type enumeration needed.
func isProxyLayerError(err error) bool {
	var pe proxyLayerErr
	return errors.As(err, &pe) && pe.IsProxyLayer()
}

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
	// failures before activating TCP-WS fallback.
	quicConnFailThreshold int32 = 3
)

// TransportManager manages primary and TCP-WS transports.
// QUIC is strongly preferred; TCP-WS is used as last-resort fallback only
// when QUIC is genuinely unavailable (UDP blocked). ISPs rarely block UDP,
// so even with 40%+ packet loss, QUIC outperforms TCP-WS.
type TransportManager struct {
	mu          deadlock.RWMutex
	primary     Dialer // was: quic — the preferred transport (QUIC-443 or tcpws)
	tcpws       Dialer
	primaryType string // "quic-443" or "tcpws"

	// Fallback state: tracks QUIC→TCP-WS transport-level fallback.
	// Set when consecutive QUIC handshake failures reach quicConnFailThreshold.
	// Cleared on QUIC recovery, Reset(), or successful probe.
	fallbackActive atomic.Bool

	// Post-reset grace: first QUIC dial after Reset gets extended budget.
	freshAfterReset atomic.Bool

	// Background QUIC probe: actively tests QUIC recovery during fallback.
	probeCancel context.CancelFunc
}

// NewTransportManager creates a new transport manager.
func NewTransportManager() *TransportManager {
	return &TransportManager{}
}

// SetPrimary sets the primary transport with its type label.
func (tm *TransportManager) SetPrimary(d Dialer, transportType string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.primary = d
	tm.primaryType = transportType
}

// Primary returns the current primary transport.
func (tm *TransportManager) Primary() Dialer {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.primary
}

// PrimaryType returns the type label of the current primary transport.
func (tm *TransportManager) PrimaryType() string {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.primaryType
}

// ReplacePrimary hot-swaps the primary transport during runtime re-race.
func (tm *TransportManager) ReplacePrimary(d Dialer, transportType string) {
	tm.mu.Lock()
	old := tm.primary
	tm.primary = d
	tm.primaryType = transportType
	tm.fallbackActive.Store(false)
	tm.mu.Unlock()

	// Close old primary outside lock (Rule #4).
	if old != nil {
		tm.mu.RLock()
		isTCPWS := old == tm.tcpws
		tm.mu.RUnlock()
		if !isTCPWS {
			old.Close()
		}
	}

	slog.Info("wire: primary transport replaced",
		"type", transportType,
		"hadOld", old != nil,
	)
}

// SetTCPWS sets the TCP-WS transport.
func (tm *TransportManager) SetTCPWS(d Dialer) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.tcpws = d
}

// SwapTCPWS replaces the TCP-WS transport and returns the old one.
// The caller is responsible for closing the returned dialer (outside any lock).
func (tm *TransportManager) SwapTCPWS(d Dialer) Dialer {
	tm.mu.Lock()
	old := tm.tcpws
	tm.tcpws = d
	tm.mu.Unlock()
	return old
}

// recordQuicSuccess clears fallback state when QUIC succeeds.
func (tm *TransportManager) recordQuicSuccess() {
	tm.mu.RLock()
	if qc, ok := tm.primary.(*QUICClient); ok {
		qc.RecordSuccess()
	}
	tm.mu.RUnlock()

	wasFallback := tm.fallbackActive.Swap(false)
	if wasFallback {
		slog.Info("wire: QUIC recovered from fallback")
		slog.Info("DIAG: transport-switch", "from", "tcpws", "to", "quic", "reason", "quic-recovered")
	}
}

// SetFallbackActive sets or clears the QUIC→TCP-WS fallback flag.
func (tm *TransportManager) SetFallbackActive(active bool) {
	tm.SetFallbackActiveWithReason(active, "quic-handshake-threshold")
}

// SetFallbackActiveWithReason sets or clears fallback with a specific reason for diagnostics.
func (tm *TransportManager) SetFallbackActiveWithReason(active bool, reason string) {
	if active {
		if tm.fallbackActive.CompareAndSwap(false, true) {
			slog.Warn("wire: QUIC fallback activated", "reason", reason)
			slog.Warn("DIAG: transport-switch", "from", "quic", "to", "tcpws", "reason", reason)
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
func (tm *TransportManager) DialTCP(ctx context.Context, addr string, earlyData []byte) (net.Conn, error) {
	tm.mu.RLock()
	primary := tm.primary
	tcpws := tm.tcpws
	tm.mu.RUnlock()

	slog.Debug("TransportManager: DialTCP", "addr", addr, "hasPrimary", primary != nil, "hasTCPWS", tcpws != nil)

	var primaryErr error

	// Try primary first (preferred), skip if fallback is active.
	if primary != nil && !tm.fallbackActive.Load() {
		quicCtx, quicCancel := tm.quicContext(ctx, tcpws != nil)
		slog.Debug("TransportManager: trying primary first")
		conn, err := primary.DialTCP(quicCtx, addr, earlyData)
		quicCancel()
		if err == nil {
			tm.recordQuicSuccess()
			return conn, nil
		}
		// Server responded with error — transport worked, proxy layer failed.
		// TCP-WS hits the same server; fallback won't help.
		if isProxyLayerError(err) {
			slog.Debug("TransportManager: primary proxy error, skipping TCP-WS", "err", err)
			return nil, err
		}

		slog.Debug("TransportManager: primary transport error, trying TCP-WS", "err", err)
		primaryErr = err
		if qc, ok := primary.(*QUICClient); ok && qc.ConsecConnFails() >= quicConnFailThreshold {
			tm.SetFallbackActive(true)
		}
	} else if primary != nil {
		slog.Debug("TransportManager: primary skipped, fallback active")
	}

	// Fall back to TCP-WS
	if tcpws != nil {
		slog.Debug("TransportManager: trying TCP-WS fallback")
		conn, err := tcpws.DialTCP(ctx, addr, earlyData)
		if err == nil {
			return conn, nil
		}
		slog.Debug("TransportManager: TCP-WS failed", "err", err)
		if primaryErr != nil {
			return nil, &MultiTransportError{QUICErr: primaryErr, TCPWSErr: err}
		}
		return nil, fmt.Errorf("TCP-WS: %w", err)
	}

	if primaryErr != nil {
		return nil, fmt.Errorf("QUIC: %w", primaryErr)
	}
	return nil, ErrNotConnected
}

// DialUDP opens a UDP proxy connection.
// Prefers QUIC (datagram), falls back to TCP-WS (smux stream).
// ErrDatagramRelayUnhealthy is not counted as a QUIC transport failure (F1 handles it).
func (tm *TransportManager) DialUDP(ctx context.Context, addr string) (net.PacketConn, error) {
	tm.mu.RLock()
	primary := tm.primary
	tcpws := tm.tcpws
	tm.mu.RUnlock()

	slog.Debug("TransportManager: DialUDP", "addr", addr, "hasPrimary", primary != nil, "hasTCPWS", tcpws != nil)

	var primaryErr error

	if primary != nil && !tm.fallbackActive.Load() {
		quicCtx, quicCancel := tm.quicContext(ctx, tcpws != nil)
		slog.Debug("TransportManager: trying primary for UDP")
		conn, err := primary.DialUDP(quicCtx, addr)
		quicCancel()
		if err == nil {
			// Don't call recordQuicSuccess() here. QUIC datagram success
			// doesn't prove the TCP stream path works. Let DialTCP manage
			// its own recovery when TCP actually succeeds.
			slog.Debug("TransportManager: primary UDP success", "addr", addr)
			return conn, nil
		}
		// Server responded with error — transport worked, proxy layer failed.
		if isProxyLayerError(err) {
			slog.Debug("TransportManager: primary UDP proxy error, skipping TCP-WS", "err", err)
			return nil, err
		}

		slog.Debug("TransportManager: primary UDP transport error, trying TCP-WS", "err", err)
		primaryErr = err
		if qc, ok := primary.(*QUICClient); ok && qc.ConsecConnFails() >= quicConnFailThreshold {
			tm.SetFallbackActive(true)
		}
	} else if primary != nil {
		slog.Debug("TransportManager: primary UDP skipped, fallback active")
	}

	if tcpws != nil {
		slog.Debug("TransportManager: trying TCP-WS for UDP fallback")
		conn, err := tcpws.DialUDP(ctx, addr)
		if err == nil {
			slog.Debug("TransportManager: TCP-WS UDP success", "addr", addr)
			return conn, nil
		}
		slog.Debug("TransportManager: TCP-WS UDP failed", "err", err)
		if primaryErr != nil {
			return nil, &MultiTransportError{QUICErr: primaryErr, TCPWSErr: err}
		}
		return nil, fmt.Errorf("TCP-WS: %w", err)
	}

	if primaryErr != nil {
		return nil, fmt.Errorf("QUIC: %w", primaryErr)
	}
	return nil, ErrNotConnected
}

// DialStreamUDP opens a stream-backed UDP session (reliable).
// Prefers QUIC stream, falls back to TCP-WS (already stream-based).
func (tm *TransportManager) DialStreamUDP(ctx context.Context, addr string) (net.PacketConn, error) {
	tm.mu.RLock()
	primary := tm.primary
	tcpws := tm.tcpws
	tm.mu.RUnlock()

	slog.Debug("TransportManager: DialStreamUDP", "addr", addr, "hasPrimary", primary != nil, "hasTCPWS", tcpws != nil)

	var primaryErr error

	if primary != nil && !tm.fallbackActive.Load() {
		quicCtx, quicCancel := tm.quicContext(ctx, tcpws != nil)
		conn, err := primary.DialStreamUDP(quicCtx, addr)
		quicCancel()
		if err == nil {
			return conn, nil
		}
		if isProxyLayerError(err) {
			return nil, err
		}
		slog.Debug("TransportManager: primary stream UDP failed, trying TCP-WS", "err", err)
		primaryErr = err
		if qc, ok := primary.(*QUICClient); ok && qc.ConsecConnFails() >= quicConnFailThreshold {
			tm.SetFallbackActive(true)
		}
	}

	if tcpws != nil {
		conn, err := tcpws.DialStreamUDP(ctx, addr)
		if err == nil {
			return conn, nil
		}
		if primaryErr != nil {
			return nil, &MultiTransportError{QUICErr: primaryErr, TCPWSErr: err}
		}
		return nil, fmt.Errorf("TCP-WS: %w", err)
	}

	if primaryErr != nil {
		return nil, fmt.Errorf("QUIC: %w", primaryErr)
	}
	return nil, ErrNotConnected
}

// Handshake verifies wire connectivity by completing the transport handshake
// (QUIC or TCP-WS) without opening a proxy stream or dialing any target.
// The cached connection is reused by subsequent DialTCP/DialUDP calls.
//
// Follows the same QUIC-preferred, TCP-WS-fallback logic as DialTCP:
// - Try QUIC connect first (skip if fallback active)
// - On QUIC failure, track handshake streak + try TCP-WS
// - Return MultiTransportError if both fail
func (tm *TransportManager) Handshake(ctx context.Context) error {
	tm.mu.RLock()
	primary := tm.primary
	tcpws := tm.tcpws
	tm.mu.RUnlock()

	slog.Debug("TransportManager: Handshake", "hasPrimary", primary != nil, "hasTCPWS", tcpws != nil)

	var primaryErr error

	// Try primary first (preferred), skip if fallback is active.
	if primary != nil && !tm.fallbackActive.Load() {
		quicCtx, quicCancel := tm.quicContext(ctx, tcpws != nil)
		slog.Debug("TransportManager: Handshake trying primary")
		if qc, ok := primary.(*QUICClient); ok {
			_, err := qc.connect(quicCtx)
			quicCancel()
			if err == nil {
				tm.recordQuicSuccess()
				slog.Info("TransportManager: Handshake primary ok")
				return nil
			}
			slog.Debug("TransportManager: Handshake primary failed", "err", err)
			primaryErr = err
			if qc.ConsecConnFails() >= quicConnFailThreshold {
				tm.SetFallbackActive(true)
			}
		} else {
			quicCancel()
		}
	}

	// Fall back to TCP-WS.
	if tcpws != nil {
		slog.Debug("TransportManager: Handshake trying TCP-WS")
		if tc, ok := tcpws.(*TCPWSClient); ok {
			_, err := tc.connect(ctx)
			if err == nil {
				slog.Info("TransportManager: Handshake TCP-WS ok")
				return nil
			}
			slog.Debug("TransportManager: Handshake TCP-WS failed", "err", err)
			if primaryErr != nil {
				return &MultiTransportError{QUICErr: primaryErr, TCPWSErr: err}
			}
			return fmt.Errorf("TCP-WS: %w", err)
		}
	}

	if primaryErr != nil {
		return fmt.Errorf("QUIC: %w", primaryErr)
	}
	return ErrNotConnected
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

	// First QUIC dial after Reset gets extended budget for
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
// Call this after SetPrimary/SetTCPWS. Stopped by Close() or Reset().
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
		p := tm.primary
		tm.mu.RUnlock()

		qc, ok := p.(*QUICClient)
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
			// connect() increments handshakeFailStreak, but that's harmless —
			// fallbackActive is already true, so the threshold check is a no-op.
		}
	}
}

// Reset implements the Dialer Reset contract: resets cached connections on
// the primary and TCP-WS transports and clears fallback state, giving QUIC
// a fresh chance on the next dial. Called by engine on network change.
func (tm *TransportManager) Reset() {
	slog.Debug("wire: connections reset",
		"wasFallback", tm.fallbackActive.Load(),
	)
	tm.mu.RLock()
	p := tm.primary
	t := tm.tcpws
	tm.mu.RUnlock()

	if p != nil {
		p.Reset()
	}
	if t != nil {
		t.Reset()
	}

	tm.fallbackActive.Store(false)
	tm.freshAfterReset.Store(true)
}

// TCPRecentlyHealthy returns true if a TCP stream succeeded within the given window.
// Used by health monitor to skip destructive reconnects when TCP is working fine.
func (tm *TransportManager) TCPRecentlyHealthy(window time.Duration) bool {
	tm.mu.RLock()
	p := tm.primary
	tm.mu.RUnlock()
	if qc, ok := p.(*QUICClient); ok {
		return qc.tcpRecentlyHealthy(window)
	}
	return false
}

// WireStats returns stats from the active transport.
// When in fallback mode, reports TCP-WS stats with WireFallback flag set.
func (tm *TransportManager) WireStats() WireStats {
	tm.mu.RLock()
	p, t := tm.primary, tm.tcpws
	tm.mu.RUnlock()

	if tm.fallbackActive.Load() {
		if sp, ok := t.(StatsProvider); ok {
			ws := sp.WireStats()
			ws.WireFallback = true
			return ws
		}
	}

	if sp, ok := p.(StatsProvider); ok {
		return sp.WireStats()
	}
	if sp, ok := t.(StatsProvider); ok {
		return sp.WireStats()
	}
	return WireStats{}
}

// Close closes both transports and stops the probe goroutine.
// probeCancel is accessed without tm.mu — safe because engine state machine
// guarantees Start/Close serialization (no concurrent callers).
func (tm *TransportManager) Close() error {
	if tm.probeCancel != nil {
		tm.probeCancel()
		tm.probeCancel = nil
	}

	// Extract refs under lock, nil them, close outside (Rule #4).
	tm.mu.Lock()
	primary := tm.primary
	tm.primary = nil
	tcpws := tm.tcpws
	tm.tcpws = nil
	tm.mu.Unlock()

	var firstErr error
	// Avoid double-close when primary == tcpws (tcpws won the race).
	if primary != nil && primary != tcpws {
		if err := primary.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if tcpws != nil {
		if err := tcpws.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

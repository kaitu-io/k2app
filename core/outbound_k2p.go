package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/kaitu-io/kuic"
	"github.com/sasha-s/go-deadlock"

	"github.com/kaitu-io/k2/k2p"
	"github.com/kaitu-io/k2/wire"
)

// k2pOutbound routes connections through a k2p peer (Home) via ICE hole punch.
// Lazy-connect: the first DialTCP/DialUDP triggers k2p.Dial(). If the connection
// dies, the next dial automatically re-establishes. Follows the same pattern as
// QUICClient's lazy connect with connectMu serialization.
type k2pOutbound struct {
	cfg  k2p.Config
	name string

	connectMu deadlock.Mutex // serializes connect attempts
	mu        deadlock.Mutex // protects inner
	inner     wire.Dialer    // nil until first connect, from k2p.Dial()
	closed    bool
}

// NewK2POutbound creates a lazy-connect outbound for k2p peer access.
// The k2pURL is parsed immediately; ICE/QUIC dial is deferred to first use.
func NewK2POutbound(k2pURL string) (Outbound, error) {
	cfg, err := k2p.ParseURL(k2pURL)
	if err != nil {
		return nil, fmt.Errorf("k2p outbound: %w", err)
	}
	return &k2pOutbound{
		cfg:  cfg,
		name: "k2p-" + cfg.HomeUDID,
	}, nil
}

func (o *k2pOutbound) DialTCP(ctx context.Context, dest string, earlyData []byte) (net.Conn, error) {
	d, err := o.getOrDial(ctx)
	if err != nil {
		return nil, err
	}
	conn, err := d.DialTCP(ctx, dest, earlyData)
	if err != nil && isDeadConn(err) {
		slog.Info("k2p: connection dead, reconnecting", "peer", o.cfg.HomeUDID, "err", err)
		o.reset()
		d, err = o.getOrDial(ctx)
		if err != nil {
			return nil, err
		}
		return d.DialTCP(ctx, dest, earlyData)
	}
	return conn, err
}

func (o *k2pOutbound) DialUDP(ctx context.Context, dest string) (net.PacketConn, net.Addr, error) {
	d, err := o.getOrDial(ctx)
	if err != nil {
		return nil, nil, err
	}
	conn, err := d.DialUDP(ctx, dest)
	if err != nil && isDeadConn(err) {
		slog.Info("k2p: connection dead, reconnecting", "peer", o.cfg.HomeUDID, "err", err)
		o.reset()
		d, err = o.getOrDial(ctx)
		if err != nil {
			return nil, nil, err
		}
		conn, err = d.DialUDP(ctx, dest)
	}
	return conn, nil, err // nil destAddr = pre-connected
}

func (o *k2pOutbound) Name() string { return o.name }

func (o *k2pOutbound) Close() error {
	o.mu.Lock()
	o.closed = true
	inner := o.inner
	o.inner = nil
	o.mu.Unlock()
	if inner != nil {
		return inner.Close()
	}
	return nil
}

// getOrDial returns the existing dialer or establishes a new k2p connection.
func (o *k2pOutbound) getOrDial(ctx context.Context) (wire.Dialer, error) {
	o.mu.Lock()
	if o.closed {
		o.mu.Unlock()
		return nil, errors.New("k2p: outbound closed")
	}
	if o.inner != nil {
		d := o.inner
		o.mu.Unlock()
		return d, nil
	}
	o.mu.Unlock()

	// Serialize connect attempts.
	o.connectMu.Lock()
	defer o.connectMu.Unlock()

	// Re-check after acquiring connectMu (another goroutine may have connected).
	o.mu.Lock()
	if o.closed {
		o.mu.Unlock()
		return nil, errors.New("k2p: outbound closed")
	}
	if o.inner != nil {
		d := o.inner
		o.mu.Unlock()
		return d, nil
	}
	o.mu.Unlock()

	// Perform ICE negotiation + QUIC dial.
	slog.Info("k2p: connecting", "peer", o.cfg.HomeUDID)
	start := time.Now()
	result, err := k2p.Dial(ctx, o.cfg)
	if err != nil {
		return nil, fmt.Errorf("k2p: dial %s: %w", o.cfg.HomeUDID, err)
	}
	slog.Info("k2p: connected",
		"peer", o.cfg.HomeUDID,
		"transport", result.TransportType,
		"dialMs", time.Since(start).Milliseconds())

	o.mu.Lock()
	if o.closed {
		o.mu.Unlock()
		result.Dialer.Close()
		return nil, errors.New("k2p: outbound closed during connect")
	}
	o.inner = result.Dialer
	o.mu.Unlock()

	return result.Dialer, nil
}

// reset clears the inner dialer so the next getOrDial re-connects.
func (o *k2pOutbound) reset() {
	o.mu.Lock()
	inner := o.inner
	o.inner = nil
	o.mu.Unlock()
	if inner != nil {
		inner.Close()
	}
}

// isDeadConn returns true if the error indicates the underlying QUIC connection is dead
// and a fresh k2p.Dial() is needed. Uses typed error checks (quic-go error types)
// per Error Ownership Rules — no string matching.
func isDeadConn(err error) bool {
	if err == nil {
		return false
	}
	// Caller-initiated cancellation is not connection death.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	// Standard Go network errors.
	if errors.Is(err, net.ErrClosed) {
		return true
	}
	// QUIC idle timeout (peer gone or network changed).
	var idleErr *quic.IdleTimeoutError
	if errors.As(err, &idleErr) {
		return true
	}
	// QUIC stateless reset (peer rebooted).
	var resetErr *quic.StatelessResetError
	if errors.As(err, &resetErr) {
		return true
	}
	// QUIC application error code 0 = graceful close.
	var appErr *quic.ApplicationError
	if errors.As(err, &appErr) && appErr.ErrorCode == 0 {
		return true
	}
	// QUIC transport error (INTERNAL_ERROR, CONNECTION_REFUSED, etc.).
	var transportErr *quic.TransportError
	if errors.As(err, &transportErr) {
		return true
	}
	return false
}

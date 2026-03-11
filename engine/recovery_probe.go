package engine

import (
	"context"
	"log/slog"
	"net"
	"time"

	"github.com/sasha-s/go-deadlock"

	"github.com/kaitu-io/k2/safego"
	"github.com/kaitu-io/k2/wire"
)

const (
	probeInitialDelay  = 3 * time.Second  // wait for network stack + DHCP + routes
	probeDialTimeout   = 5 * time.Second  // per-attempt DialTCP timeout
	probeRetryInterval = 10 * time.Second // between failed attempts
	probeMaxAttempts   = 3                // give up after this many failures
	probeTarget        = "1.1.1.1:443"    // same as Start() step 4.5
)

// probeDialer is the subset of wire.TransportManager needed by recoveryProbe.
type probeDialer interface {
	DialTCP(ctx context.Context, addr string, earlyData []byte) (net.Conn, error)
}

// Compile-time check: TransportManager satisfies probeDialer.
var _ probeDialer = (*wire.TransportManager)(nil)

// recoveryProbe actively verifies wire connectivity after a network change.
// Triggered by OnNetworkChanged(), it runs a short-lived probe loop that
// dials the server to force fresh transport connections when no app traffic
// is flowing. Each trigger() replaces any in-flight probe cycle.
type recoveryProbe struct {
	dialer probeDialer
	engine *Engine

	mu     deadlock.Mutex
	cancel context.CancelFunc
}

func newRecoveryProbe(dialer probeDialer, engine *Engine) *recoveryProbe {
	return &recoveryProbe{dialer: dialer, engine: engine}
}

// trigger starts a new probe cycle, cancelling any in-flight one.
func (p *recoveryProbe) trigger() {
	p.mu.Lock()
	if p.cancel != nil {
		p.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	p.mu.Unlock()

	safego.Go(func() { p.run(ctx) })
}

// stop cancels any in-flight probe. Safe to call multiple times.
func (p *recoveryProbe) stop() {
	p.mu.Lock()
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	p.mu.Unlock()
}

func (p *recoveryProbe) run(ctx context.Context) {
	// Initial delay: network stack stabilization + dedup rapid OnNetworkChanged calls.
	select {
	case <-ctx.Done():
		return
	case <-time.After(probeInitialDelay):
	}

	for attempt := 1; attempt <= probeMaxAttempts; attempt++ {
		// Check engine state: only probe when connected and not paused.
		p.engine.mu.Lock()
		state := p.engine.state
		paused := p.engine.paused
		lastErr := p.engine.lastError
		p.engine.mu.Unlock()

		if state != StateConnected || paused {
			slog.Debug("recovery: skip probe, engine not connected", "state", state, "paused", paused)
			return
		}
		if lastErr != nil && lastErr.Category == CategoryClient {
			slog.Debug("recovery: skip probe, client error active", "code", lastErr.Code)
			return
		}

		slog.Info("recovery: probing wire connectivity", "attempt", attempt)
		probeCtx, probeCancel := context.WithTimeout(ctx, probeDialTimeout)
		conn, err := p.dialer.DialTCP(probeCtx, probeTarget, nil)
		probeCancel()

		if ctx.Err() != nil {
			return // cancelled by new trigger() or stop()
		}

		if err == nil {
			conn.Close()
			slog.Info("recovery: probe succeeded", "attempt", attempt)
			p.engine.ClearWireError()
			return
		}

		slog.Warn("recovery: probe failed", "attempt", attempt, "err", err)
		p.engine.ReportWireError(err)

		// Check if classified as client error (e.g. 401 from server).
		classified := ClassifyError(err)
		if classified.Category == CategoryClient {
			slog.Info("recovery: stopping probes, client error", "code", classified.Code)
			return
		}

		if attempt < probeMaxAttempts {
			select {
			case <-ctx.Done():
				return
			case <-time.After(probeRetryInterval):
			}
		}
	}

	slog.Warn("recovery: all probe attempts failed, waiting for next network change")
}

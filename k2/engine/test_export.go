//go:build !release

package engine

// Test-only helpers for cross-package E2E tests under k2/test/.
//
// File excluded from release builds via the `release` build tag. Default
// builds (developer + CI test) compile this file; production release builds
// must pass `-tags release` (e.g. via Makefile / release.sh) to strip it.
//
// Why this is needed: cross-package tests cannot see _test.go files, so
// these helpers must live in a regular .go file. The `!release` constraint
// guarantees the symbols never reach a tagged production binary even by
// accident.
//
// Do NOT call any of these from production code paths — they exist solely
// to let test code construct outboundTransport instances and drive
// replaceOutboundServer without spinning up the full engine.Start()
// pipeline.

import (
	"context"
	"time"

	"github.com/kaitu-io/k2/wire"
)

// ForceConnected sets the engine to connected state and fires OnStatus.
// For testing only — allows daemon tests to simulate a connected engine.
// Releases lock before calling handler to match OnNetworkChanged/Pause/Wake pattern.
func (e *Engine) ForceConnected() {
	e.mu.Lock()
	e.state = StateConnected
	e.connectedAt = time.Now()
	e.lastError = nil
	status := e.buildStatusLocked()
	handler := e.handler
	e.mu.Unlock()
	if handler != nil {
		handler.OnStatus(status)
	}
}

// OutboundTransportShape is the exported alias for outboundTransport.
// Used only by E2E test helpers in package test that need to construct
// outboundTransport instances and pass them through engine APIs.
type OutboundTransportShape = outboundTransport

// NewTestTarget constructs an outboundTransport with a fresh TransportManager
// and SwappableDialer for the given URL. The TM has no live transports — it's
// the engine.Start() shape minus actual wire connectivity. Suitable for
// driving replaceOutboundServer in tests that fake setupOutboundTransportFn.
func NewTestTarget(url string) *OutboundTransportShape {
	tm := wire.NewTransportManager()
	return &outboundTransport{
		tm:        tm,
		wireCfg:   wire.Config{Host: "test", Port: 443},
		dialAddr:  "test:443",
		name:      "k2v5-test",
		url:       url,
		swappable: wire.NewSwappableDialer(tm),
	}
}

// AttachTestTarget wires target into e's outboundTMs slice so
// replaceOutboundServer's "still alive" check finds it. Caller must also
// call ForceConnected (or ensure state is StateConnected) before invoking
// TriggerReplaceForTest.
func AttachTestTarget(e *Engine, target *OutboundTransportShape) {
	e.mu.Lock()
	e.outboundTMs = append(e.outboundTMs, target)
	e.mu.Unlock()
}

// DetachTestTarget removes target from e.outboundTMs. Used by D-group tests
// that simulate Stop() racing with replace.
func DetachTestTarget(e *Engine, target *OutboundTransportShape) {
	e.mu.Lock()
	out := e.outboundTMs[:0]
	for _, ot := range e.outboundTMs {
		if ot != target {
			out = append(out, ot)
		}
	}
	e.outboundTMs = out
	e.mu.Unlock()
}

// TriggerReplaceForTest drives engine.replaceOutboundServer directly. This
// bypasses the reRaceChan / outboundReplaceLoop pump, so tests can exercise
// the swap logic deterministically without spinning up the full engine.
func TriggerReplaceForTest(
	e *Engine,
	target *OutboundTransportShape,
	reason error,
	dd wire.DirectDialer,
	provider OutboundProvider,
) error {
	return e.replaceOutboundServer(context.Background(), target, reason, dd, provider)
}

// TriggerReplaceForTestCtx is TriggerReplaceForTest with a caller-supplied ctx
// (D-group cancellation tests).
func TriggerReplaceForTestCtx(
	ctx context.Context,
	e *Engine,
	target *OutboundTransportShape,
	reason error,
	dd wire.DirectDialer,
	provider OutboundProvider,
) error {
	return e.replaceOutboundServer(ctx, target, reason, dd, provider)
}

// SetSetupOutboundTransportFnShape replaces the package-level
// setupOutboundTransportFn. Returns the previous value so callers can restore
// it via t.Cleanup.
func SetSetupOutboundTransportFnShape(
	fn func(ctx context.Context, via string, dd wire.DirectDialer, wireMode string, clientVersion string) (*OutboundTransportShape, wire.Dialer, error),
) func(ctx context.Context, via string, dd wire.DirectDialer, wireMode string, clientVersion string) (*OutboundTransportShape, wire.Dialer, error) {
	prev := setupOutboundTransportFn
	setupOutboundTransportFn = fn
	return prev
}

// NewOutboundTransportForSetup constructs an outboundTransport that
// fakeWireSetup can return from its replacement of setupOutboundTransportFn.
// dialer is what the SwappableDialer will route to (typically a closeTracker
// wrapper around a fresh TransportManager).
func NewOutboundTransportForSetup(url string, tm *wire.TransportManager) *OutboundTransportShape {
	return &outboundTransport{
		tm:        tm,
		wireCfg:   wire.Config{Host: "replacement", Port: 443},
		dialAddr:  "replacement:443",
		name:      "k2v5-replacement",
		url:       url,
		swappable: wire.NewSwappableDialer(tm),
	}
}

// Outbound accessors used by package test assertions.
func (o *outboundTransport) URL() string                       { return o.url }
func (o *outboundTransport) Name() string                      { return o.name }
func (o *outboundTransport) TM() *wire.TransportManager        { return o.tm }
func (o *outboundTransport) Swappable() *wire.SwappableDialer  { return o.swappable }

// ReRaceFailStreak returns the current Phase-A failure count. Reads under
// e.mu via the engine reference. Used by A2 to assert the streak resets to 0
// after a successful swap.
func ReRaceFailStreak(e *Engine, target *OutboundTransportShape) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return target.reRaceFailStreak
}

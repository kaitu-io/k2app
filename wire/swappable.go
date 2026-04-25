package wire

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
)

// ErrSwappableEmpty is returned by SwappableDialer's Dial methods when no
// underlying Dialer is installed. It should not occur in normal operation —
// SwappableDialer is constructed with a non-nil Dialer and Swap rejects nil.
var ErrSwappableEmpty = errors.New("wire: SwappableDialer has no Dialer installed")

// SwappableDialer is a wire.Dialer whose underlying Dialer can be replaced
// atomically at runtime. Dial calls always route to the currently-installed
// Dialer.
//
// Use case: the engine installs a SwappableDialer into the outbound table at
// Start. On tunnel-server failure, the engine builds a replacement Dialer
// (new k2v5:// URL → new TransportManager) in the background; once ready,
// calling Swap atomically redirects future Dial calls to the new Dialer
// without tearing down TUN, provider, DNS hijack, rule engine, or connTracker.
//
// Concurrency model — linearizable, lock-free:
//   - Dial* methods load the pointer once, then call through. In-flight Dials
//     that captured the old pointer complete (or fail fast) against the old
//     Dialer; the swap does not interrupt them.
//   - Swap is a single atomic store. Before store: all Dials go to old.
//     After store: all Dials go to new.
//   - Swap returns the replaced Dialer; the caller owns it and must Close()
//     when in-flight operations have drained (or immediately, if the caller
//     is willing to fail-fast outstanding Dials — quic-go / smux both return
//     context-cancelled errors from OpenStreamSync when the underlying
//     connection Close()s, no hang).
//
// Package placement: lives in wire/ because it implements wire.Dialer and has
// no knowledge of higher-layer concepts (subscription, outbound table).
type SwappableDialer struct {
	// current always holds a non-nil *Dialer after New. atomic.Pointer gives
	// lock-free Load/Store with sequential consistency.
	current atomic.Pointer[Dialer]
}

// NewSwappableDialer wraps an initial Dialer. Panics if d is nil —
// a SwappableDialer without a Dialer is never a valid state.
func NewSwappableDialer(d Dialer) *SwappableDialer {
	if d == nil {
		panic("wire: NewSwappableDialer: nil dialer")
	}
	s := &SwappableDialer{}
	s.current.Store(&d)
	return s
}

// Current returns the currently-installed Dialer. Useful when callers need
// to type-assert for optional interfaces (StatsProvider, EchoProber).
func (s *SwappableDialer) Current() Dialer {
	p := s.current.Load()
	if p == nil {
		return nil
	}
	return *p
}

// Swap atomically installs newD and returns the previously-installed Dialer.
// The caller owns the returned Dialer and is responsible for Close()ing it.
// Panics if newD is nil.
func (s *SwappableDialer) Swap(newD Dialer) Dialer {
	if newD == nil {
		panic("wire: SwappableDialer.Swap: nil dialer")
	}
	old := s.current.Swap(&newD)
	if old == nil {
		return nil
	}
	return *old
}

// DialTCP forwards to the current Dialer.
func (s *SwappableDialer) DialTCP(ctx context.Context, addr string, earlyData []byte) (net.Conn, error) {
	d := s.Current()
	if d == nil {
		return nil, ErrSwappableEmpty
	}
	return d.DialTCP(ctx, addr, earlyData)
}

// DialUDP forwards to the current Dialer.
func (s *SwappableDialer) DialUDP(ctx context.Context, addr string) (net.PacketConn, error) {
	d := s.Current()
	if d == nil {
		return nil, ErrSwappableEmpty
	}
	return d.DialUDP(ctx, addr)
}

// DialStreamUDP forwards to the current Dialer.
func (s *SwappableDialer) DialStreamUDP(ctx context.Context, addr string) (net.PacketConn, error) {
	d := s.Current()
	if d == nil {
		return nil, ErrSwappableEmpty
	}
	return d.DialStreamUDP(ctx, addr)
}

// Close closes the currently-installed Dialer. Note this does NOT clear the
// pointer — subsequent Dial calls forward to an already-closed Dialer, which
// will return its own closed-state error. SwappableDialer is designed so that
// Close is a rare terminal operation (engine Stop); hot swaps go through
// Swap() instead, and the caller Close()s the returned old Dialer.
func (s *SwappableDialer) Close() error {
	d := s.Current()
	if d == nil {
		return nil
	}
	return d.Close()
}

// Reset forwards to the current Dialer if it supports the Dialer contract's
// Reset method. SwappableDialer is itself a Dialer; its Reset clears the
// inner Dialer's cached connection state. Note this does NOT replace the
// inner Dialer — Reset is for connection-level state, Swap is for Dialer
// replacement.
func (s *SwappableDialer) Reset() {
	d := s.Current()
	if d == nil {
		return
	}
	d.Reset()
}

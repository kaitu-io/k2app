package wire

import (
	"context"
	"errors"
	"time"
)

// User-dial connect-failure evidence.
//
// engine's echo probe can only test a connection that exists. A node that
// refuses or kills every connection (process down, port closed, auth reject
// at metadata) leaves no cached connection, so every probe returns
// ErrEchoNoConnection — "no evidence either way" — and the probe alone can
// never tell that node from an idle client whose connection was dropped by a
// Reset. User traffic can: every user dial without a live connection redials
// lazily, and if those redials keep failing the node is dead. If there is no
// traffic, nobody needs the node and nothing here changes.
//
// So TransportManager records the outcome of USER dials only (DialTCP /
// DialUDP / DialStreamUDP — the paths core's wireOutbound and the DNS proxy
// use). The echo probe (RunEchoProbe, which for TCP-WS connects on its own)
// and wire's QUIC recovery probe (runProbeOnce) bypass these methods, so a
// probe can never manufacture the evidence that makes its own result count.
// engine reads it through ConnectFailures; see engine/health.go
// notProbedDialFailing.

// ConnectFailures reports the user-dial connect-failure evidence: seq counts
// every connect-level failure ever recorded on this manager (monotonic; a
// caller compares it with the value it saw last time to learn whether a NEW
// failure happened in between), and last is the time of the newest failure
// that is still unresolved — zero once a later user dial was answered, or
// after Reset / ResetConnections / ReplacePrimary.
func (tm *TransportManager) ConnectFailures() (seq uint64, last time.Time) {
	seq = tm.connFailSeq.Load()
	if ns := tm.connFailAt.Load(); ns != 0 {
		last = time.Unix(0, ns)
	}
	return seq, last
}

// clearConnFailure drops the unresolved failure and starts a new generation,
// so a dial already in flight against the old connection cannot record
// evidence after the clear. Called by Reset, ResetConnections and
// ReplacePrimary.
func (tm *TransportManager) clearConnFailure() {
	tm.connGen.Add(1)
	tm.connFailAt.Store(0)
}

// recordUserDial classifies one finished user dial. gen is connGen as read
// when the dial started.
//
//   - Answered (nil, or a proxy-layer error carrying a server status such as
//     a StreamError — the target, not the node, failed): the node is alive,
//     the unresolved failure is cleared.
//   - Connect-level failure (any transport-layer error: handshake failure or
//     timeout, refused, unreachable, stream open on a dead connection; or an
//     explicit metadata auth rejection, which closes the connection before
//     it is cached): recorded.
//   - Everything else is no opinion: the caller's own ctx ended (the app gave
//     up, not the node), ErrNotConnected (closed manager / stale-connection
//     race), ErrDatagramRelayUnhealthy (relay-layer signal), and a request
//     sent on a live connection that was never answered (a response timeout
//     is data-path evidence, owned by recordQUICDataPath; that connection
//     exists, so the echo probe tests it directly).
//
// Atomics only — never takes tm.mu.
func (tm *TransportManager) recordUserDial(ctx context.Context, gen uint64, err error, now time.Time) {
	if tm.connGen.Load() != gen {
		return // straddled a Reset / ResetConnections / ReplacePrimary
	}
	if err == nil {
		tm.connFailAt.Store(0)
		return
	}
	if ctx.Err() != nil || errors.Is(err, ErrNotConnected) || errors.Is(err, ErrDatagramRelayUnhealthy) {
		return
	}
	if isProxyLayerError(err) && !errors.Is(err, ErrMetadataRejected) {
		if !isUnanswered(err) {
			tm.connFailAt.Store(0) // the server answered
		}
		return
	}
	tm.connFailSeq.Add(1)
	tm.connFailAt.Store(now.UnixNano())
}

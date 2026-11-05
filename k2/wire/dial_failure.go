package wire

import (
	"context"
	"errors"
	"time"

	"github.com/sasha-s/go-deadlock"
)

// User-dial connect-failure evidence.
//
// engine's echo probe can only test a connection that exists. A node that
// refuses or kills every connection (process down, port closed, a coded auth
// reject at metadata) leaves no cached connection, so every probe returns
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

// connFailState is the evidence. seq counts every connect-level failure ever
// recorded (monotonic); at is the newest failure still unresolved (zero =
// none: a later answered dial, Reset, ResetConnections or ReplacePrimary
// clears it); gen is bumped by those three clears, so a dial that started
// before one records nothing after it. All three move together under mu —
// a leaf lock, held only for field reads/writes (no I/O, no other lock) —
// so the generation check and the store are one step: a clear can neither
// slip between them (a pre-clear failure recorded as post-clear) nor be
// overwritten.
type connFailState struct {
	mu  deadlock.Mutex
	seq uint64
	at  time.Time
	gen uint64
}

// ConnectFailures reports the user-dial connect-failure evidence: seq counts
// every connect-level failure ever recorded on this manager (monotonic; a
// caller compares it with the value it saw last time to learn whether a NEW
// failure happened in between), last is the time of the newest failure that
// is still unresolved — zero once a later user dial was answered, or after
// Reset / ResetConnections / ReplacePrimary — and gen is the connection
// generation, which those three bump (a caller holding per-connection state,
// like engine's echo strikes, drops it when gen moves).
func (tm *TransportManager) ConnectFailures() (seq uint64, last time.Time, gen uint64) {
	c := &tm.connFail
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.seq, c.at, c.gen
}

// connGen returns the current connection generation (read at dial start).
func (tm *TransportManager) connGen() uint64 {
	c := &tm.connFail
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gen
}

// clearConnFailure drops the unresolved failure and starts a new generation.
// Called by Reset, ResetConnections and ReplacePrimary.
func (tm *TransportManager) clearConnFailure() {
	c := &tm.connFail
	c.mu.Lock()
	c.gen++
	c.at = time.Time{}
	c.mu.Unlock()
}

// recordUserDial classifies one finished user dial. gen is connGen() as read
// when the dial started.
//
//   - Answered (nil, or a proxy-layer error carrying a server status such as
//     a StreamError — the target, not the node, failed): the unresolved
//     failure is cleared. NOTE "nil" is not always a round trip — a UDP dial
//     over a cached QUIC conn or smux session sends nothing and waits for
//     nothing — so this is fail-safe (it can only clear evidence, delaying
//     detection) and must NOT be reused as proof of life (see engine's
//     suppressStrike, which deliberately counts DialTCP successes only).
//   - Connect-level failure: any transport-layer error (handshake failure or
//     timeout, refused, unreachable, stream open on a dead connection), or a
//     metadata auth reject that carries an explicit code (StreamError: 402
//     subscription, 401/403 Center verdict, 503 node auth fault). Recorded.
//   - Everything else is no opinion: the caller's own ctx ended (the app gave
//     up, not the node), ErrNotConnected (closed manager / stale-connection
//     race), ErrDatagramRelayUnhealthy (relay-layer signal), a request sent
//     on a live connection that was never answered (a response timeout is
//     data-path evidence, owned by recordQUICDataPath; that connection
//     exists, so the echo probe tests it directly), and a CODE-LESS metadata
//     reject. k2s sends the code-less legacy "unauthorized" when it cannot
//     reach Center (undecidable verdict under enforce, metadata_auth.go
//     default branch): that is not this node being dead, and engine
//     classifies it as 401 (user action → fail, no retry), so counting it
//     would turn a Center outage into fleet-wide terminal disconnects instead
//     of today's self-healing lazy redial.
func (tm *TransportManager) recordUserDial(ctx context.Context, gen uint64, err error, now time.Time) {
	failed := false
	switch {
	case err == nil:
	case ctx.Err() != nil, errors.Is(err, ErrNotConnected), errors.Is(err, ErrDatagramRelayUnhealthy):
		return
	case errors.Is(err, ErrMetadataRejected):
		var se *StreamError
		if !errors.As(err, &se) {
			return // code-less: Center unreachable on the node, no opinion
		}
		failed = true
	case isProxyLayerError(err):
		if isUnanswered(err) {
			return
		}
		// the server answered: fall through to clear
	default:
		failed = true
	}

	tm.connFail.store(gen, failed, now)
}

// store applies one classified outcome. The generation check and the write
// are one critical section: a clear can neither slip between them nor be
// overwritten by a dial that started before it.
func (c *connFailState) store(gen uint64, failed bool, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.gen != gen {
		return // straddled a Reset / ResetConnections / ReplacePrimary
	}
	if failed {
		c.seq++
		c.at = now
	} else {
		c.at = time.Time{}
	}
}

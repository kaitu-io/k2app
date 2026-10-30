package wire

import (
	"sync"
	"sync/atomic"
	"time"
)

// Device-auth admission — the single decision point for whether a connection
// may proxy, shared by both transports.
//
// Admission is a property of the CONNECTION, not of an individual stream: the
// metadata stream (StreamType 0x10) carries the credentials once, and every
// proxy path on that connection lives or dies by that one verdict. So it is
// modelled once, here.
//
// Why it is centralised. The enforce gate was first written as a check repeated
// at each egress dispatch site. Three paths shipped without one — TCP-WS proxy
// streams, QUIC datagrams (the primary UDP path), and the QUIC overflow stream
// — each found only in a later review. A gate that must be remembered is a gate
// that gets forgotten, and this one fails OPEN and SILENTLY when it is.
//
// The structural fix is that the gate is not a check but the sole supplier of
// what egress needs: `admitTCP` is the only route from a proxy stream to
// AcceptTCP, and `admitUDP` is the only reader of the server's UDPHandler. A new
// path that forgets to ask has nothing to proxy with.
// TestAdmissionGate_EgressSinksAreGated pins that so it cannot rot.

// connAuth is the per-connection device-auth verdict. ok is the validation
// result; done is closed once the metadata stream has been processed (success,
// failure, or parse error) so a proxy path that raced ahead of the metadata
// stream waits for the verdict instead of being wrongly rejected.
type connAuth struct {
	ok   atomic.Bool
	done chan struct{}
	once sync.Once

	// rolloutCounted guards RolloutStats double-counting. A connection can
	// reach a rollout-counter increment via two independent races: (1) the
	// accept loop's teardown defer (RecordLegacyNoMetadata, when the
	// connection dies before a verdict lands) can run concurrently with the
	// metadata-stream goroutine still resolving its verdict (recordVerdict)
	// — Center-slow/unreachable is exactly the condition both are trying to
	// observe; (2) nothing limits a client to one StreamType 0x10 stream per
	// connection, so recordVerdict can otherwise fire once per stream while
	// RecordConn fires once per connection. rolloutCounted is CAS'd exactly
	// once by whichever caller reaches claimRolloutCount() first, so each
	// connection contributes at most one rollout-counter increment
	// regardless of how many metadata streams it opens or which of the two
	// paths above wins.
	rolloutCounted atomic.Bool
}

func newConnAuth() *connAuth { return &connAuth{done: make(chan struct{})} }

// settle records the verdict and unblocks waiters. The first call wins, so a
// metadata handler can `defer a.settle(false)` as a fail-closed backstop
// covering parse errors and panics, and still report the real verdict on the
// success path.
func (a *connAuth) settle(ok bool) {
	a.once.Do(func() {
		a.ok.Store(ok)
		close(a.done)
	})
}

// authenticated reports the verdict so far, without waiting for one.
func (a *connAuth) authenticated() bool { return a.ok.Load() }

// settled reports whether the metadata verdict has been recorded (done closed)
// — i.e. this connection presented a metadata stream at all. Used by the
// rollout counters to classify legacy (pre-metadata) clients at teardown.
func (a *connAuth) settled() bool {
	select {
	case <-a.done:
		return true
	default:
		return false
	}
}

// claimRolloutCount reports whether the caller is the first to claim this
// connection's rollout-counter increment. The CAS is unconditional and
// idempotent from the caller's point of view: call it right before every
// Record* call that classifies this connection (RecordLegacyNoMetadata at
// teardown, recordVerdict at metadata-stream completion) and only make that
// call when claimRolloutCount returns true. See the rolloutCounted field
// doc for the two races this closes.
func (a *connAuth) claimRolloutCount() bool {
	return a.rolloutCounted.CompareAndSwap(false, true)
}

// admissionPolicy is the server-side enforcement configuration. Both
// QUICServer and TCPWSServer hold one, and k2s always enforces on both:
// enforcement that covered only QUIC would let an attacker force the TCP-WS
// fallback and bypass device auth entirely.
type admissionPolicy struct {
	enforce  bool
	authWait time.Duration
}

// admit reports whether a connection may perform egress.
//
// Permissive (the zero value; k2s never uses it — server.New always enforces,
// so it survives only for wire tests and k2p) never blocks: an unauthenticated connection is still
// served, but attribution follows the verdict, not the self-report — only a
// passing verdict attributes to the sent udid, otherwise the connection lands
// in the empty-udid bucket. Enforce admits a connection whose metadata stream
// validated, waiting up to authWait for a verdict that may not have landed
// yet.
//
// That wait covers only stream-acceptance reordering on the server: the client
// sends metadata synchronously inside connect() and reads the response before
// dialling anything, so on the wire metadata always precedes proxy traffic. It
// is kept short so a legacy client — which never sends metadata at all — is
// rejected promptly rather than stalled, and it is skipped entirely once a
// verdict exists, so a rejected connection pays it once per attempt, not twice.
func (p admissionPolicy) admit(a *connAuth) bool {
	if !p.enforce {
		return true
	}
	if a.ok.Load() {
		return true
	}
	select {
	case <-a.done:
		return a.ok.Load() // verdict already final — nothing to wait for
	default:
	}
	timer := time.NewTimer(p.authWait)
	defer timer.Stop()
	select {
	case <-a.done:
	case <-timer.C:
	}
	return a.ok.Load()
}

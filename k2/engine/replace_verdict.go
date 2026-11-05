package engine

import (
	"errors"

	"github.com/kaitu-io/k2/wire"
)

// Which re-race / candidate-setup errors are a verdict on THIS DEVICE (stop:
// no node can fix it) and which are a server-side transient (keep re-racing,
// keep rotating candidates). Scope: the replace loop only — Phase-A
// (failIfUserAction, every caller), Phase-B candidate setups
// (replaceOutboundServer / failExhausted). ClassifyError's mapping, used at
// initial connect and for lastError, is deliberately unchanged.

// isCodeLessReject reports a metadata / proxy-stream rejection WITHOUT a
// structured code: the legacy 0x01 "unauthorized". k2s sends it when its
// auth verdict is undecidable — typically Center unreachable from the node
// (wire/metadata_auth.go default branch). ClassifyError maps it to 401 for
// initial connect; in the replace loop it must never end the session, or a
// Center outage longer than the cookie/auth-cache TTL (15 min) disconnects
// every client whose connection cycles (review C1/N2).
//
// A metadata response that timed out after the transport handshake
// (wire.ErrMetadataTimeout) is the same case — node alive, Center stuck
// (hanging rather than refusing, PVE S15) — and counts as code-less here:
// never node death toward exhaustion, never user action.
func isCodeLessReject(err error) bool {
	if err == nil {
		return false
	}
	var se *wire.StreamError
	if errors.As(err, &se) {
		return false // a coded verdict anywhere in the chain wins
	}
	return errors.Is(err, wire.ErrMetadataTimeout) ||
		errors.Is(err, wire.ErrMetadataRejected) || errors.Is(err, wire.ErrStreamRejected)
}

// replaceLoopUserAction returns the EngineError to fail with when err is
// Center's explicit verdict on this device, nil otherwise:
//   - code-less reject → nil (server-side transient, see isCodeLessReject);
//   - a coded metadata reject 401 / 402 / 403 (403 covers device class, ban
//     and brand mismatch — 403003 on the HTTP API arrives as 403 on the wire)
//     → that code, CategoryClient;
//   - anything else ClassifyError marks RequiresUserAction (e.g. a k2subs
//     fetch 401/402, or a sticky lastError passed in at exhaustion) → as
//     classified;
//   - a candidate exhaustion (exhaustedError) → its candidates' coded
//     verdict when every candidate tried was rejected with one, else nil.
func replaceLoopUserAction(err error) *EngineError {
	var x *exhaustedError
	if errors.As(err, &x) {
		// A candidate exhaustion is an account verdict only when every
		// candidate tried said so (R1); its chain is the provider's error.
		if x.allUserAction {
			return x.userAction
		}
		return nil
	}
	if err == nil || isCodeLessReject(err) {
		return nil
	}
	if errors.Is(err, wire.ErrMetadataRejected) {
		var se *wire.StreamError
		if errors.As(err, &se) {
			switch se.Code {
			case ErrCodeAuthRejected, ErrCodePaymentRequired, ErrCodeForbidden:
				return ClassifyError(err)
			}
		}
		return nil
	}
	if ce := ClassifyError(err); ce.RequiresUserAction() {
		return ce
	}
	return nil
}

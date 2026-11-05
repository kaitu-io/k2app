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
func isCodeLessReject(err error) bool {
	if err == nil {
		return false
	}
	if !errors.Is(err, wire.ErrMetadataRejected) && !errors.Is(err, wire.ErrStreamRejected) {
		return false
	}
	var se *wire.StreamError
	return !errors.As(err, &se)
}

// replaceLoopUserAction returns the EngineError to fail with when err is
// Center's explicit verdict on this device, nil otherwise:
//   - code-less reject → nil (server-side transient, see isCodeLessReject);
//   - a coded metadata reject 401 / 402 / 403 (403 covers device class, ban
//     and brand mismatch — 403003 on the HTTP API arrives as 403 on the wire)
//     → that code, CategoryClient;
//   - anything else ClassifyError marks RequiresUserAction (e.g. a k2subs
//     fetch 401/402) → as classified.
func replaceLoopUserAction(err error) *EngineError {
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

package wire

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"time"
)

// metadataAuthResult is the single server-side admission decision for one
// metadata frame. Both transports consume it verbatim, so QUIC and TCP-WS
// cannot drift (spec §9 two-transport consistency).
type metadataAuthResult struct {
	authed    bool
	code      int    // 0 = none; 401/402/403 = Center explicit reject (spec §5.2)
	udid      string // server-determined identity; "" = unattributed bucket (spec §5.3)
	mode      string // server-determined mode
	sessionID string // hex(16B); "" when unauthenticated
	token     string // credential for periodic revalidation (wire/session.go)
	authAt    int64  // unix seconds of the Center endorsement backing this verdict
	svcExp    int64  // 0 = no subscription clock
	newCookie string // non-empty ONLY right after a Center cold auth (spec §6.3)
	cookieHit bool   // local cookie verification short-circuited the validator
}

// resolveMetadataAuth decides admission for one metadata frame.
//
// Cookie path: a valid cookie is local proof of a recent Center endorsement —
// the validator chain is NOT consulted and NO new cookie is produced. Local
// renewal is structurally impossible: authAt only ever moves through the cold
// path below, so "distance from last Center endorsement > cookieTTL" always
// forces a return to the validator (spec §6.3).
//
// Cold path: the validator chain runs. AuthVerdict.Code is FOUR-state (see
// wire/auth.go); after ChainValidators collapses no-opinions, this function
// sees pass / explicit-reject(401|402|403) / undecidable(-1). Only an
// explicit reject earns a structured code; undecidable follows the enforce
// flag exactly like the pre-cookie behavior (spec §6.4).
func resolveMetadataAuth(signer *CookieSigner, validate Validator, meta MetadataFrame, enforce bool, now time.Time) metadataAuthResult {
	// 1. Cookie short-circuit (spec §6: 本地验签直接放行，完全不进入 validator 链).
	if meta.Cookie != "" && signer != nil {
		if c, ok := signer.Verify(meta.Cookie, now); ok {
			return metadataAuthResult{
				authed:    true,
				udid:      c.UDID, // server-determined — never meta.UDID (spec §5.3)
				mode:      c.Mode,
				sessionID: hex.EncodeToString(c.SessionID[:]),
				token:     meta.Token,
				authAt:    c.AuthAt,
				svcExp:    c.SvcExp,
				cookieHit: true,
			}
		}
		// Invalid/expired cookie: silent fall-through to cold auth — the
		// credentials are already in the same frame, zero extra round trips.
	}

	// 2. Cold auth through the validator chain.
	if validate == nil {
		// No auth backend (k2p, tests): permissive-only, mirrors the
		// pre-cookie nil-validator behavior in both handlers.
		return metadataAuthResult{authed: !enforce, token: meta.Token}
	}
	v := validate(meta.Token, meta.UDID, meta.Mode)
	switch {
	case v.OK:
		var sid [16]byte
		_, _ = rand.Read(sid[:])
		res := metadataAuthResult{
			authed:    true,
			udid:      meta.UDID, // Center confirmed device.UDID == meta.UDID
			mode:      meta.Mode, // Center validated mode (spec §5.4)
			sessionID: hex.EncodeToString(sid[:]),
			token:     meta.Token,
			authAt:    now.Unix(),
			svcExp:    v.SvcExp,
		}
		if signer != nil {
			cookie, err := signer.Issue(Cookie{
				UDID:      meta.UDID,
				UserID:    v.UserID,
				Mode:      meta.Mode,
				SessionID: sid,
				AuthAt:    now.Unix(),
				SvcExp:    v.SvcExp,
			})
			if err != nil {
				slog.Warn("wire: cookie issue failed (serving without)", "err", err)
			} else {
				res.newCookie = cookie
			}
		}
		return res
	case v.Code == 401 || v.Code == 402 || v.Code == 403:
		return metadataAuthResult{authed: false, code: v.Code, token: meta.Token}
	default:
		// Undecidable (-1) or unclassified: permissive admits, enforce
		// rejects WITHOUT a structured code. Never conflate "Center didn't
		// answer" with "Center said no" (spec §6.4).
		return metadataAuthResult{authed: !enforce, token: meta.Token}
	}
}

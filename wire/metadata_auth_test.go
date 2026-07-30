package wire

import (
	"testing"
	"time"
)

// countingValidator returns a fixed verdict and counts invocations.
func countingValidator(v AuthVerdict, calls *int) Validator {
	return func(token, udid, mode string) AuthVerdict {
		*calls++
		return v
	}
}

func okVerdict() AuthVerdict {
	return AuthVerdict{OK: true, UserID: 7, SvcExp: time.Now().Add(24 * time.Hour).Unix()}
}

func TestMetadataAuth_ColdAuthIssuesCookie(t *testing.T) {
	signer := newTestSigner(t)
	calls := 0
	meta := MetadataFrame{UDID: "dev-1", Token: "tok", Mode: ""}
	st := resolveMetadataAuth(signer, countingValidator(okVerdict(), &calls), meta, true, time.Now())
	if !st.authed || calls != 1 {
		t.Fatalf("cold auth: authed=%v calls=%d", st.authed, calls)
	}
	if st.newCookie == "" {
		t.Fatal("cold auth did not issue a cookie")
	}
	if st.udid != "dev-1" || st.sessionID == "" || st.authAt == 0 {
		t.Fatalf("cold auth result incomplete: %+v", st)
	}
	c, ok := signer.Verify(st.newCookie, time.Now())
	if !ok || c.UDID != "dev-1" || c.UserID != 7 {
		t.Fatalf("issued cookie does not verify or carries wrong identity: %+v ok=%v", c, ok)
	}
}

func TestMetadataAuth_CookieHitSkipsValidator(t *testing.T) {
	signer := newTestSigner(t)
	calls := 0
	v := countingValidator(okVerdict(), &calls)
	now := time.Now()

	cold := resolveMetadataAuth(signer, v, MetadataFrame{UDID: "dev-1", Token: "tok"}, true, now)
	if calls != 1 {
		t.Fatalf("setup: cold auth calls=%d", calls)
	}

	warm := resolveMetadataAuth(signer, v,
		MetadataFrame{UDID: "dev-1", Token: "tok", Cookie: cold.newCookie}, true, now.Add(time.Minute))
	if !warm.authed || !warm.cookieHit {
		t.Fatalf("cookie hit not taken: %+v", warm)
	}
	if calls != 1 {
		t.Fatalf("VALIDATOR REACHED ON COOKIE HIT: calls=%d", calls)
	}
	if warm.sessionID != cold.sessionID {
		t.Fatal("session id not preserved across cookie hit")
	}
}

// THE Phase-2 core invariant (spec §6.3): presenting the same cookie any
// number of times never advances AuthAt and never yields a new cookie; once
// 15 minutes have passed since the Center endorsement, the validator MUST be
// consulted again.
func TestMetadataAuth_NoLocalRenewal(t *testing.T) {
	signer := newTestSigner(t)
	calls := 0
	v := countingValidator(okVerdict(), &calls)
	now := time.Now()

	cold := resolveMetadataAuth(signer, v, MetadataFrame{UDID: "dev-1", Token: "tok"}, true, now)

	meta := MetadataFrame{UDID: "dev-1", Token: "tok", Cookie: cold.newCookie}
	for i := 0; i < 100; i++ {
		st := resolveMetadataAuth(signer, v, meta, true, now.Add(time.Duration(i)*8*time.Second)) // up to ~13.3min
		if !st.authed || !st.cookieHit {
			t.Fatalf("iteration %d: cookie unexpectedly rejected: %+v", i, st)
		}
		if st.newCookie != "" {
			t.Fatalf("RENEWAL LEAK at iteration %d: cookie hit produced a new cookie", i)
		}
		if st.authAt != cold.authAt {
			t.Fatalf("RENEWAL LEAK at iteration %d: AuthAt advanced %d → %d", i, cold.authAt, st.authAt)
		}
	}
	if calls != 1 {
		t.Fatalf("validator called %d times during warm presentations, want 1", calls)
	}

	// Past the window: MUST fall back to the validator.
	st := resolveMetadataAuth(signer, v, meta, true, now.Add(cookieTTL+time.Second))
	if st.cookieHit {
		t.Fatal("expired cookie still short-circuited")
	}
	if calls != 2 {
		t.Fatalf("validator not consulted after TTL: calls=%d", calls)
	}
	if st.newCookie == "" || st.authAt == cold.authAt {
		t.Fatal("post-TTL cold auth did not mint a fresh endorsement")
	}
}

// udid收口 (spec §5.3): on a cookie hit the attributed udid comes from the
// cookie, never from the client-reported meta.UDID.
func TestMetadataAuth_UDIDFromCookieNotClient(t *testing.T) {
	signer := newTestSigner(t)
	calls := 0
	v := countingValidator(okVerdict(), &calls)
	now := time.Now()
	cold := resolveMetadataAuth(signer, v, MetadataFrame{UDID: "real-dev", Token: "tok"}, true, now)

	st := resolveMetadataAuth(signer, v,
		MetadataFrame{UDID: "spoofed-dev", Token: "tok", Cookie: cold.newCookie}, true, now.Add(time.Minute))
	if st.udid != "real-dev" {
		t.Fatalf("ATTRIBUTION SPOOF: udid=%q, want cookie's real-dev", st.udid)
	}
}

func TestMetadataAuth_ExplicitRejectCode(t *testing.T) {
	signer := newTestSigner(t)
	for _, code := range []int{401, 402, 403} {
		calls := 0
		st := resolveMetadataAuth(signer,
			countingValidator(AuthVerdict{OK: false, Code: code}, &calls),
			MetadataFrame{UDID: "dev-1", Token: "bad"}, true, time.Now())
		if st.authed || st.code != code {
			t.Fatalf("code %d: authed=%v code=%d", code, st.authed, st.code)
		}
		if st.udid != "" {
			t.Fatalf("rejected connection attributed to %q, want empty bucket", st.udid)
		}
	}
}

// Undecidable (-1) at COLD auth follows enforce (spec §6.4 row 3): permissive
// admits, enforce rejects — and it is never given a structured 4xx code
// ("Center didn't answer" must be distinguishable from "Center said no").
func TestMetadataAuth_UndecidableColdAuthFollowsEnforce(t *testing.T) {
	signer := newTestSigner(t)
	und := AuthVerdict{OK: false, Code: -1}
	calls := 0
	if st := resolveMetadataAuth(signer, countingValidator(und, &calls),
		MetadataFrame{UDID: "d", Token: "t"}, false, time.Now()); !st.authed {
		t.Fatal("permissive + undecidable must admit")
	}
	if st := resolveMetadataAuth(signer, countingValidator(und, &calls),
		MetadataFrame{UDID: "d", Token: "t"}, true, time.Now()); st.authed {
		t.Fatal("enforce + undecidable must reject")
	} else if st.code == 401 || st.code == 402 || st.code == 403 {
		t.Fatalf("undecidable mislabeled as explicit reject: code=%d", st.code)
	}
	if st := resolveMetadataAuth(signer, countingValidator(und, &calls),
		MetadataFrame{UDID: "d", Token: "t"}, false, time.Now()); st.newCookie != "" {
		t.Fatal("undecidable produced a cookie — only a Center OK may")
	}
}

// A node without cert_dir (legacy TLS mode) has no signer: cookies in the
// frame are ignored, cold auth still works, no cookie is issued.
func TestMetadataAuth_NilSignerColdOnly(t *testing.T) {
	calls := 0
	st := resolveMetadataAuth(nil, countingValidator(okVerdict(), &calls),
		MetadataFrame{UDID: "dev-1", Token: "tok", Cookie: "garbage"}, true, time.Now())
	if !st.authed || calls != 1 || st.newCookie != "" {
		t.Fatalf("nil signer: authed=%v calls=%d newCookie=%q", st.authed, calls, st.newCookie)
	}
}

func TestMetadataAuth_NilValidatorMirrorsEnforce(t *testing.T) {
	if st := resolveMetadataAuth(nil, nil, MetadataFrame{UDID: "d", Token: "t"}, false, time.Now()); !st.authed {
		t.Fatal("nil validator + permissive must admit (k2p/tests)")
	}
	if st := resolveMetadataAuth(nil, nil, MetadataFrame{UDID: "d", Token: "t"}, true, time.Now()); st.authed {
		t.Fatal("nil validator + enforce must reject (misconfigured node)")
	}
}

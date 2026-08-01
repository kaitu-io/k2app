package wire

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testCookie() Cookie {
	return Cookie{
		UDID:      "device-abc",
		UserID:    42,
		Mode:      "gateway",
		SessionID: [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		AuthAt:    time.Now().Unix(),
		SvcExp:    time.Now().Add(48 * time.Hour).Unix(),
	}
}

func newTestSigner(t *testing.T) *CookieSigner {
	t.Helper()
	s, err := NewCookieSigner(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestCookie_RoundTrip(t *testing.T) {
	s := newTestSigner(t)
	in := testCookie()
	raw, err := s.Issue(in)
	if err != nil {
		t.Fatal(err)
	}
	out, ok := s.Verify(raw, time.Now())
	if !ok {
		t.Fatal("fresh cookie failed verification")
	}
	if out.UDID != in.UDID || out.UserID != in.UserID || out.Mode != in.Mode ||
		out.SessionID != in.SessionID || out.AuthAt != in.AuthAt || out.SvcExp != in.SvcExp {
		t.Fatalf("round trip mismatch: in=%+v out=%+v", in, out)
	}
	if out.Ver != 1 || out.KeyID == 0 {
		t.Fatalf("Ver/KeyID not stamped: %+v", out)
	}
}

func TestCookie_ModeEmptyRoundTrip(t *testing.T) {
	s := newTestSigner(t)
	in := testCookie()
	in.Mode = ""
	raw, err := s.Issue(in)
	if err != nil {
		t.Fatal(err)
	}
	out, ok := s.Verify(raw, time.Now())
	if !ok || out.Mode != "" {
		t.Fatalf("empty mode round trip failed: ok=%v mode=%q", ok, out.Mode)
	}
}

func TestCookieVerify_TamperRejected(t *testing.T) {
	s := newTestSigner(t)
	raw, err := s.Issue(testCookie())
	if err != nil {
		t.Fatal(err)
	}
	data, _ := base64.RawURLEncoding.DecodeString(raw)
	// Flip one bit in the udid region of the payload (byte 44 is udid[0]).
	data[44] ^= 0x01
	tampered := base64.RawURLEncoding.EncodeToString(data)
	if _, ok := s.Verify(tampered, time.Now()); ok {
		t.Fatal("SECURITY: tampered cookie accepted")
	}
	// Also: garbage and truncated inputs must fail, not panic.
	for _, bad := range []string{"", "!!!!", base64.RawURLEncoding.EncodeToString([]byte("short"))} {
		if _, ok := s.Verify(bad, time.Now()); ok {
			t.Fatalf("malformed cookie %q accepted", bad)
		}
	}
}

func TestCookieVerify_ExpiredRejected(t *testing.T) {
	s := newTestSigner(t)
	c := testCookie()
	raw, err := s.Issue(c)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Unix(c.AuthAt, 0)
	if _, ok := s.Verify(raw, at.Add(cookieTTL-time.Second)); !ok {
		t.Fatal("cookie inside TTL rejected")
	}
	if _, ok := s.Verify(raw, at.Add(cookieTTL)); ok {
		t.Fatal("cookie at/after AuthAt+cookieTTL accepted")
	}
}

// exp = min(AuthAt+15min, SvcExp): a subscription expiring in 3 minutes caps
// the cookie at 3 minutes, to the second (spec §6.3).
func TestCookieVerify_SvcExpTruncates(t *testing.T) {
	s := newTestSigner(t)
	c := testCookie()
	c.SvcExp = c.AuthAt + 180 // 3 min after endorsement
	raw, err := s.Issue(c)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Unix(c.AuthAt, 0)
	if _, ok := s.Verify(raw, at.Add(179*time.Second)); !ok {
		t.Fatal("cookie inside SvcExp rejected")
	}
	if _, ok := s.Verify(raw, at.Add(180*time.Second)); ok {
		t.Fatal("cookie past SvcExp accepted — subscription expiry not enforced")
	}
}

// SvcExp == 0 (users_file branch, no subscription clock) must NOT participate
// in the min — otherwise min(x, 0) would make every standalone-node cookie
// dead on arrival.
func TestCookieVerify_SvcExpZeroUsersFileBranch(t *testing.T) {
	s := newTestSigner(t)
	c := testCookie()
	c.SvcExp = 0
	raw, err := s.Issue(c)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Unix(c.AuthAt, 0)
	if _, ok := s.Verify(raw, at.Add(cookieTTL-time.Second)); !ok {
		t.Fatal("SvcExp==0 cookie rejected inside TTL — 0 leaked into the min")
	}
	if _, ok := s.Verify(raw, at.Add(cookieTTL)); ok {
		t.Fatal("SvcExp==0 cookie accepted past TTL")
	}
}

// Node restart must not stampede Center: a new CookieSigner over the same dir
// loads the same key, so cookies issued before the restart still verify.
func TestCookieSigner_RestartReloadsSameKey(t *testing.T) {
	dir := t.TempDir()
	s1, err := NewCookieSigner(dir)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := s1.Issue(testCookie())
	if err != nil {
		t.Fatal(err)
	}
	s2, err := NewCookieSigner(dir) // simulated restart
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s2.Verify(raw, time.Now()); !ok {
		t.Fatal("THUNDERING HERD: cookie invalid after signer restart — key not persisted/reloaded")
	}
}

func TestCookieSigner_KeyFilePermissions0600(t *testing.T) {
	dir := t.TempDir()
	if _, err := NewCookieSigner(dir); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "cookie.key"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("cookie.key mode = %o, want 0600", info.Mode().Perm())
	}
}

// After the 30-day period elapses, the next Issue rotates the key. Cookies
// signed by the previous key keep verifying for the 30-minute overlap, then
// are rejected.
func TestCookieSigner_AutoRotateWithOverlap(t *testing.T) {
	dir := t.TempDir()
	s, err := NewCookieSigner(dir)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now()
	s.now = func() time.Time { return base }

	// Every cookie's AuthAt is pinned to the scenario clock (s.now), never the
	// real wall clock, and SvcExp=0 (users_file form) keeps the subscription
	// clock out of exp=min(AuthAt+15min, SvcExp). Otherwise testCookie()'s
	// real-time AuthAt/SvcExp would put the payload's 15-minute TTL decades in
	// the past the moment s.now jumps 30 days ahead — and Verify would correctly
	// return false, turning this test red against a CORRECT implementation.
	freshAt := func(at time.Time) Cookie {
		c := testCookie()
		c.AuthAt = at.Unix()
		c.SvcExp = 0
		return c
	}

	oldRaw, err := s.Issue(freshAt(base))
	if err != nil {
		t.Fatal(err)
	}
	oldC, ok := s.Verify(oldRaw, base)
	if !ok {
		t.Fatal("fresh original-key cookie failed verification")
	}

	// Issue a cookie just before the rotation period elapses — still the
	// original key (period not yet due, so this Issue must NOT rotate).
	preAt := base.Add(cookieKeyRotatePeriod - time.Minute)
	s.now = func() time.Time { return preAt }
	preRaw, err := s.Issue(freshAt(preAt))
	if err != nil {
		t.Fatal(err)
	}

	// Cross the rotation boundary; the next Issue must rotate the key.
	rotatedAt := base.Add(cookieKeyRotatePeriod + time.Minute)
	s.now = func() time.Time { return rotatedAt }
	newRaw, err := s.Issue(freshAt(rotatedAt))
	if err != nil {
		t.Fatal(err)
	}
	newC, ok := s.Verify(newRaw, rotatedAt)
	if !ok {
		t.Fatal("cookie signed by rotated key failed verification")
	}
	if newC.KeyID == oldC.KeyID {
		t.Fatalf("KeyID did not increment on rotation: %d", newC.KeyID)
	}

	// The pre-rotation cookie (AuthAt = base+30d-1min → payload TTL to
	// base+30d+14min) verified 2 min after rotation: inside both its payload
	// TTL and the 30-minute prev-key overlap → must still verify on the prev key.
	if _, ok := s.Verify(preRaw, rotatedAt.Add(time.Minute)); !ok {
		t.Fatal("old-key cookie rejected inside overlap+TTL window")
	}
	// Well past the overlap window → rejected. NOTE: because cookieKeyOverlap
	// (30min) exceeds cookieTTL (15min), a prev-key cookie past its overlap is
	// necessarily past its payload TTL too, so this assertion holds regardless
	// of which gate fires (see Step 6 mutation 5 for the consequence).
	if _, ok := s.Verify(preRaw, rotatedAt.Add(cookieKeyOverlap+time.Minute)); ok {
		t.Fatal("old-key cookie accepted after overlap window")
	}
}

// persist() must never corrupt the previously-committed cookie.key: if the
// write-to-temp step can't even create a file (directory made read-only,
// simulating a failed rotation write), the ORIGINAL file must be left
// completely untouched — still the exact bytes it had before, still loading
// and verifying successfully. This is the property the plain-os.WriteFile
// implementation lacked: a crash between truncate and write there would leave
// cookie.key corrupt (0 bytes or partial JSON), and NewCookieSigner's load
// path hard-fails on that, refusing to boot on every restart until an
// operator deletes the file by hand.
func TestCookieSigner_PersistFailureLeavesOriginalIntact(t *testing.T) {
	dir := t.TempDir()
	s, err := NewCookieSigner(dir)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := s.Issue(testCookie())
	if err != nil {
		t.Fatal(err)
	}

	keyPath := filepath.Join(dir, cookieKeyFileName)
	before, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}

	// Make the directory read-only: the temp file persist() tries to create
	// (same dir, required for atomic os.Rename) cannot even be opened. This
	// must fail cleanly with NO write to the original path.
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) }) // let t.TempDir() clean up

	if err := s.persist(); err == nil {
		t.Fatal("persist() succeeded despite read-only directory — test setup broken")
	}

	// Restore write access to read the directory listing and to let the
	// eventual TempDir cleanup remove it.
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != cookieKeyFileName {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Fatalf("LEAKED TEMP FILE: dir contents = %v, want only %q", names, cookieKeyFileName)
	}

	after, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("CORRUPTED: cookie.key changed after failed persist():\nbefore=%s\nafter=%s", before, after)
	}

	// The original cookie, signed before the failed persist(), must still
	// verify — proving the original key material survived intact.
	s2, err := NewCookieSigner(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s2.Verify(raw, time.Now()); !ok {
		t.Fatal("cookie signed before failed persist() no longer verifies — original key corrupted")
	}
}

// Rotation must survive a restart: the rotated file carries prev key too.
func TestCookieSigner_RotationPersisted(t *testing.T) {
	dir := t.TempDir()
	s, err := NewCookieSigner(dir)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now()
	s.now = func() time.Time { return base.Add(cookieKeyRotatePeriod + time.Minute) }
	raw, err := s.Issue(testCookie()) // triggers rotation, then signs with new key
	if err != nil {
		t.Fatal(err)
	}
	s2, err := NewCookieSigner(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s2.Verify(raw, time.Now()); !ok {
		t.Fatal("post-rotation cookie invalid after restart — rotated state not persisted")
	}
}

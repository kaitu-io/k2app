package center

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wordgate/qtoolkit/redis"
)

// withRealVerificationCode flips EnableMockVerificationCode off for the
// duration of a test so we exercise the real Redis-backed code path
// (testInitConfig() enables mock by default).
func withRealVerificationCode(t *testing.T) {
	t.Helper()
	testInitConfig()
	prev := EnableMockVerificationCode
	EnableMockVerificationCode = false
	t.Cleanup(func() { EnableMockVerificationCode = prev })
}

func TestIssueOrRefreshVerificationCode_FirstCallGeneratesAndPersists(t *testing.T) {
	withRealVerificationCode(t)
	ctx := context.Background()
	hash := "test-hash-first-call"
	// ensure clean slate
	_ = redis.CacheDel(verificationCodeKey(hash))

	code, err := issueOrRefreshVerificationCode(ctx, hash)
	require.NoError(t, err)
	assert.Len(t, code, 6, "code should be 6 digits")

	// Persisted under unified key
	var persisted string
	exist, err := redis.CacheGet(verificationCodeKey(hash), &persisted)
	require.NoError(t, err)
	assert.True(t, exist)
	assert.Equal(t, code, persisted)
}

func TestIssueOrRefreshVerificationCode_ReusesAndRefreshesTTL(t *testing.T) {
	withRealVerificationCode(t)
	ctx := context.Background()
	hash := "test-hash-reuse"
	_ = redis.CacheDel(verificationCodeKey(hash))

	first, err := issueOrRefreshVerificationCode(ctx, hash)
	require.NoError(t, err)

	// Simulate ~half of TTL elapsing
	testMiniRedis.FastForward(time.Duration(VerificationCodeExpiry/2) * time.Second)

	second, err := issueOrRefreshVerificationCode(ctx, hash)
	require.NoError(t, err)

	// Same code returned (reuse semantics)
	assert.Equal(t, first, second, "second call must reuse the existing code")

	// TTL should have been refreshed close to full window again. We assert
	// it's > 0.6 * expiry — the half we fast-forwarded should NOT still be
	// burning down.
	ttl := testMiniRedis.TTL(verificationCodeKey(hash))
	assert.Greater(t, ttl, time.Duration(VerificationCodeExpiry)*time.Second*6/10,
		"TTL must be refreshed by a resend")
}

func TestVerifyEmailCode_Correct(t *testing.T) {
	withRealVerificationCode(t)
	ctx := context.Background()
	hash := "test-hash-verify-ok"
	_ = redis.CacheDel(verificationCodeKey(hash))

	code, err := issueOrRefreshVerificationCode(ctx, hash)
	require.NoError(t, err)

	assert.Equal(t, VerifyCodeOK, verifyEmailCode(ctx, hash, code))
}

func TestVerifyEmailCode_WrongValueReportsWrongNotMissing(t *testing.T) {
	withRealVerificationCode(t)
	ctx := context.Background()
	hash := "test-hash-verify-wrong"
	_ = redis.CacheDel(verificationCodeKey(hash))

	_, err := issueOrRefreshVerificationCode(ctx, hash)
	require.NoError(t, err)

	// Front-end relies on this distinction: wrong-value lets us say
	// "请检查验证码" instead of "请重发"
	assert.Equal(t, VerifyCodeWrong, verifyEmailCode(ctx, hash, "000000"))
}

func TestVerifyEmailCode_NoCodeReportsNotIssued(t *testing.T) {
	withRealVerificationCode(t)
	ctx := context.Background()
	hash := "test-hash-never-issued"
	_ = redis.CacheDel(verificationCodeKey(hash))

	assert.Equal(t, VerifyCodeNotIssued, verifyEmailCode(ctx, hash, "123456"))
}

func TestMarkVerificationCodeUsed_KeepsCodeButShrinksTTLToGrace(t *testing.T) {
	withRealVerificationCode(t)
	ctx := context.Background()
	hash := "test-hash-mark-used"
	_ = redis.CacheDel(verificationCodeKey(hash))

	code, err := issueOrRefreshVerificationCode(ctx, hash)
	require.NoError(t, err)

	require.NoError(t, markVerificationCodeUsed(ctx, hash))

	// Code is still verifiable within grace window (this is the whole point —
	// double-click / network retry idempotency).
	assert.Equal(t, VerifyCodeOK, verifyEmailCode(ctx, hash, code))

	// TTL has been shrunk to <= grace period.
	ttl := testMiniRedis.TTL(verificationCodeKey(hash))
	assert.LessOrEqual(t, ttl, time.Duration(VerificationCodeUsedGracePeriod)*time.Second)
}

func TestIssueOrRefreshDuringGrace_IssuesFreshCodeAndInvalidatesOld(t *testing.T) {
	// Invariant: the 60s grace ceiling is enforced by code, not just by the
	// natural TTL. A resend while a code is in its used-grace window must
	// produce a fresh code and immediately invalidate the old one.
	withRealVerificationCode(t)
	ctx := context.Background()
	hash := "test-hash-resend-during-grace"
	_ = redis.CacheDel(verificationCodeKey(hash))

	oldCode, err := issueOrRefreshVerificationCode(ctx, hash)
	require.NoError(t, err)
	require.NoError(t, markVerificationCodeUsed(ctx, hash))

	// Resend while in grace period
	newCode, err := issueOrRefreshVerificationCode(ctx, hash)
	require.NoError(t, err)

	assert.NotEqual(t, oldCode, newCode, "resend during grace must mint a fresh code")

	// Old code is now invalid (the user/attacker who only has oldCode loses access)
	assert.Equal(t, VerifyCodeWrong, verifyEmailCode(ctx, hash, oldCode),
		"old code must not validate after a grace-period resend")

	// New code works as usual
	assert.Equal(t, VerifyCodeOK, verifyEmailCode(ctx, hash, newCode))

	// Fresh code got full TTL (not the leftover grace TTL)
	ttl := testMiniRedis.TTL(verificationCodeKey(hash))
	assert.Greater(t, ttl, time.Duration(VerificationCodeUsedGracePeriod*5)*time.Second,
		"resend during grace must reset TTL to the full window")
}

func TestVerifyEmailCode_DuringGrace_WrongCodeStillReportsWrong(t *testing.T) {
	// Even in the grace period, a wrong submitted code must surface as Wrong
	// (not OK, not NotIssued). The grace path must not "swallow" mismatches.
	withRealVerificationCode(t)
	ctx := context.Background()
	hash := "test-hash-grace-wrong"
	_ = redis.CacheDel(verificationCodeKey(hash))

	_, err := issueOrRefreshVerificationCode(ctx, hash)
	require.NoError(t, err)
	require.NoError(t, markVerificationCodeUsed(ctx, hash))

	assert.Equal(t, VerifyCodeWrong, verifyEmailCode(ctx, hash, "000000"))
}

func TestMarkVerificationCodeUsed_TwiceIsIdempotent(t *testing.T) {
	// Concurrent successful verifies could both call markUsed. The second
	// must not corrupt the stored value (e.g., "used:used:957727") nor
	// flip the code back to active state.
	withRealVerificationCode(t)
	ctx := context.Background()
	hash := "test-hash-mark-twice"
	_ = redis.CacheDel(verificationCodeKey(hash))

	code, err := issueOrRefreshVerificationCode(ctx, hash)
	require.NoError(t, err)
	require.NoError(t, markVerificationCodeUsed(ctx, hash))
	require.NoError(t, markVerificationCodeUsed(ctx, hash))

	// The original code is still verifiable (grace).
	assert.Equal(t, VerifyCodeOK, verifyEmailCode(ctx, hash, code))

	// Stored value must NOT be "used:used:<code>" — that would break verify.
	var raw string
	exist, err := redis.CacheGet(verificationCodeKey(hash), &raw)
	require.NoError(t, err)
	require.True(t, exist)
	assert.Equal(t, "used:"+code, raw, "double-mark must not double-prefix")
}

func TestMarkVerificationCodeUsed_NoKeyIsNoOp(t *testing.T) {
	withRealVerificationCode(t)
	ctx := context.Background()
	hash := "test-hash-mark-nokey"
	_ = redis.CacheDel(verificationCodeKey(hash))

	// Should not error even though no code exists. Models the race where
	// a concurrent request already consumed the code.
	assert.NoError(t, markVerificationCodeUsed(ctx, hash))
}

func TestVerifyEmailCode_AfterGracePeriod_ReportsNotIssued(t *testing.T) {
	withRealVerificationCode(t)
	ctx := context.Background()
	hash := "test-hash-grace-expires"
	_ = redis.CacheDel(verificationCodeKey(hash))

	code, err := issueOrRefreshVerificationCode(ctx, hash)
	require.NoError(t, err)
	require.NoError(t, markVerificationCodeUsed(ctx, hash))

	// Fast-forward past the grace period
	testMiniRedis.FastForward(time.Duration(VerificationCodeUsedGracePeriod+1) * time.Second)

	// Now reused-code attempts must surface as "expired", not "wrong"
	assert.Equal(t, VerifyCodeNotIssued, verifyEmailCode(ctx, hash, code))
}

func TestVerifyEmailCode_AfterFullTTL_ReportsNotIssued(t *testing.T) {
	withRealVerificationCode(t)
	ctx := context.Background()
	hash := "test-hash-ttl-expires"
	_ = redis.CacheDel(verificationCodeKey(hash))

	code, err := issueOrRefreshVerificationCode(ctx, hash)
	require.NoError(t, err)

	// Skip past full TTL without verifying — the historical bug scenario
	// (user took >5min and saw "invalid code"). Now the result must be
	// the more accurate NotIssued so the UI can prompt resend.
	testMiniRedis.FastForward(time.Duration(VerificationCodeExpiry+1) * time.Second)

	assert.Equal(t, VerifyCodeNotIssued, verifyEmailCode(ctx, hash, code))
}

func TestIssueOrRefreshVerificationCode_DifferentEmailsGetDifferentCodes(t *testing.T) {
	withRealVerificationCode(t)
	ctx := context.Background()
	_ = redis.CacheDel(verificationCodeKey("hash-a"))
	_ = redis.CacheDel(verificationCodeKey("hash-b"))

	codeA, err := issueOrRefreshVerificationCode(ctx, "hash-a")
	require.NoError(t, err)
	codeB, err := issueOrRefreshVerificationCode(ctx, "hash-b")
	require.NoError(t, err)

	// Verifying A's code against B's hash must NOT pass — codes are
	// per-email, not global.
	assert.Equal(t, VerifyCodeWrong, verifyEmailCode(ctx, "hash-b", codeA),
		"code from one email must not validate against another email")
	_ = codeB
}

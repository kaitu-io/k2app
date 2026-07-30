package wire

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCachingValidator_CachesSuccess(t *testing.T) {
	var calls atomic.Int32
	inner := func(token, udid, mode string) AuthVerdict {
		calls.Add(1)
		return AuthVerdict{OK: true}
	}
	now := time.Unix(1000, 0)
	c := &authCache{inner: inner, ttl: time.Minute, now: func() time.Time { return now }, entries: map[string]cachedVerdict{}}

	if !c.validate("tok", "dev", "").OK {
		t.Fatal("first call should pass")
	}
	if !c.validate("tok", "dev", "").OK {
		t.Fatal("second call should pass (cached)")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("inner called %d times, want 1 (second served from cache)", got)
	}
}

func TestCachingValidator_ExpiresAfterTTL(t *testing.T) {
	var calls atomic.Int32
	inner := func(token, udid, mode string) AuthVerdict {
		calls.Add(1)
		return AuthVerdict{OK: true}
	}
	now := time.Unix(1000, 0)
	c := &authCache{inner: inner, ttl: time.Minute, now: func() time.Time { return now }, entries: map[string]cachedVerdict{}}

	c.validate("tok", "dev", "")
	now = now.Add(2 * time.Minute) // past TTL
	c.validate("tok", "dev", "")
	if got := calls.Load(); got != 2 {
		t.Errorf("inner called %d times, want 2 (cache expired)", got)
	}
}

func TestCachingValidator_DoesNotCacheFailure(t *testing.T) {
	var calls atomic.Int32
	inner := func(token, udid, mode string) AuthVerdict {
		calls.Add(1)
		return AuthVerdict{Code: 401}
	}
	now := time.Unix(1000, 0)
	c := &authCache{inner: inner, ttl: time.Minute, now: func() time.Time { return now }, entries: map[string]cachedVerdict{}}

	c.validate("tok", "dev", "")
	c.validate("tok", "dev", "")
	if got := calls.Load(); got != 2 {
		t.Errorf("inner called %d times, want 2 (failures not cached — a renewed user must not be locked out)", got)
	}
}

func TestCachingValidator_KeyIsolation(t *testing.T) {
	var calls atomic.Int32
	inner := func(token, udid, mode string) AuthVerdict {
		calls.Add(1)
		return AuthVerdict{OK: true}
	}
	now := time.Unix(1000, 0)
	c := &authCache{inner: inner, ttl: time.Minute, now: func() time.Time { return now }, entries: map[string]cachedVerdict{}}

	c.validate("tok", "devA", "")
	c.validate("tok", "devB", "")  // different udid → not a cache hit
	c.validate("tokX", "devA", "") // different token → not a cache hit
	if got := calls.Load(); got != 3 {
		t.Errorf("inner called %d times, want 3 (distinct keys must not share cache)", got)
	}
}

// TestCachingValidator_NeverCachesUnreachable: caching a -1 would freeze a
// transient Center blip into a TTL-long outage for that device.
func TestCachingValidator_NeverCachesUnreachable(t *testing.T) {
	calls := 0
	inner := func(token, udid, mode string) AuthVerdict { calls++; return AuthVerdict{Code: -1} }
	v := CachingValidator(inner, time.Minute)
	v("t", "d", "")
	v("t", "d", "")
	if calls != 2 {
		t.Fatalf("unreachable (-1) must never be cached: inner called %d times, want 2", calls)
	}
}

// TestCachingValidator_NeverCachesExplicitRejection: a user who just renewed
// must not stay locked out for the TTL.
func TestCachingValidator_NeverCachesExplicitRejection(t *testing.T) {
	calls := 0
	inner := func(token, udid, mode string) AuthVerdict { calls++; return AuthVerdict{Code: 402} }
	v := CachingValidator(inner, time.Minute)
	v("t", "d", "")
	v("t", "d", "")
	if calls != 2 {
		t.Fatalf("explicit rejection must never be cached: inner called %d times, want 2", calls)
	}
}

// TestCachingValidator_SingleflightCollapsesConcurrentMisses: the transport
// race dials up to three candidates near-simultaneously; the 300ms/800ms
// stagger makes concurrent cold misses unlikely, not impossible. All
// concurrent misses for one key must collapse into one Center call.
func TestCachingValidator_SingleflightCollapsesConcurrentMisses(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	inner := func(token, udid, mode string) AuthVerdict {
		calls.Add(1)
		<-release
		return AuthVerdict{OK: true}
	}
	v := CachingValidator(inner, time.Minute)

	var wg sync.WaitGroup
	results := make([]AuthVerdict, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) { defer wg.Done(); results[i] = v("t", "d", "") }(i)
	}
	time.Sleep(100 * time.Millisecond) // let all goroutines pile up on the key
	close(release)
	wg.Wait()

	if got := calls.Load(); got != 1 {
		t.Fatalf("concurrent misses must collapse into one inner call, got %d", got)
	}
	for i, r := range results {
		if !r.OK {
			t.Fatalf("caller %d did not receive the shared OK verdict: %+v", i, r)
		}
	}
}

// TestCachingValidator_CachedVerdictKeepsFields: a hit must return the full
// verdict (UserID/SvcExp feed Phase 2 cookie issuance), not just OK.
func TestCachingValidator_CachedVerdictKeepsFields(t *testing.T) {
	inner := func(token, udid, mode string) AuthVerdict {
		return AuthVerdict{OK: true, UserID: 42, SvcExp: 1999999999}
	}
	v := CachingValidator(inner, time.Minute)
	v("t", "d", "")
	got := v("t", "d", "") // cache hit
	if got.UserID != 42 || got.SvcExp != 1999999999 {
		t.Fatalf("cache hit dropped fields: %+v", got)
	}
}

package wire

import (
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

package provider

import (
	"net/netip"
	"testing"
	"time"
)

func TestCacheKey_Format(t *testing.T) {
	got := cacheKey("tcp", netip.MustParseAddr("192.168.1.5"), 54321)
	want := "tcp:192.168.1.5:54321"
	if got != want {
		t.Errorf("cacheKey = %q, want %q", got, want)
	}
}

func TestProcessCache_HitAfterSet(t *testing.T) {
	c := newProcessCache(16, time.Second)
	c.set("k", "chrome", "")
	name, pkg, hit := c.get("k")
	if !hit || name != "chrome" || pkg != "" {
		t.Errorf("get = (%q,%q,%v), want (chrome,,true)", name, pkg, hit)
	}
}

func TestProcessCache_MissOnEmpty(t *testing.T) {
	c := newProcessCache(16, time.Second)
	_, _, hit := c.get("nope")
	if hit {
		t.Errorf("expected miss on empty cache")
	}
}

func TestProcessCache_NegativeEntryStored(t *testing.T) {
	c := newProcessCache(16, time.Second)
	c.set("k", "", "") // negative cache
	name, pkg, hit := c.get("k")
	if !hit || name != "" || pkg != "" {
		t.Errorf("get = (%q,%q,%v), want (,,true)", name, pkg, hit)
	}
}

func TestProcessCache_TTLExpiry(t *testing.T) {
	c := newProcessCache(16, 10*time.Millisecond)
	c.set("k", "chrome", "")
	time.Sleep(20 * time.Millisecond)
	_, _, hit := c.get("k")
	if hit {
		t.Errorf("expected expiry after TTL")
	}
}

func TestProcessCache_SoftCapEvictsExpired(t *testing.T) {
	c := newProcessCache(2, 10*time.Millisecond)
	c.set("k1", "a", "")
	c.set("k2", "b", "")
	time.Sleep(20 * time.Millisecond) // both expire
	c.set("k3", "c", "")              // len >= cap triggers sweep
	// After sweep, k1/k2 (expired) removed; k3 added.
	if _, _, hit := c.get("k1"); hit {
		t.Errorf("k1 should have been evicted")
	}
	if _, _, hit := c.get("k3"); !hit {
		t.Errorf("k3 should be present")
	}
}

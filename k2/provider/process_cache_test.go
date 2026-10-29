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
	c.set("k", "chrome", "", "")
	name, pkg, installer, hit := c.get("k")
	if !hit || name != "chrome" || pkg != "" || installer != "" {
		t.Errorf("get = (%q,%q,%q,%v), want (chrome,,,true)", name, pkg, installer, hit)
	}
}

func TestProcessCache_MissOnEmpty(t *testing.T) {
	c := newProcessCache(16, time.Second)
	_, _, _, hit := c.get("nope")
	if hit {
		t.Errorf("expected miss on empty cache")
	}
}

func TestProcessCache_NegativeEntryStored(t *testing.T) {
	c := newProcessCache(16, time.Second)
	c.set("k", "", "", "") // negative cache
	name, pkg, installer, hit := c.get("k")
	if !hit || name != "" || pkg != "" || installer != "" {
		t.Errorf("get = (%q,%q,%q,%v), want (,,,true)", name, pkg, installer, hit)
	}
}

func TestProcessCache_TTLExpiry(t *testing.T) {
	c := newProcessCache(16, 10*time.Millisecond)
	c.set("k", "chrome", "", "")
	time.Sleep(20 * time.Millisecond)
	_, _, _, hit := c.get("k")
	if hit {
		t.Errorf("expected expiry after TTL")
	}
}

func TestProcessCache_SoftCapEvictsExpired(t *testing.T) {
	c := newProcessCache(2, 10*time.Millisecond)
	c.set("k1", "a", "", "")
	c.set("k2", "b", "", "")
	time.Sleep(20 * time.Millisecond) // both expire
	c.set("k3", "c", "", "")          // len >= cap triggers sweep
	// After sweep, k1/k2 (expired) removed; k3 added.
	if _, _, _, hit := c.get("k1"); hit {
		t.Errorf("k1 should have been evicted")
	}
	if _, _, _, hit := c.get("k3"); !hit {
		t.Errorf("k3 should be present")
	}
}

func TestProcessCache_InstallerPersisted(t *testing.T) {
	c := newProcessCache(16, time.Second)
	c.set("k", "wechat", "com.tencent.mm", "com.xiaomi.market")
	name, pkg, installer, hit := c.get("k")
	if !hit || name != "wechat" || pkg != "com.tencent.mm" || installer != "com.xiaomi.market" {
		t.Errorf("get = (%q,%q,%q,%v), want (wechat,com.tencent.mm,com.xiaomi.market,true)",
			name, pkg, installer, hit)
	}
}

func BenchmarkProcessCache_HotPath(b *testing.B) {
	c := newProcessCache(processCacheCap, processCacheTTL)
	addr := netip.MustParseAddr("1.2.3.4")
	key := cacheKey("tcp", addr, 5678)
	c.set(key, "chrome", "", "")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Mimic FindProcess hot path: key construction + map lookup.
		k := cacheKey("tcp", addr, 5678)
		c.get(k)
	}
	b.ReportAllocs()
}

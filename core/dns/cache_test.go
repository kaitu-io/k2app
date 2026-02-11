package dns

import (
	"sync"
	"testing"
	"time"

	"github.com/kaitu-io/k2rule"
)

func TestDynamicIPCache_AddAndGet(t *testing.T) {
	cache := NewDynamicIPCache()

	// Add rule
	rule := IPRule{
		Domain:   "example.com",
		Action:   k2rule.TargetProxy,
		ExpireAt: time.Now().Add(5 * time.Minute),
	}
	cache.Add("192.168.1.1", rule)

	// Get rule
	got, found := cache.GetRule("192.168.1.1")
	if !found {
		t.Fatal("Expected rule to be found")
	}

	if got.Domain != "example.com" {
		t.Errorf("Expected domain example.com, got %s", got.Domain)
	}

	if got.Action != k2rule.TargetProxy {
		t.Errorf("Expected action proxy, got %s", got.Action)
	}
}

func TestDynamicIPCache_NotFound(t *testing.T) {
	cache := NewDynamicIPCache()

	_, found := cache.GetRule("192.168.1.1")
	if found {
		t.Error("Expected rule not to be found")
	}
}

func TestDynamicIPCache_Expiration(t *testing.T) {
	cache := NewDynamicIPCache()

	// Add rule that expires in 100ms
	rule := IPRule{
		Domain:   "example.com",
		Action:   k2rule.TargetProxy,
		ExpireAt: time.Now().Add(100 * time.Millisecond),
	}
	cache.Add("192.168.1.1", rule)

	// Should be found immediately
	_, found := cache.GetRule("192.168.1.1")
	if !found {
		t.Fatal("Expected rule to be found")
	}

	// Wait for expiration
	time.Sleep(150 * time.Millisecond)

	// Should not be found after expiration
	_, found = cache.GetRule("192.168.1.1")
	if found {
		t.Error("Expected rule to be expired and not found")
	}

	// Verify it was removed from cache
	if cache.Size() != 0 {
		t.Errorf("Expected cache size 0, got %d", cache.Size())
	}
}

func TestDynamicIPCache_Update(t *testing.T) {
	cache := NewDynamicIPCache()

	// Add initial rule
	rule1 := IPRule{
		Domain:   "example.com",
		Action:   k2rule.TargetProxy,
		ExpireAt: time.Now().Add(5 * time.Minute),
	}
	cache.Add("192.168.1.1", rule1)

	// Update with new rule
	rule2 := IPRule{
		Domain:   "updated.com",
		Action:   k2rule.TargetDirect,
		ExpireAt: time.Now().Add(10 * time.Minute),
	}
	cache.Add("192.168.1.1", rule2)

	// Get updated rule
	got, found := cache.GetRule("192.168.1.1")
	if !found {
		t.Fatal("Expected rule to be found")
	}

	if got.Domain != "updated.com" {
		t.Errorf("Expected domain updated.com, got %s", got.Domain)
	}

	if got.Action != k2rule.TargetDirect {
		t.Errorf("Expected action direct, got %s", got.Action)
	}
}

func TestDynamicIPCache_Delete(t *testing.T) {
	cache := NewDynamicIPCache()

	// Add rule
	rule := IPRule{
		Domain:   "example.com",
		Action:   k2rule.TargetProxy,
		ExpireAt: time.Now().Add(5 * time.Minute),
	}
	cache.Add("192.168.1.1", rule)

	// Verify it exists
	_, found := cache.GetRule("192.168.1.1")
	if !found {
		t.Fatal("Expected rule to be found")
	}

	// Delete
	cache.Delete("192.168.1.1")

	// Verify it's gone
	_, found = cache.GetRule("192.168.1.1")
	if found {
		t.Error("Expected rule to be deleted")
	}
}

func TestDynamicIPCache_Size(t *testing.T) {
	cache := NewDynamicIPCache()

	if cache.Size() != 0 {
		t.Errorf("Expected initial size 0, got %d", cache.Size())
	}

	// Add rules
	for i := 0; i < 5; i++ {
		rule := IPRule{
			Domain:   "example.com",
			Action:   k2rule.TargetProxy,
			ExpireAt: time.Now().Add(5 * time.Minute),
		}
		cache.Add(string(rune(i)), rule)
	}

	if cache.Size() != 5 {
		t.Errorf("Expected size 5, got %d", cache.Size())
	}
}

func TestDynamicIPCache_Cleanup(t *testing.T) {
	cache := NewDynamicIPCache()

	// Add some expired rules
	for i := 0; i < 3; i++ {
		rule := IPRule{
			Domain:   "expired.com",
			Action:   k2rule.TargetProxy,
			ExpireAt: time.Now().Add(-1 * time.Minute), // Already expired
		}
		cache.Add(string(rune(i)), rule)
	}

	// Add some valid rules
	for i := 3; i < 6; i++ {
		rule := IPRule{
			Domain:   "valid.com",
			Action:   k2rule.TargetProxy,
			ExpireAt: time.Now().Add(5 * time.Minute),
		}
		cache.Add(string(rune(i)), rule)
	}

	if cache.Size() != 6 {
		t.Errorf("Expected size 6 before cleanup, got %d", cache.Size())
	}

	// Run cleanup
	cache.cleanup()

	// Only valid rules should remain
	if cache.Size() != 3 {
		t.Errorf("Expected size 3 after cleanup, got %d", cache.Size())
	}
}

func TestDynamicIPCache_ConcurrentAccess(t *testing.T) {
	cache := NewDynamicIPCache()
	var wg sync.WaitGroup

	// Concurrent writes
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			rule := IPRule{
				Domain:   "example.com",
				Action:   k2rule.TargetProxy,
				ExpireAt: time.Now().Add(5 * time.Minute),
			}
			cache.Add(string(rune(idx%10)), rule)
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			cache.GetRule(string(rune(idx % 10)))
		}(i)
	}

	wg.Wait()

	// Should have 10 unique entries
	if cache.Size() > 10 {
		t.Errorf("Expected max size 10, got %d", cache.Size())
	}
}

func TestDynamicIPCache_StartCleanup(t *testing.T) {
	cache := NewDynamicIPCache()

	// Add some rules that expire quickly
	for i := 0; i < 5; i++ {
		rule := IPRule{
			Domain:   "example.com",
			Action:   k2rule.TargetProxy,
			ExpireAt: time.Now().Add(100 * time.Millisecond),
		}
		cache.Add(string(rune(i)), rule)
	}

	if cache.Size() != 5 {
		t.Errorf("Expected size 5, got %d", cache.Size())
	}

	// Start cleanup with short interval
	cache.StartCleanup(200 * time.Millisecond)

	// Wait for cleanup to run
	time.Sleep(300 * time.Millisecond)

	// All rules should be cleaned up
	if cache.Size() != 0 {
		t.Errorf("Expected size 0 after cleanup, got %d", cache.Size())
	}
}

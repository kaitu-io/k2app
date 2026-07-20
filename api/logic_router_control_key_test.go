package center

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// routerControlKeyTestUser creates a bare user for router-control-key tests.
// Cleanup is registered on t.
func routerControlKeyTestUser(t *testing.T) *User {
	t.Helper()
	user := &User{
		UUID: "rck-user-" + time.Now().Format("150405.000000000"),
		Tier: TierBasic,
	}
	require.NoError(t, db.Get().Create(user).Error)
	t.Cleanup(func() {
		db.Get().Unscoped().Delete(user)
	})
	return user
}

func TestEnsureRouterControlKeyIdempotent(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	user := routerControlKeyTestUser(t)

	k1, err := EnsureRouterControlKey(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	if !strings.HasPrefix(k1, "rck_") || len(k1) != 4+64 {
		t.Fatalf("key format: %q", k1)
	}
	k2, err := EnsureRouterControlKey(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("second ensure: %v", err)
	}
	if k1 != k2 {
		t.Fatalf("ensure must be idempotent: %q != %q", k1, k2)
	}
}

func TestResetRouterControlKeyRotates(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	user := routerControlKeyTestUser(t)

	k1, _ := EnsureRouterControlKey(context.Background(), user.ID)
	k2, err := ResetRouterControlKey(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if k1 == k2 {
		t.Fatal("reset must rotate the key")
	}
	k3, _ := EnsureRouterControlKey(context.Background(), user.ID)
	if k3 != k2 {
		t.Fatal("ensure after reset must return the rotated key")
	}
}

// TestEnsureRouterControlKeyConcurrent guards the first-mint race: concurrent
// EnsureRouterControlKey calls for a brand-new user must all converge on the
// same winning key, and that key must match what's actually stored in the DB.
func TestEnsureRouterControlKeyConcurrent(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	user := routerControlKeyTestUser(t)

	const n = 10
	var wg sync.WaitGroup
	results := make([]string, n)
	errs := make([]error, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = EnsureRouterControlKey(context.Background(), user.ID)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: %v", i, err)
		}
	}
	want := results[0]
	if !strings.HasPrefix(want, "rck_") || len(want) != 4+64 {
		t.Fatalf("key format: %q", want)
	}
	for i, got := range results {
		if got != want {
			t.Fatalf("goroutine %d returned %q, want %q — racers did not converge", i, got, want)
		}
	}

	var stored User
	require.NoError(t, db.Get().First(&stored, user.ID).Error)
	if stored.RouterControlKey == nil || *stored.RouterControlKey != want {
		t.Fatalf("DB key = %v, want %q", stored.RouterControlKey, want)
	}
}

func TestHashRouterControlKey(t *testing.T) {
	h := HashRouterControlKey("rck_abc")
	if len(h) != 64 {
		t.Fatalf("hash length = %d, want 64", len(h))
	}
	if h == HashRouterControlKey("rck_def") {
		t.Fatal("different keys must hash differently")
	}
}

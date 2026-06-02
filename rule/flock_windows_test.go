//go:build windows

package rule

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestAcquireLock_SerializesWindows(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), ".ensure.lock")
	ctx := context.Background()

	rel1, err := acquireLock(ctx, lockPath)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	// Second acquire must block until rel1 runs. Give it a short ctx so a
	// broken (no-op) lock fails the test by returning immediately.
	ctx2, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		rel2, e := acquireLock(ctx2, lockPath)
		if rel2 != nil {
			rel2()
		}
		done <- e
	}()

	select {
	case e := <-done:
		t.Fatalf("second acquire returned before first released (err=%v) — lock is not exclusive", e)
	case <-time.After(100 * time.Millisecond):
		// Good: still blocked.
	}

	rel1()
	if e := <-done; e != nil && e != context.DeadlineExceeded {
		t.Logf("second acquire after release: %v", e)
	}
}

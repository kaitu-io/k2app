package rule

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEnsureBundles_FreshSkip(t *testing.T) {
	dir := t.TempDir()

	// Write a fresh bundles.version — EnsureBundles should see it and skip
	// any network work. We give it an unreachable source to prove no HTTP
	// request was attempted.
	versionPath := filepath.Join(dir, "bundles.version")
	if err := os.WriteFile(versionPath, []byte("2026-04-20T00:00:00Z sha256:abc\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := EnsureBundles(ctx, dir, []string{"http://127.0.0.1:1/"})
	if err != nil {
		t.Errorf("expected nil (fresh skip), got %v", err)
	}
}

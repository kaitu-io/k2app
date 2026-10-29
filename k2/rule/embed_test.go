package rule

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEmbeddedRules_IsUsableTarball(t *testing.T) {
	if len(embeddedRules) == 0 {
		t.Fatal("embeddedRules is empty — placeholder missing or build-fetch wrote nothing")
	}
	gz, err := gzip.NewReader(bytes.NewReader(embeddedRules))
	if err != nil {
		t.Fatalf("embeddedRules is not gzip: %v", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	krs := 0
	var names []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar read: %v", err)
		}
		names = append(names, hdr.Name)
		if strings.HasSuffix(hdr.Name, ".krs") {
			krs++
		}
	}
	if krs == 0 {
		t.Fatalf("embedded archive has 0 .krs (names=%v)", names)
	}
	found := false
	for _, n := range names {
		if n == "cn.krs" || strings.HasSuffix(n, "/cn.krs") {
			found = true
		}
	}
	if !found {
		t.Errorf("embedded archive missing cn.krs (names=%v)", names)
	}
}

func TestSeedFromEmbedIfEmpty_SeedsColdCache(t *testing.T) {
	dir := t.TempDir()

	if err := SeedFromEmbedIfEmpty(dir); err != nil {
		t.Fatalf("SeedFromEmbedIfEmpty: %v", err)
	}
	if countKRS(dir) == 0 {
		t.Fatal("cold cache not seeded")
	}
	if _, err := os.Stat(filepath.Join(dir, "cn.krs")); err != nil {
		t.Errorf("cn.krs not seeded: %v", err)
	}
	v, err := os.ReadFile(filepath.Join(dir, "bundles.version"))
	if err != nil {
		t.Fatalf("bundles.version not written: %v", err)
	}
	if !strings.Contains(string(v), "embedded") {
		t.Errorf("bundles.version missing 'embedded' marker: %q", v)
	}
	if !isVersionFresh(dir, bundlesFreshTTL) {
		t.Error("cache not fresh after seeding")
	}
	if _, err := os.Stat(filepath.Join(dir, ".seed-tmp")); !os.IsNotExist(err) {
		t.Errorf(".seed-tmp should be removed, stat err=%v", err)
	}
}

func TestSeedFromEmbedIfEmpty_NoOpWhenWarm(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "existing.krs")
	if err := os.WriteFile(marker, []byte{0x4b, 0x32, 0x42, 0x00}, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SeedFromEmbedIfEmpty(dir); err != nil {
		t.Fatalf("SeedFromEmbedIfEmpty on warm cache: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "cn.krs")); !os.IsNotExist(err) {
		t.Errorf("warm cache was re-seeded (cn.krs appeared), stat err=%v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("existing.krs disturbed: %v", err)
	}
}

func TestSeedFromEmbedIfEmpty_EmptyCacheDirIsNoOp(t *testing.T) {
	if err := SeedFromEmbedIfEmpty(""); err != nil {
		t.Errorf("empty cacheDir should be a no-op, got %v", err)
	}
}

// TestWasEmbedSeeded guards the provenance signal that lets the background
// updater tell an embedded floor apart from a real CDN download. Without it,
// a freshly-seeded cache reads as "fresh" and the updater honors the 24h gate
// — serving the build-time baseline untouched for a whole TTL (Finding A).
func TestWasEmbedSeeded(t *testing.T) {
	dir := t.TempDir()

	// No bundles.version yet → not embed-seeded.
	if wasEmbedSeeded(dir) {
		t.Error("empty cache must not read as embed-seeded")
	}

	// After a seed, the version line carries the embedded marker.
	if err := SeedFromEmbedIfEmpty(dir); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if !wasEmbedSeeded(dir) {
		t.Error("seeded cache must read as embed-seeded")
	}

	// A CDN-style stamp (no marker) must NOT read as embed-seeded — otherwise
	// the updater would force a download on every tick forever.
	if err := os.WriteFile(filepath.Join(dir, "bundles.version"),
		[]byte("2026-06-01T00:00:00Z sha256:deadbeef\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if wasEmbedSeeded(dir) {
		t.Error("CDN-stamped cache must not read as embed-seeded")
	}
}

func TestColdStartComposition_SeedThenEnsureOffline(t *testing.T) {
	dir := t.TempDir()
	// Mirror engine.Start(): seed the embed floor, then EnsureBundles with an
	// unreachable source. Cold start must succeed offline and leave a usable
	// cache — EnsureBundles sees the fresh seeded cache and skips the network.
	if err := SeedFromEmbedIfEmpty(dir); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := EnsureBundles(ctx, dir, []string{"http://127.0.0.1:1/"}); err != nil {
		t.Fatalf("EnsureBundles after seed should be a fresh no-op, got %v", err)
	}
	if countKRS(dir) == 0 {
		t.Fatal("composition left no .krs")
	}
}

package rule

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func writeTestBundle(t *testing.T, dir, filename string, sets []BundleSet) {
	t.Helper()
	var buf bytes.Buffer
	if err := WriteBundle(&buf, sets); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, filename), buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadEmpty(t *testing.T) {
	dir := t.TempDir()
	bundles, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundles) != 0 {
		t.Fatalf("expected 0 bundles, got %d", len(bundles))
	}
}

func TestLoadNonexistent(t *testing.T) {
	bundles, err := Load(filepath.Join(t.TempDir(), "no-such-dir"))
	if err != nil {
		t.Fatalf("expected nil error for nonexistent dir, got %v", err)
	}
	if bundles != nil {
		t.Fatalf("expected nil bundles, got %v", bundles)
	}
}

func TestLoadBundles(t *testing.T) {
	dir := t.TempDir()

	writeTestBundle(t, dir, "a.k2b", []BundleSet{
		{Name: "google", DomainSuffixes: []string{"google.com"}},
		{Name: "youtube", DomainSuffixes: []string{"youtube.com"}},
	})
	writeTestBundle(t, dir, "b.k2b", []BundleSet{
		{Name: "github", DomainSuffixes: []string{"github.com"}},
	})
	// Non-.k2b file should be ignored.
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	bundles, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundles) != 2 {
		t.Fatalf("expected 2 bundles, got %d", len(bundles))
	}

	idx := Index(bundles)
	for _, name := range []string{"google", "youtube", "github"} {
		if _, ok := idx[name]; !ok {
			t.Errorf("expected %q in index", name)
		}
	}
	if len(idx) != 3 {
		t.Errorf("expected 3 entries in index, got %d", len(idx))
	}
}

func TestLoadSkipsCorrupt(t *testing.T) {
	dir := t.TempDir()

	writeTestBundle(t, dir, "good.k2b", []BundleSet{
		{Name: "valid", DomainSuffixes: []string{"example.com"}},
	})
	// Write corrupt .k2b file.
	if err := os.WriteFile(filepath.Join(dir, "bad.k2b"), []byte("not a bundle"), 0644); err != nil {
		t.Fatal(err)
	}

	bundles, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundles) != 1 {
		t.Fatalf("expected 1 bundle (corrupt skipped), got %d", len(bundles))
	}
	if bundles[0].Sets[0].Name != "valid" {
		t.Errorf("expected set name %q, got %q", "valid", bundles[0].Sets[0].Name)
	}
}

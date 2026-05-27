package rule

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/kaitu-io/k2-rules/krs"
)

func writeBundleFile(t *testing.T, path string, sets []krs.NamedSet) {
	t.Helper()
	var buf bytes.Buffer
	if err := krs.WriteBundle(&buf, &krs.Bundle{Sets: sets}); err != nil {
		t.Fatalf("krs.WriteBundle: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
}

func TestLoadNamed_PairsFilenameWithBundle(t *testing.T) {
	dir := t.TempDir()
	writeBundleFile(t, filepath.Join(dir, "cn.krs"), []krs.NamedSet{
		{Name: "china", DomainSuffixes: []string{"baidu.com"}},
	})
	if err := os.WriteFile(filepath.Join(dir, "leftover.k2b"), []byte("ignored"), 0o644); err != nil {
		t.Fatalf("WriteFile leftover: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignored"), 0o644); err != nil {
		t.Fatalf("WriteFile notes: %v", err)
	}

	got, err := LoadNamed(dir)
	if err != nil {
		t.Fatalf("LoadNamed: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1; got=%+v", len(got), got)
	}
	if got[0].Region != "cn" {
		t.Fatalf("Region = %q, want %q", got[0].Region, "cn")
	}
	if got[0].Bundle == nil {
		t.Fatal("Bundle is nil")
	}
	if len(got[0].Bundle.Sets) != 1 || got[0].Bundle.Sets[0].Name != "china" {
		t.Fatalf("Sets = %+v, want one set named 'china'", got[0].Bundle.Sets)
	}
}

func TestLoadNamed_MissingDirReturnsNilNil(t *testing.T) {
	got, err := LoadNamed("/nonexistent/path/should/not/error")
	if err != nil {
		t.Fatalf("LoadNamed: err = %v, want nil", err)
	}
	if got != nil {
		t.Fatalf("got = %+v, want nil", got)
	}
}

func TestLoadNamed_ParseErrorSkipped(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "corrupt.krs"), []byte("not a real bundle"), 0o644); err != nil {
		t.Fatalf("WriteFile corrupt: %v", err)
	}
	writeBundleFile(t, filepath.Join(dir, "ok.krs"), []krs.NamedSet{
		{Name: "okset", DomainSuffixes: []string{"example.com"}},
	})

	got, err := LoadNamed(dir)
	if err != nil {
		t.Fatalf("LoadNamed: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1; got=%+v", len(got), got)
	}
	if got[0].Region != "ok" {
		t.Fatalf("Region = %q, want %q", got[0].Region, "ok")
	}
}

func TestLoadNamed_EmptyDirReturnsNil(t *testing.T) {
	dir := t.TempDir()
	got, err := LoadNamed(dir)
	if err != nil {
		t.Fatalf("LoadNamed: %v", err)
	}
	if got != nil {
		t.Fatalf("got = %+v, want nil", got)
	}
}

func TestLoadNamed_SubdirectoriesIgnored(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "foo")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	writeBundleFile(t, filepath.Join(sub, "inner.krs"), []krs.NamedSet{
		{Name: "inner", DomainSuffixes: []string{"inner.example"}},
	})

	got, err := LoadNamed(dir)
	if err != nil {
		t.Fatalf("LoadNamed: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("len(got) = %d, want 0 (no recursion); got=%+v", len(got), got)
	}
}

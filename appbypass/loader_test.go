package appbypass

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_EmptyDir(t *testing.T) {
	ps, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !ps.Empty() {
		t.Errorf("expected empty PresetSet from empty dir")
	}
}

func TestLoad_NonexistentDir(t *testing.T) {
	ps, err := Load(filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !ps.Empty() {
		t.Errorf("missing dir should yield empty set, not error")
	}
}

func TestLoad_FixtureCN(t *testing.T) {
	ps, err := Load("testdata")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cn := ps.Get("cn")
	if cn == nil {
		t.Fatal("expected cn preset")
	}
	if len(cn.Android.InstallerExact) == 0 {
		t.Error("empty installer_exact")
	}
	if len(cn.Desktop.ProcessPrefix) == 0 {
		t.Error("empty process_prefix")
	}
}

func TestLoad_SkipsInvalidVersion(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "app-bypass-xx.yaml"),
		[]byte("version: 999\nregion: xx\n"), 0644)
	os.WriteFile(filepath.Join(dir, "app-bypass-yy.yaml"),
		[]byte("version: 1\nregion: yy\n"), 0644)
	ps, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if ps.Get("xx") != nil {
		t.Error("xx (bad version) should be skipped")
	}
	if ps.Get("yy") == nil {
		t.Error("yy (valid) should load")
	}
}

func TestLoad_SkipsMalformedYAML(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "app-bypass-zz.yaml"),
		[]byte("garbage::\n: : :"), 0644)
	ps, err := Load(dir)
	if err != nil {
		t.Fatalf("Load should not propagate per-file errors: %v", err)
	}
	if !ps.Empty() {
		t.Error("malformed file should be silently skipped")
	}
}

func TestLoad_IgnoresNonYAMLFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "cn-direct.k2b"), []byte("binary"), 0644)
	os.WriteFile(filepath.Join(dir, "bundles.version"), []byte("anything"), 0644)
	ps, err := Load(dir)
	if err != nil || !ps.Empty() {
		t.Errorf("non-yaml files should be ignored; err=%v empty=%v", err, ps.Empty())
	}
}

func TestLoad_FilenameRegionMismatch(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "app-bypass-cn.yaml"),
		[]byte("version: 1\nregion: ir\n"), 0644)
	ps, _ := Load(dir)
	if !ps.Empty() {
		t.Error("filename/region mismatch should cause skip")
	}
}

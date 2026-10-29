package config

import (
	"os"
	"path/filepath"
	"testing"
)

// searchClientConfig is a testable version of SearchClientConfig that accepts
// candidate paths instead of using hardcoded system paths.
func searchClientConfig(candidates []string) (string, error) {
	return searchFirstExisting(candidates)
}

// searchServerConfig is a testable version of SearchServerConfig that accepts
// candidate paths instead of using hardcoded system paths.
func searchServerConfig(candidates []string) (string, error) {
	return searchFirstExisting(candidates)
}

func TestSearchConfigClient_CurrentDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "k2.yaml")
	if err := os.WriteFile(path, []byte("server: k2v5://test"), 0644); err != nil {
		t.Fatal(err)
	}

	found, err := searchClientConfig([]string{path})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found != path {
		t.Errorf("found = %q, want %q", found, path)
	}
}

func TestSearchConfigClient_UserDir(t *testing.T) {
	userDir := t.TempDir()
	userCfg := filepath.Join(userDir, ".config", "k2", "k2.yaml")
	if err := os.MkdirAll(filepath.Dir(userCfg), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userCfg, []byte("server: k2v5://test"), 0644); err != nil {
		t.Fatal(err)
	}

	found, err := searchClientConfig([]string{userCfg})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found != userCfg {
		t.Errorf("found = %q, want %q", found, userCfg)
	}
}

func TestSearchConfigClient_SystemDir(t *testing.T) {
	sysDir := t.TempDir()
	sysCfg := filepath.Join(sysDir, "etc", "k2", "k2.yaml")
	if err := os.MkdirAll(filepath.Dir(sysCfg), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sysCfg, []byte("server: k2v5://test"), 0644); err != nil {
		t.Fatal(err)
	}

	found, err := searchClientConfig([]string{sysCfg})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found != sysCfg {
		t.Errorf("found = %q, want %q", found, sysCfg)
	}
}

func TestSearchConfigClient_Priority(t *testing.T) {
	dir := t.TempDir()

	// Create both candidate files; first one should win.
	first := filepath.Join(dir, "k2.yaml")
	second := filepath.Join(dir, "etc", "k2", "k2.yaml")
	if err := os.MkdirAll(filepath.Dir(second), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(first, []byte("server: k2v5://first"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("server: k2v5://second"), 0644); err != nil {
		t.Fatal(err)
	}

	found, err := searchClientConfig([]string{first, second})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found != first {
		t.Errorf("found = %q, want %q (first file should win)", found, first)
	}
}

func TestSearchConfigClient_NotFound(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "nonexistent.yaml")

	found, err := searchClientConfig([]string{missing})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found != "" {
		t.Errorf("found = %q, want empty string", found)
	}
}

func TestSearchConfigServer_CurrentDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "k2s.yaml")
	if err := os.WriteFile(path, []byte("listen: :443"), 0644); err != nil {
		t.Fatal(err)
	}

	found, err := searchServerConfig([]string{path})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found != path {
		t.Errorf("found = %q, want %q", found, path)
	}
}

func TestSearchConfigServer_SystemDir(t *testing.T) {
	sysDir := t.TempDir()
	sysCfg := filepath.Join(sysDir, "etc", "k2s", "k2s.yaml")
	if err := os.MkdirAll(filepath.Dir(sysCfg), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sysCfg, []byte("listen: :443"), 0644); err != nil {
		t.Fatal(err)
	}

	found, err := searchServerConfig([]string{sysCfg})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found != sysCfg {
		t.Errorf("found = %q, want %q", found, sysCfg)
	}
}

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIncludeGlob(t *testing.T) {
	dir := t.TempDir()

	// Write two YAML files that match a glob
	if err := os.WriteFile(filepath.Join(dir, "a.yaml"), []byte("key_a: value_a\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.yaml"), []byte("key_b: value_b\n"), 0644); err != nil {
		t.Fatal(err)
	}

	base := map[string]any{}
	if err := ResolveInclude(filepath.Join(dir, "*.yaml"), base); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if base["key_a"] != "value_a" {
		t.Errorf("key_a = %v, want value_a", base["key_a"])
	}
	if base["key_b"] != "value_b" {
		t.Errorf("key_b = %v, want value_b", base["key_b"])
	}
}

func TestIncludeMerge(t *testing.T) {
	dir := t.TempDir()

	// a.yaml sets shared_key to "from_a"; b.yaml overrides it to "from_b"
	// Files are sorted alphabetically, so a comes first, b overrides
	if err := os.WriteFile(filepath.Join(dir, "a.yaml"), []byte("shared_key: from_a\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.yaml"), []byte("shared_key: from_b\n"), 0644); err != nil {
		t.Fatal(err)
	}

	base := map[string]any{}
	if err := ResolveInclude(filepath.Join(dir, "*.yaml"), base); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if base["shared_key"] != "from_b" {
		t.Errorf("shared_key = %v, want from_b (later file should win)", base["shared_key"])
	}
}

func TestIncludeNested(t *testing.T) {
	// Nested includes (include field inside included file) are NOT supported.
	// The included file's "include" key is just treated as data, not processed.
	dir := t.TempDir()

	// First file references another file via "include" key — should NOT be recursed.
	nested := filepath.Join(dir, "nested.yaml")
	if err := os.WriteFile(nested, []byte("nested_key: nested_value\n"), 0644); err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(dir, "main.yaml")
	if err := os.WriteFile(main, []byte("include: "+nested+"\nmain_key: main_value\n"), 0644); err != nil {
		t.Fatal(err)
	}

	base := map[string]any{}
	if err := ResolveInclude(main, base); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// main_key should be loaded
	if base["main_key"] != "main_value" {
		t.Errorf("main_key = %v, want main_value", base["main_key"])
	}
	// nested_key should NOT be loaded (no recursion)
	if base["nested_key"] != nil {
		t.Errorf("nested_key should not be present (no nested include support), got %v", base["nested_key"])
	}
}

func TestIncludeBadYAML(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(bad, []byte(":\tinvalid: yaml: [\n"), 0644); err != nil {
		t.Fatal(err)
	}

	base := map[string]any{}
	err := ResolveInclude(bad, base)
	if err == nil {
		t.Fatal("expected error for bad YAML, got nil")
	}
	// Error message should include the file name
	if !containsStr(err.Error(), "bad.yaml") {
		t.Errorf("error should mention file name 'bad.yaml', got: %v", err)
	}
}

func TestIncludeNoMatch(t *testing.T) {
	dir := t.TempDir()

	// Glob that matches nothing
	base := map[string]any{"existing": "value"}
	if err := ResolveInclude(filepath.Join(dir, "*.yaml"), base); err != nil {
		t.Fatalf("no-match glob should return nil error, got: %v", err)
	}
	// Existing base data should be unchanged
	if base["existing"] != "value" {
		t.Errorf("existing key should be preserved, got %v", base["existing"])
	}
}

func TestLoadClientWithInclude(t *testing.T) {
	dir := t.TempDir()
	confDir := filepath.Join(dir, "conf.d")
	if err := os.Mkdir(confDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Write an include fragment that sets server URL
	fragment := filepath.Join(confDir, "server.yaml")
	if err := os.WriteFile(fragment, []byte("server: k2v5://test.example.com\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Write main config with include directive
	main := filepath.Join(dir, "k2.yaml")
	mainContent := "listen: 127.0.0.1:1777\ninclude: " + filepath.Join(confDir, "*.yaml") + "\n"
	if err := os.WriteFile(main, []byte(mainContent), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadClient(main)
	if err != nil {
		t.Fatalf("LoadClient with include: %v", err)
	}

	if cfg.Server != "k2v5://test.example.com" {
		t.Errorf("server = %q, want k2v5://test.example.com (from included file)", cfg.Server)
	}
	if cfg.Listen != "127.0.0.1:1777" {
		t.Errorf("listen = %q, want 127.0.0.1:1777 (from main config)", cfg.Listen)
	}
}

// containsStr is a helper to check if a string contains a substring.
func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}

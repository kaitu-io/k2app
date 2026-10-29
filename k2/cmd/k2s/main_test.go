package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestK2sTestConfig_Valid verifies that runTestConfig with a valid config file
// prints success and returns nil error.
func TestK2sTestConfig_Valid(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "server.yaml")

	// Write a valid server config
	validYAML := `listen: ":9443"
`
	if err := os.WriteFile(cfgPath, []byte(validYAML), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var out bytes.Buffer
	err := runTestConfig(cfgPath, &out)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "OK") && !strings.Contains(got, "valid") {
		t.Errorf("expected success output, got: %q", got)
	}
}

// TestK2sTestConfig_Invalid verifies that runTestConfig with bad YAML returns an error.
func TestK2sTestConfig_Invalid(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "bad.yaml")

	// Write invalid YAML (genuine parse error)
	badYAML := "key: [unclosed bracket\n"
	if err := os.WriteFile(cfgPath, []byte(badYAML), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var out bytes.Buffer
	err := runTestConfig(cfgPath, &out)
	if err == nil {
		t.Fatal("expected error for bad YAML, got nil")
	}
}


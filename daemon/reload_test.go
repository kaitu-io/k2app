package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/engine"
)

// writeConfigFile writes a YAML client config file to the given path.
func writeConfigFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writeConfigFile: %v", err)
	}
}

// TestReload_DynamicFields verifies that changing routes in the config file
// causes Reload() to update the daemon's lastConfig.Server (derived) without reconnecting.
func TestReload_DynamicFields(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	initial := "routes:\n  - via: \"k2v5://user:pass@initial.host:443\"\n    match:\n      all: true\n"
	writeConfigFile(t, cfgPath, initial)

	d := New()
	d.ConfigPath = cfgPath

	// Pre-set lastConfig to simulate a connected daemon.
	initialCfg := config.ClientFromURL("k2v5://user:pass@initial.host:443")
	d.mu.Lock()
	d.lastConfig = initialCfg
	d.lastStatus.State = engine.StateConnected
	d.mu.Unlock()

	// Now update the config file with a new server URL.
	updated := "routes:\n  - via: \"k2v5://user:pass@new.host:443\"\n    match:\n      all: true\n"
	writeConfigFile(t, cfgPath, updated)

	if err := d.Reload(); err != nil {
		t.Fatalf("Reload() error: %v", err)
	}

	d.mu.RLock()
	gotServer := d.lastConfig.Server
	d.mu.RUnlock()

	if gotServer != "k2v5://user:pass@new.host:443" {
		t.Errorf("lastConfig.Server = %q, want k2v5://user:pass@new.host:443", gotServer)
	}
}

// TestReload_StaticFieldWarning verifies that changing `listen` logs a warning
// and the daemon's lastConfig.Listen field remains unchanged.
func TestReload_StaticFieldWarning(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	// Write initial config.
	initial := "routes:\n  - via: \"k2v5://user:pass@host:443\"\n    match:\n      all: true\nlisten: \"127.0.0.1:1777\"\n"
	writeConfigFile(t, cfgPath, initial)

	d := New()
	d.ConfigPath = cfgPath

	initialCfg := config.ClientFromURL("k2v5://user:pass@host:443")
	initialCfg.Listen = "127.0.0.1:1777"
	d.mu.Lock()
	d.lastConfig = initialCfg
	d.lastStatus.State = engine.StateDisconnected
	d.mu.Unlock()

	// Write updated config with changed listen address.
	updated := "routes:\n  - via: \"k2v5://user:pass@host:443\"\n    match:\n      all: true\nlisten: \"127.0.0.1:9999\"\n"
	writeConfigFile(t, cfgPath, updated)

	// Capture log output.
	var logBuf strings.Builder
	origLogger := setTestLogger(&logBuf)
	defer restoreTestLogger(origLogger)

	if err := d.Reload(); err != nil {
		t.Fatalf("Reload() error: %v", err)
	}

	// Static field `listen` must NOT have changed.
	d.mu.RLock()
	gotListen := d.lastConfig.Listen
	d.mu.RUnlock()

	if gotListen != "127.0.0.1:1777" {
		t.Errorf("lastConfig.Listen = %q, want 127.0.0.1:1777 (static field must not change)", gotListen)
	}

	// A warning must have been logged.
	logOut := logBuf.String()
	if !strings.Contains(logOut, "listen") {
		t.Errorf("expected warning about 'listen' in log output, got: %q", logOut)
	}
}

// TestReload_DNSUpdate verifies that changing DNS servers in the config file
// causes Reload() to apply the new DNS configuration.
func TestReload_DNSUpdate(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	initial := "routes:\n  - via: \"k2v5://user:pass@host:443\"\n    match:\n      all: true\ndns:\n  direct:\n    - 8.8.8.8:53\n"
	writeConfigFile(t, cfgPath, initial)

	d := New()
	d.ConfigPath = cfgPath

	initialCfg := config.ClientFromURL("k2v5://user:pass@host:443")
	initialCfg.DNS.Direct = []string{"8.8.8.8:53"}
	d.mu.Lock()
	d.lastConfig = initialCfg
	d.lastStatus.State = engine.StateDisconnected
	d.mu.Unlock()

	// Update DNS servers.
	updated := "routes:\n  - via: \"k2v5://user:pass@host:443\"\n    match:\n      all: true\ndns:\n  direct:\n    - 1.1.1.1:53\n    - 9.9.9.9:53\n"
	writeConfigFile(t, cfgPath, updated)

	if err := d.Reload(); err != nil {
		t.Fatalf("Reload() error: %v", err)
	}

	d.mu.RLock()
	gotDirect := d.lastConfig.DNS.Direct
	d.mu.RUnlock()

	if len(gotDirect) != 2 {
		t.Fatalf("DNS.Direct len = %d, want 2", len(gotDirect))
	}
	if gotDirect[0] != "1.1.1.1:53" {
		t.Errorf("DNS.Direct[0] = %q, want 1.1.1.1:53", gotDirect[0])
	}
	if gotDirect[1] != "9.9.9.9:53" {
		t.Errorf("DNS.Direct[1] = %q, want 9.9.9.9:53", gotDirect[1])
	}
}

// TestReload_LogLevel verifies that changing log.level in the config file
// causes Reload() to apply the new log level immediately.
func TestReload_LogLevel(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	initial := "routes:\n  - via: \"k2v5://user:pass@host:443\"\n    match:\n      all: true\nlog:\n  level: info\n"
	writeConfigFile(t, cfgPath, initial)

	d := New()
	d.ConfigPath = cfgPath

	initialCfg := config.ClientFromURL("k2v5://user:pass@host:443")
	initialCfg.Log.Level = "info"
	d.mu.Lock()
	d.lastConfig = initialCfg
	d.lastStatus.State = engine.StateDisconnected
	d.mu.Unlock()

	// Update log level.
	updated := "routes:\n  - via: \"k2v5://user:pass@host:443\"\n    match:\n      all: true\nlog:\n  level: debug\n"
	writeConfigFile(t, cfgPath, updated)

	if err := d.Reload(); err != nil {
		t.Fatalf("Reload() error: %v", err)
	}

	d.mu.RLock()
	gotLevel := d.lastConfig.Log.Level
	d.mu.RUnlock()

	if gotLevel != "debug" {
		t.Errorf("Log.Level = %q, want debug", gotLevel)
	}
}

// TestReload_InvalidConfig verifies that broken YAML causes Reload() to return
// an error while keeping the old config unchanged.
func TestReload_InvalidConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	valid := "routes:\n  - via: \"k2v5://user:pass@host:443\"\n    match:\n      all: true\n"
	writeConfigFile(t, cfgPath, valid)

	d := New()
	d.ConfigPath = cfgPath

	initialCfg := config.ClientFromURL("k2v5://user:pass@host:443")
	d.mu.Lock()
	d.lastConfig = initialCfg
	d.lastStatus.State = engine.StateDisconnected
	d.mu.Unlock()

	// Write invalid YAML (unclosed bracket — a genuine parse error).
	writeConfigFile(t, cfgPath, "server: [unclosed bracket")

	err := d.Reload()
	if err == nil {
		t.Fatal("Reload() should return error for invalid YAML, got nil")
	}

	// Old config must still be in place.
	d.mu.RLock()
	gotServer := d.lastConfig.Server
	d.mu.RUnlock()

	if gotServer != "k2v5://user:pass@host:443" {
		t.Errorf("lastConfig.Server = %q after failed reload, want original k2v5://user:pass@host:443", gotServer)
	}
}

// TestReload_NoConfigPath verifies that Reload() returns an error when
// ConfigPath is empty.
func TestReload_NoConfigPath(t *testing.T) {
	d := New()
	// ConfigPath is intentionally left empty.

	err := d.Reload()
	if err == nil {
		t.Fatal("Reload() should return error when ConfigPath is empty, got nil")
	}
	if !strings.Contains(err.Error(), "no config file to reload") {
		t.Errorf("error = %q, want to contain 'no config file to reload'", err.Error())
	}
}

// setTestLogger redirects the standard log package output to the given writer
// and returns the original flags and writer so they can be restored.
// Returns an opaque restore value.
func setTestLogger(w *strings.Builder) interface{} {
	return redirectLog(w)
}

// restoreTestLogger restores the standard logger to the state returned by setTestLogger.
func restoreTestLogger(orig interface{}) {
	restoreLog(orig)
}

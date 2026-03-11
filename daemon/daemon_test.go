package daemon

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/engine"
)

// ==========================================================================
// Config-driven connect tests (F1)
// ==========================================================================

func TestHandleUpConfigJSON(t *testing.T) {
	d := New()
	d.StartFunc = mockStartFunc

	body := `{"action":"up","params":{"config":{"server":"k2v5://test:token@host:443"}}}`
	req := httptest.NewRequest("POST", "/api/core", bytes.NewReader([]byte(body)))
	w := httptest.NewRecorder()
	d.handleCore(w, req)

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Code != 0 {
		t.Fatalf("expected code 0, got %d: %s", resp.Code, resp.Message)
	}

	if d.State() != engine.StateConnected {
		t.Fatalf("expected connected, got %s", d.State())
	}
}

func TestHandleUpRejectsWireURL(t *testing.T) {
	d := New()

	// Sending wire_url directly (legacy API) should be rejected.
	body := `{"action":"up","params":{"wire_url":"k2v5://test:token@host:443"}}`
	req := httptest.NewRequest("POST", "/api/core", bytes.NewReader([]byte(body)))
	w := httptest.NewRecorder()
	d.handleCore(w, req)

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Code != 510 {
		t.Fatalf("expected code 510 for legacy wire_url, got %d: %s", resp.Code, resp.Message)
	}
}

func TestDoUpAcceptsClientConfig(t *testing.T) {
	var captured engine.Config
	d := New()
	d.StartFunc = func(eng *engine.Engine, cfg engine.Config) error {
		captured = cfg
		eng.ForceConnected()
		return nil
	}

	clientCfg := &config.ClientConfig{
		Server: "k2v5://test:token@host:443",
	}
	config.SetDefaults(clientCfg)

	if err := d.doUp(clientCfg, 0); err != nil {
		t.Fatalf("doUp: %v", err)
	}

	if d.State() != engine.StateConnected {
		t.Fatalf("expected connected, got %s", d.State())
	}
	if captured.Client == nil {
		t.Fatal("engine Client is nil")
	}
	if captured.Client.Server != "k2v5://test:token@host:443" {
		t.Fatalf("engine Client.Server = %q, want k2v5://test:token@host:443", captured.Client.Server)
	}
}

func TestEngineConfigFromClientConfig_TunFields(t *testing.T) {
	var captured engine.Config
	d := New()
	d.StartFunc = func(eng *engine.Engine, cfg engine.Config) error {
		captured = cfg
		eng.ForceConnected()
		return nil
	}

	clientCfg := &config.ClientConfig{
		Server: "k2v5://test:token@host:443",
		Tun: config.TunConfig{
			IPv4: "10.0.0.2/24",
			IPv6: "fd00::2/128",
		},
	}
	config.SetDefaults(clientCfg)

	if err := d.doUp(clientCfg, 0); err != nil {
		t.Fatalf("doUp: %v", err)
	}

	if captured.Client.Tun.IPv4 != "10.0.0.2/24" {
		t.Errorf("engine Client.Tun.IPv4 = %q, want 10.0.0.2/24", captured.Client.Tun.IPv4)
	}
	if captured.Client.Tun.IPv6 != "fd00::2/128" {
		t.Errorf("engine Client.Tun.IPv6 = %q, want fd00::2/128", captured.Client.Tun.IPv6)
	}
}

func TestEngineConfigFromClientConfig_TunDefaults(t *testing.T) {
	var captured engine.Config
	d := New()
	d.StartFunc = func(eng *engine.Engine, cfg engine.Config) error {
		captured = cfg
		eng.ForceConnected()
		return nil
	}

	// No Tun fields set — SetDefaults fills platform defaults.
	clientCfg := &config.ClientConfig{
		Server: "k2v5://test:token@host:443",
	}
	config.SetDefaults(clientCfg)

	if err := d.doUp(clientCfg, 0); err != nil {
		t.Fatalf("doUp: %v", err)
	}

	if captured.Client.Tun.IPv4 != config.DefaultTunIPv4 {
		t.Errorf("engine Client.Tun.IPv4 = %q, want %q", captured.Client.Tun.IPv4, config.DefaultTunIPv4)
	}
	if captured.Client.Tun.IPv6 != config.DefaultTunIPv6 {
		t.Errorf("engine Client.Tun.IPv6 = %q, want %q", captured.Client.Tun.IPv6, config.DefaultTunIPv6)
	}
}

func TestEngineConfigFromClientConfig_ProxyModeUnchanged(t *testing.T) {
	var captured engine.Config
	d := New()
	d.StartFunc = func(eng *engine.Engine, cfg engine.Config) error {
		captured = cfg
		eng.ForceConnected()
		return nil
	}

	clientCfg := &config.ClientConfig{
		Server: "k2v5://test:token@host:443",
		Mode:   "proxy",
		Tun: config.TunConfig{
			IPv4: "10.0.0.2/24",
		},
	}
	clientCfg.Proxy.Listen = "127.0.0.1:8080"
	config.SetDefaults(clientCfg)

	if err := d.doUp(clientCfg, 0); err != nil {
		t.Fatalf("doUp: %v", err)
	}

	if captured.Client.Mode != "proxy" {
		t.Errorf("engine Client.Mode = %q, want proxy", captured.Client.Mode)
	}
	if captured.Client.Proxy.Listen != "127.0.0.1:8080" {
		t.Errorf("engine Client.Proxy.Listen = %q, want 127.0.0.1:8080", captured.Client.Proxy.Listen)
	}
	// TUN fields still pass through even in proxy mode (engine ignores them).
	if captured.Client.Tun.IPv4 != "10.0.0.2/24" {
		t.Errorf("engine Client.Tun.IPv4 = %q, want 10.0.0.2/24", captured.Client.Tun.IPv4)
	}
}

func TestPersistedStateWithClientConfig(t *testing.T) {
	// Use state file path and clean up.
	os.Remove(stateFilePath())
	defer os.Remove(stateFilePath())

	cfg := &config.ClientConfig{
		Server: "k2v5://test:token@host:443",
		Mode:   "tun",
	}
	config.SetDefaults(cfg)

	st := persistedState{
		Config:    cfg,
		State:     engine.StateConnected,
		Timestamp: time.Now(),
	}
	saveState(st)

	loaded, err := loadState()
	if err != nil {
		t.Fatalf("loadState: %v", err)
	}

	if loaded.Config == nil {
		t.Fatal("loaded Config is nil")
	}
	if loaded.Config.Server != cfg.Server {
		t.Fatalf("Config.Server = %q, want %q", loaded.Config.Server, cfg.Server)
	}
	if loaded.Config.Mode != cfg.Mode {
		t.Fatalf("Config.Mode = %q, want %q", loaded.Config.Mode, cfg.Mode)
	}
}

func TestStatusIncludesClientConfig(t *testing.T) {
	d := New()
	d.StartFunc = mockStartFunc

	// Connect via config JSON.
	upBody := `{"action":"up","params":{"config":{"server":"k2v5://test:tok@host:443","mode":"tun"}}}`
	req := httptest.NewRequest("POST", "/api/core", bytes.NewReader([]byte(upBody)))
	w := httptest.NewRecorder()
	d.handleCore(w, req)

	// Status should include config.
	body := `{"action":"status"}`
	req = httptest.NewRequest("POST", "/api/core", bytes.NewReader([]byte(body)))
	w = httptest.NewRecorder()
	d.handleCore(w, req)

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Code != 0 {
		t.Fatalf("expected code 0, got %d: %s", resp.Code, resp.Message)
	}

	data := resp.Data.(map[string]any)
	cfgRaw, ok := data["config"]
	if !ok {
		t.Fatal("expected config field in status response")
	}

	cfg, ok := cfgRaw.(map[string]any)
	if !ok {
		t.Fatalf("expected config to be a map, got %T", cfgRaw)
	}
	if cfg["server"] != "k2v5://test:tok@host:443" {
		t.Fatalf("expected server in config, got %v", cfg["server"])
	}
	// Should NOT have legacy wire_url key.
	if _, ok := cfg["wire_url"]; ok {
		t.Fatalf("status config should not contain legacy wire_url key")
	}
}

// ==========================================================================
// Daemon error alignment tests (T2)
// ==========================================================================

func TestDaemon_StatusWithStructuredError(t *testing.T) {
	d := New()
	d.StartFunc = failingStartFunc // returns fmt.Errorf("simulated failure")

	cfg := config.ClientFromURL("k2v5://test:tok@host:443")
	err := d.doUp(cfg, 0)
	if err == nil {
		t.Fatal("expected error from failing starter")
	}

	// Status should contain a structured error object, not a plain string.
	info := d.statusInfo()
	errRaw, ok := info["error"]
	if !ok {
		t.Fatal("expected error key in status after failed doUp")
	}

	// Serialize to JSON and re-parse to verify structure.
	// The error must be an object with "code" and "message" fields.
	data, marshalErr := json.Marshal(info)
	if marshalErr != nil {
		t.Fatalf("json.Marshal(statusInfo): %v", marshalErr)
	}

	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	var errObj struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(parsed["error"], &errObj); err != nil {
		t.Fatalf("error field is not a structured object: %v (raw: %s, type: %T)", err, string(parsed["error"]), errRaw)
	}

	if errObj.Code == 0 {
		t.Fatal("expected non-zero error code")
	}
	if errObj.Message == "" {
		t.Fatal("expected non-empty error message")
	}
}

func TestDaemon_StatusNoError(t *testing.T) {
	d := New()

	// Fresh daemon, no doUp called — status should not have "error" key.
	info := d.statusInfo()
	if _, ok := info["error"]; ok {
		t.Fatalf("expected no error key in fresh daemon status, got %v", info["error"])
	}
}

func TestDaemon_DoUpClearsLastError(t *testing.T) {
	d := New()
	cfg := config.ClientFromURL("k2v5://test:tok@host:443")

	// First: fail.
	d.StartFunc = failingStartFunc
	if err := d.doUp(cfg, 0); err == nil {
		t.Fatal("expected error from failing starter")
	}

	// Verify error is set.
	info := d.statusInfo()
	if info["error"] == nil {
		t.Fatal("expected error after failed doUp")
	}

	// Second: succeed.
	d.StartFunc = mockStartFunc
	if err := d.doUp(cfg, 0); err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	// After success, error should be cleared (nil, not present in map).
	info = d.statusInfo()
	if info["error"] != nil {
		t.Fatalf("expected error cleared after successful doUp, got %v", info["error"])
	}
}

func TestAutoReconnectWithSavedConfig(t *testing.T) {
	os.Remove(stateFilePath())
	defer os.Remove(stateFilePath())

	cfg := &config.ClientConfig{
		Server: "k2v5://test:token@host:443",
	}
	config.SetDefaults(cfg)

	// Save connected state with Config field.
	saveState(persistedState{
		Config:    cfg,
		State:     engine.StateConnected,
		Timestamp: time.Now(),
	})

	// Verify loadState returns the config.
	loaded, err := loadState()
	if err != nil {
		t.Fatalf("loadState: %v", err)
	}
	if loaded.Config == nil {
		t.Fatal("loaded Config is nil")
	}
	if loaded.Config.Server != "k2v5://test:token@host:443" {
		t.Fatalf("Config.Server = %q, want k2v5://test:token@host:443", loaded.Config.Server)
	}
}

// ==========================================================================
// doUp abort tests
// ==========================================================================

func TestDoUpAbortedByDown(t *testing.T) {
	// Channel-gated StartFunc: blocks until we release it.
	gate := make(chan struct{})
	d := New()
	d.StartFunc = func(eng *engine.Engine, cfg engine.Config) error {
		<-gate // block until test signals
		eng.ForceConnected()
		return nil
	}

	cfg := config.ClientFromURL("k2v5://test:tok@host:443")

	var wg sync.WaitGroup
	var upErr error

	// Launch doUp in background — it will block on gate.
	wg.Add(1)
	go func() {
		defer wg.Done()
		upErr = d.doUp(cfg, 0)
	}()

	// Give doUp time to acquire opMu and reach the gate.
	time.Sleep(50 * time.Millisecond)

	// doDown signals abort and queues behind opMu.
	go d.doDown()

	// Give doDown time to set abortUp and start waiting on opMu.
	time.Sleep(50 * time.Millisecond)

	// Release the gate — engine.Start completes, doUp checks abortUp.
	close(gate)

	// Wait for doUp to finish.
	wg.Wait()

	// doUp should have been aborted.
	if upErr == nil {
		t.Fatal("expected doUp to return error when aborted by doDown")
	}

	// Engine should NOT be installed (aborted after start).
	d.mu.RLock()
	eng := d.engine
	d.mu.RUnlock()
	if eng != nil {
		t.Fatal("expected engine to be nil after aborted doUp")
	}

	// State should be disconnected.
	if d.State() != engine.StateDisconnected {
		t.Fatalf("expected disconnected, got %s", d.State())
	}
}

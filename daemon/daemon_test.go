package daemon

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
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
	d.EngineStarter = mockEngineStarter

	body := `{"action":"up","params":{"config":{"server":"k2v5://test:token@host:443"}}}`
	req := httptest.NewRequest("POST", "/api/core", bytes.NewReader([]byte(body)))
	w := httptest.NewRecorder()
	d.handleCore(w, req)

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Code != 0 {
		t.Fatalf("expected code 0, got %d: %s", resp.Code, resp.Message)
	}

	if d.State() != StateConnected {
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
	d.EngineStarter = func(cfg engine.Config) (*engine.Engine, error) {
		captured = cfg
		return engine.New(), nil
	}

	clientCfg := &config.ClientConfig{
		Server: "k2v5://test:token@host:443",
	}
	config.SetDefaults(clientCfg)

	if err := d.doUp(clientCfg, 0); err != nil {
		t.Fatalf("doUp: %v", err)
	}

	if d.State() != StateConnected {
		t.Fatalf("expected connected, got %s", d.State())
	}
	if captured.WireURL != "k2v5://test:token@host:443" {
		t.Fatalf("engine WireURL = %q, want k2v5://test:token@host:443", captured.WireURL)
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
		State:     StateConnected,
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
	d.EngineStarter = mockEngineStarter

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
	d.EngineStarter = failingEngineStarter // returns fmt.Errorf("simulated failure")

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
	d.EngineStarter = failingEngineStarter
	if err := d.doUp(cfg, 0); err == nil {
		t.Fatal("expected error from failing starter")
	}

	// Verify error is set.
	info := d.statusInfo()
	if info["error"] == nil {
		t.Fatal("expected error after failed doUp")
	}

	// Second: succeed.
	d.EngineStarter = mockEngineStarter
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
		State:     StateConnected,
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

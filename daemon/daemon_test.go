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

func TestHandleGetConfigReturnsClientConfig(t *testing.T) {
	d := New()
	d.EngineStarter = mockEngineStarter

	// Connect via config JSON.
	upBody := `{"action":"up","params":{"config":{"server":"k2v5://test:tok@host:443","mode":"tun"}}}`
	req := httptest.NewRequest("POST", "/api/core", bytes.NewReader([]byte(upBody)))
	w := httptest.NewRecorder()
	d.handleCore(w, req)

	// Get config.
	body := `{"action":"get_config"}`
	req = httptest.NewRequest("POST", "/api/core", bytes.NewReader([]byte(body)))
	w = httptest.NewRecorder()
	d.handleCore(w, req)

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Code != 0 {
		t.Fatalf("expected code 0, got %d: %s", resp.Code, resp.Message)
	}

	// The response data should be a full ClientConfig with server field.
	data := resp.Data.(map[string]any)
	if data["server"] != "k2v5://test:tok@host:443" {
		t.Fatalf("expected server in config, got %v", data)
	}
	// Should NOT have legacy wire_url key.
	if _, ok := data["wire_url"]; ok {
		t.Fatalf("get_config should not return legacy wire_url key")
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

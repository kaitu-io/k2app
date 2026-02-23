package appext

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

// ==========================================================================
// Mock helpers
// ==========================================================================

// mockEventHandler records calls for assertion.
type mockEventHandler struct {
	mu     sync.Mutex
	states []string
	errors []string
}

func (m *mockEventHandler) OnStateChange(state string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.states = append(m.states, state)
}

func (m *mockEventHandler) OnError(message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errors = append(m.errors, message)
}

func (m *mockEventHandler) OnStats(txBytes, rxBytes int64) {}

func (m *mockEventHandler) getStates() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]string, len(m.states))
	copy(cp, m.states)
	return cp
}

func (m *mockEventHandler) getErrors() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]string, len(m.errors))
	copy(cp, m.errors)
	return cp
}

// testEngineConfig returns an EngineConfig suitable for tests.
// Uses empty CacheDir so k2rule.Init fails fast (no network downloads).
func testEngineConfig() *EngineConfig {
	return &EngineConfig{}
}

// ==========================================================================
// Lifecycle tests
// ==========================================================================

func TestEngine_NewEngine(t *testing.T) {
	e := NewEngine()
	if e == nil {
		t.Fatal("NewEngine returned nil")
	}
	if e.Status() != StateDisconnected {
		t.Fatalf("state = %q, want %q", e.Status(), StateDisconnected)
	}
}

func TestEngine_Start_InvalidURL(t *testing.T) {
	e := NewEngine()
	cfg := `{"server":"garbage"}`
	err := e.Start(cfg, 0, testEngineConfig())
	if err == nil {
		t.Fatal("Start with invalid URL should return error")
	}
	if e.Status() != StateDisconnected {
		t.Fatalf("state = %q, want %q after failed start", e.Status(), StateDisconnected)
	}
}

func TestEngine_Start_NilConfig(t *testing.T) {
	e := NewEngine()
	err := e.Start(`{"server":"garbage"}`, 0, nil)
	if err == nil {
		t.Fatal("Start with nil EngineConfig should return error")
	}
	if !strings.Contains(err.Error(), "EngineConfig is required") {
		t.Fatalf("expected 'EngineConfig is required' error, got: %v", err)
	}
}

func TestEngine_DoubleStart(t *testing.T) {
	e := NewEngine()
	cfg := `{"server":"k2v5://udid:token@127.0.0.1:12345?insecure=1"}`
	ecfg := testEngineConfig()

	_ = e.Start(cfg, 0, ecfg)
	err := e.Start(cfg, 0, ecfg)
	_ = err
}

func TestEngine_Stop_NotStarted(t *testing.T) {
	e := NewEngine()
	err := e.Stop()
	if err != nil {
		t.Fatalf("Stop on fresh engine should not error, got %v", err)
	}
}

func TestEngine_Status(t *testing.T) {
	e := NewEngine()
	if e.Status() != StateDisconnected {
		t.Fatalf("new engine status = %q, want %q", e.Status(), StateDisconnected)
	}
}

// ==========================================================================
// EventHandler tests
// ==========================================================================

func TestEngine_EventHandler_StateChanges(t *testing.T) {
	e := NewEngine()
	h := &mockEventHandler{}
	e.SetEventHandler(h)

	_ = e.Start(`{"server":"garbage"}`, 0, testEngineConfig())

	states := h.getStates()
	if len(states) < 2 {
		t.Fatalf("expected at least 2 state changes, got %d: %v", len(states), states)
	}
	if states[0] != StateConnecting {
		t.Fatalf("first state = %q, want %q", states[0], StateConnecting)
	}
	if states[len(states)-1] != StateDisconnected {
		t.Fatalf("last state = %q, want %q", states[len(states)-1], StateDisconnected)
	}
}

func TestEngine_EventHandler_OnError(t *testing.T) {
	e := NewEngine()
	h := &mockEventHandler{}
	e.SetEventHandler(h)

	_ = e.Start(`{"server":"garbage"}`, 0, testEngineConfig())

	errs := h.getErrors()
	if len(errs) == 0 {
		t.Fatal("expected OnError to be called")
	}
}

// ==========================================================================
// StatusJSON tests
// ==========================================================================

func TestEngine_StatusJSON_Disconnected(t *testing.T) {
	e := NewEngine()
	raw := e.StatusJSON()

	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("StatusJSON returned invalid JSON: %v", err)
	}
	if m["state"] != StateDisconnected {
		t.Fatalf("state = %v, want %q", m["state"], StateDisconnected)
	}
	for _, key := range []string{"connected_at", "uptime_seconds", "wire_url", "error"} {
		if _, ok := m[key]; ok {
			t.Fatalf("unexpected key %q in disconnected state", key)
		}
	}
}

func TestEngine_StatusJSON_AfterFailedStart(t *testing.T) {
	e := NewEngine()
	_ = e.Start(`{"server":"garbage"}`, 0, testEngineConfig())

	raw := e.StatusJSON()
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("StatusJSON returned invalid JSON: %v", err)
	}
	if m["state"] != StateDisconnected {
		t.Fatalf("state = %v, want %q", m["state"], StateDisconnected)
	}
	if _, ok := m["error"]; !ok {
		t.Fatal("expected 'error' key after failed start")
	}
	if m["error"] == "" {
		t.Fatal("error should be non-empty after failed start")
	}
}

// ==========================================================================
// EngineConfig tests
// ==========================================================================

func TestEngineConfig_NewEngineConfig(t *testing.T) {
	cfg := NewEngineConfig()
	if cfg == nil {
		t.Fatal("NewEngineConfig returned nil")
	}
	if cfg.CacheDir != "" {
		t.Fatalf("CacheDir = %q, want empty", cfg.CacheDir)
	}
	if cfg.LogDir != "" {
		t.Fatalf("LogDir = %q, want empty", cfg.LogDir)
	}
}

func TestEngine_Start_WithEngineConfig(t *testing.T) {
	configJSON := `{
		"server": "k2v5://udid:token@host:443",
		"mode": "tun",
		"rule": {"global": true}
	}`

	e := NewEngine()
	cfg := &EngineConfig{
		// Empty CacheDir: engine skips k2rule.Init (avoids blocking remote downloads).
		// Purpose: verify EngineConfig is accepted and JSON parsing succeeds.
		LogDir: t.TempDir(),
	}
	err := e.Start(configJSON, 0, cfg)
	if err != nil && strings.Contains(err.Error(), "parse config") {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if err != nil && strings.Contains(err.Error(), "EngineConfig is required") {
		t.Fatalf("unexpected EngineConfig error: %v", err)
	}
}

func TestAppextEngineStartInvalidJSON(t *testing.T) {
	e := NewEngine()
	err := e.Start("not json", 0, testEngineConfig())
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "parse config") {
		t.Fatalf("expected error containing 'parse config', got: %v", err)
	}
}

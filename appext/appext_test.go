package appext

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/kaitu-io/k2/config"
)

// ==========================================================================
// Mock helpers
// ==========================================================================

// mockEventHandler records OnStatus calls for assertion.
// Implements appext.EventHandler (OnStatus(string), OnStats).
type mockEventHandler struct {
	mu       sync.Mutex
	statuses []string // raw JSON from OnStatus
}

func (m *mockEventHandler) OnStatus(statusJSON string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.statuses = append(m.statuses, statusJSON)
}

func (m *mockEventHandler) OnStats(txBytes, rxBytes int64) {}

// getStates extracts state strings from recorded JSON statuses.
func (m *mockEventHandler) getStates() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var states []string
	for _, raw := range m.statuses {
		var s struct {
			State string `json:"state"`
		}
		if err := json.Unmarshal([]byte(raw), &s); err == nil {
			states = append(states, s.State)
		}
	}
	return states
}

// getErrors extracts error messages from recorded JSON statuses (non-nil errors only).
func (m *mockEventHandler) getErrors() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var errs []string
	for _, raw := range m.statuses {
		var s struct {
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(raw), &s); err == nil && s.Error != nil {
			errs = append(errs, s.Error.Message)
		}
	}
	return errs
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
	cfg := `{"routes":[{"via":"garbage","match":{"all":true}}]}`
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
	err := e.Start(`{"routes":[{"via":"garbage","match":{"all":true}}]}`, 0, nil)
	if err == nil {
		t.Fatal("Start with nil EngineConfig should return error")
	}
	if !strings.Contains(err.Error(), "EngineConfig is required") {
		t.Fatalf("expected 'EngineConfig is required' error, got: %v", err)
	}
}

func TestEngine_DoubleStart(t *testing.T) {
	e := NewEngine()
	cfg := `{"routes":[{"via":"k2v5://udid:token@127.0.0.1:12345?insecure=1","match":{"all":true}}]}`
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

// ==========================================================================
// EventHandler tests
// ==========================================================================

func TestEngine_EventHandler_StateChanges(t *testing.T) {
	e := NewEngine()
	h := &mockEventHandler{}
	e.SetEventHandler(h)

	_ = e.Start(`{"routes":[{"via":"garbage","match":{"all":true}}]}`, 0, testEngineConfig())

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

func TestEngine_EventHandler_Error(t *testing.T) {
	e := NewEngine()
	h := &mockEventHandler{}
	e.SetEventHandler(h)

	_ = e.Start(`{"routes":[{"via":"garbage","match":{"all":true}}]}`, 0, testEngineConfig())

	errs := h.getErrors()
	if len(errs) == 0 {
		t.Fatal("expected OnStatus with error to be called")
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
	for _, key := range []string{"startAt", "uptimeSeconds", "error"} {
		if _, ok := m[key]; ok {
			t.Fatalf("unexpected key %q in disconnected state", key)
		}
	}
}

func TestEngine_StatusJSON_AfterFailedStart(t *testing.T) {
	e := NewEngine()
	_ = e.Start(`{"routes":[{"via":"garbage","match":{"all":true}}]}`, 0, testEngineConfig())

	raw := e.StatusJSON()
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("StatusJSON returned invalid JSON: %v", err)
	}
	if m["state"] != StateDisconnected {
		t.Fatalf("state = %v, want %q", m["state"], StateDisconnected)
	}
	errRaw, ok := m["error"]
	if !ok {
		t.Fatal("expected 'error' key after failed start")
	}
	errObj, ok := errRaw.(map[string]any)
	if !ok {
		t.Fatalf("error should be an object, got %T", errRaw)
	}
	if errObj["message"] == "" {
		t.Fatal("error.message should be non-empty after failed start")
	}
}

// ==========================================================================
// EngineConfig tests
// ==========================================================================

func TestEngine_Start_WithEngineConfig(t *testing.T) {
	configJSON := `{
		"routes": [{"via": "k2v5://udid:token@host:443", "match": {"all": true}}],
		"mode": "tun"
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

// ==========================================================================
// PrefetchRules tests
// ==========================================================================

// Compile-time signature guard. The mobile plugin was broken in the past by
// treating PrefetchRules as if it took *EngineConfig — that forced callers
// through a nullable gomobile init, which broke iOS build. If anyone changes
// the signature back, this line fails to compile.
var _ func(string) = PrefetchRules

// TestPrefetchRules_EmptyCacheDirNoOp documents the early-return contract:
// callers on mobile pass an empty string when the platform container is
// unavailable, and the function must return without spawning a goroutine
// or panicking.
func TestPrefetchRules_EmptyCacheDirNoOp(t *testing.T) {
	// Must not panic, must not block, must not launch work. If this test
	// hangs, PrefetchRules is not honoring its early-return guard.
	PrefetchRules("")
}

// ==========================================================================
// App Bypass v2 wire test
// ==========================================================================

// TestResolveAppBypass mirrors daemon.ingestClientAppBypass behavior so the
// same JSON payload from webapp yields identical engine.Config on both
// the desktop (daemon) and mobile (appext) paths. The original P0 was that
// appext silently dropped ClientConfig.AppBypass on the floor, leaving
// Android with a non-working feature even though K2VpnService.kt exposes
// PackageResolver.InstallerForPackage and cn.yaml ships installer + package
// entries explicitly targeting Android.
func TestResolveAppBypass(t *testing.T) {
	tests := []struct {
		name        string
		configJSON  string
		wantRegion  string
		wantProcess []string
		wantPackage []string
	}{
		{
			name: "explicit region + custom adds",
			configJSON: `{
				"app_bypass": {
					"region": "CN",
					"process_adds": ["Steam.app"],
					"package_adds": ["com.example.foo"]
				}
			}`,
			wantRegion:  "cn",
			wantProcess: []string{"Steam.app"},
			wantPackage: []string{"com.example.foo"},
		},
		{
			name: "derive region from cn-access preset when region empty",
			configJSON: `{
				"routes": [
					{"via": "direct", "match": {"preset": "cn-access"}},
					{"via": "k2v5://example.com/", "match": {}}
				]
			}`,
			wantRegion: "cn",
		},
		{
			name: "explicit region wins over routes preset",
			configJSON: `{
				"app_bypass": {"region": "ir"},
				"routes": [{"via": "direct", "match": {"preset": "cn-access"}}]
			}`,
			wantRegion: "ir",
		},
		{
			name:       "global mode (no app_bypass, no preset) yields no region",
			configJSON: `{"routes": [{"via": "k2v5://example.com/", "match": {"all": true}}]}`,
			wantRegion: "",
		},
		{
			name: "custom-only (Plan C key fix: works without region)",
			configJSON: `{
				"app_bypass": {"process_adds": ["Steam.app"]}
			}`,
			wantRegion:  "",
			wantProcess: []string{"Steam.app"},
		},
		{
			name: "region whitespace + uppercase normalized",
			configJSON: `{
				"app_bypass": {"region": "  Ru  "}
			}`,
			wantRegion: "ru",
		},
		{
			name: "non-2-letter preset suffix ignored in derive",
			configJSON: `{
				"routes": [{"via": "direct", "match": {"preset": "overseas"}}]
			}`,
			wantRegion: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var c config.ClientConfig
			if err := json.Unmarshal([]byte(tc.configJSON), &c); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			region, custom := resolveAppBypass(&c)
			if region != tc.wantRegion {
				t.Errorf("region = %q, want %q", region, tc.wantRegion)
			}
			if !stringSlicesEqual(custom.ProcessAdds, tc.wantProcess) {
				t.Errorf("process = %v, want %v", custom.ProcessAdds, tc.wantProcess)
			}
			if !stringSlicesEqual(custom.PackageAdds, tc.wantPackage) {
				t.Errorf("package = %v, want %v", custom.PackageAdds, tc.wantPackage)
			}
		})
	}
}

func TestResolveAppBypass_NilSafe(t *testing.T) {
	region, custom := resolveAppBypass(nil)
	if region != "" || custom.ProcessAdds != nil || custom.PackageAdds != nil {
		t.Errorf("nil ClientConfig should yield zero values, got region=%q custom=%+v", region, custom)
	}
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i, s := range a {
		if s != b[i] {
			return false
		}
	}
	return true
}

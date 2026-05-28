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

	body := `{"action":"up","params":{"config":{"routes":[{"via":"k2v5://test:token@host:443","match":{"all":true}}]}}}`
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

	clientCfg := config.ClientFromURL("k2v5://test:token@host:443")

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

	clientCfg := config.ClientFromURL("k2v5://test:token@host:443")
	clientCfg.Tun = config.TunConfig{
		IPv4: "10.0.0.2/24",
		IPv6: "fd00::2/128",
	}

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
	clientCfg := config.ClientFromURL("k2v5://test:token@host:443")

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

	clientCfg := config.ClientFromURL("k2v5://test:token@host:443")
	clientCfg.Mode = "proxy"
	clientCfg.Tun = config.TunConfig{
		IPv4: "10.0.0.2/24",
	}
	clientCfg.Proxy.Listen = "127.0.0.1:8080"

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

	cfg := config.ClientFromURL("k2v5://test:token@host:443")
	cfg.Mode = "tun"

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
	// Server is derived (json:"-"), re-derive after deserialization.
	config.SetDefaults(loaded.Config)
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
	upBody := `{"action":"up","params":{"config":{"routes":[{"via":"k2v5://test:tok@host:443","match":{"all":true}}],"mode":"tun"}}}`
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

	cfgMap, ok := cfgRaw.(map[string]any)
	if !ok {
		t.Fatalf("expected config to be a map, got %T", cfgRaw)
	}
	// Server is derived (json:"-"), should NOT appear in JSON.
	if _, ok := cfgMap["server"]; ok {
		t.Fatalf("status config should not contain derived server field")
	}
	// Routes should contain the k2v5 URL.
	routesRaw, ok := cfgMap["routes"]
	if !ok {
		t.Fatal("expected routes field in status config")
	}
	routes, ok := routesRaw.([]any)
	if !ok || len(routes) == 0 {
		t.Fatalf("expected non-empty routes array, got %v", routesRaw)
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

	cfg := config.ClientFromURL("k2v5://test:token@host:443")

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
	// Server is derived (json:"-"), re-derive after deserialization.
	config.SetDefaults(loaded.Config)
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

// ==========================================================================
// doDown boundary condition tests (crash gate)
// ==========================================================================

func TestDoDownOnNilEngine(t *testing.T) {
	d := New()

	// Two doDown calls on a fresh daemon with no engine.
	d.doDown()
	d.doDown()

	if d.State() != engine.StateDisconnected {
		t.Fatalf("expected disconnected, got %s", d.State())
	}

	d.mu.RLock()
	eng := d.engine
	d.mu.RUnlock()
	if eng != nil {
		t.Fatal("expected engine to be nil on fresh daemon after doDown")
	}
}

func TestDoDownRapidSequential(t *testing.T) {
	d := New()
	d.StartFunc = mockStartFunc

	cfg := config.ClientFromURL("k2v5://test:tok@host:443")
	if err := d.doUp(cfg, 0); err != nil {
		t.Fatalf("doUp: %v", err)
	}
	if d.State() != engine.StateConnected {
		t.Fatalf("expected connected after doUp, got %s", d.State())
	}

	// Two rapid sequential doDown calls — reproduces the crash scenario.
	d.doDown()
	d.doDown()

	if d.State() != engine.StateDisconnected {
		t.Fatalf("expected disconnected, got %s", d.State())
	}

	d.mu.RLock()
	eng := d.engine
	d.mu.RUnlock()
	if eng != nil {
		t.Fatal("expected engine to be nil after rapid double doDown")
	}
}

func TestDoDownConcurrent(t *testing.T) {
	d := New()
	d.StartFunc = mockStartFunc

	cfg := config.ClientFromURL("k2v5://test:tok@host:443")
	if err := d.doUp(cfg, 0); err != nil {
		t.Fatalf("doUp: %v", err)
	}

	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			d.doDown()
		})
	}
	wg.Wait()

	if d.State() != engine.StateDisconnected {
		t.Fatalf("expected disconnected, got %s", d.State())
	}

	d.mu.RLock()
	eng := d.engine
	d.mu.RUnlock()
	if eng != nil {
		t.Fatal("expected engine to be nil after concurrent doDown")
	}
}

// ==========================================================================
// doUp teardown noise suppression — webapp UI flicker fix
//
// When a doUp is invoked while a previous engine is still active, the daemon
// internally calls closeTunnel → engine.Stop → OnStatus(disconnected). That
// transient disconnected state is an implementation artifact of the engine
// swap — externally, the doUp operation should look monotonic
// (connecting → connected/error), never reverting to disconnected mid-flight.
//
// Without suppression, webapp consumers (vpn-machine state machine) observe
// connected → disconnected → connecting → connected, briefly flashing through
// the "重连中" / "未连接" UI states even though no real disconnect happened.
//
// See docs/ analysis for the original 4-attempt trace from Windows VM testing.
// ==========================================================================

// TestDoUp_SuppressesTeardownDisconnectedExternally proves the bug: when a
// second doUp runs after a successful first connect, the daemon's externally
// observable state (d.State()) must not pass through "disconnected" between
// the closeTunnel of the old engine and the start of the new engine.
func TestDoUp_SuppressesTeardownDisconnectedExternally(t *testing.T) {
	d := New()

	var stateAtSecondStart string
	var startCount int
	d.StartFunc = func(eng *engine.Engine, cfg engine.Config) error {
		startCount++
		if startCount == 2 {
			// Snapshot state at the moment the SECOND engine is about to start.
			// By this point closeTunnel of the first engine has run; without
			// suppression d.State() would be "disconnected" (teardown noise).
			stateAtSecondStart = d.State()
		}
		eng.ForceConnected()
		return nil
	}

	cfg := config.ClientFromURL("k2v5://test:tok@host:443")

	if err := d.doUp(cfg, 0); err != nil {
		t.Fatalf("first doUp: %v", err)
	}
	if d.State() != engine.StateConnected {
		t.Fatalf("after first doUp: state=%q, want %q", d.State(), engine.StateConnected)
	}

	if err := d.doUp(cfg, 0); err != nil {
		t.Fatalf("second doUp: %v", err)
	}

	if stateAtSecondStart == engine.StateDisconnected {
		t.Fatalf("teardown noise leaked externally: state=%q at second StartFunc invocation; "+
			"doUp must pre-fire %q before closeTunnel so consumers see monotonic "+
			"connecting → connected", stateAtSecondStart, engine.StateConnecting)
	}
	if stateAtSecondStart != engine.StateConnecting {
		t.Fatalf("state at second StartFunc invocation: got %q, want %q",
			stateAtSecondStart, engine.StateConnecting)
	}
}

// TestOnStatus_TeardownFilter_PreservesErrors verifies the suppression is
// gated on Error == nil. Real engine failures (e.fail with classified error)
// must always propagate, even if they hypothetically arrive during the
// closeTunnel window.
func TestOnStatus_TeardownFilter_PreservesErrors(t *testing.T) {
	d := New()
	d.inDoUpTeardown.Add(1)

	d.OnStatus(engine.Status{
		State: engine.StateDisconnected,
		Error: &engine.EngineError{Code: 503, Category: "server", Message: "test failure"},
	})

	if d.State() != engine.StateDisconnected {
		t.Fatalf("error status was suppressed: state=%q, want %q",
			d.State(), engine.StateDisconnected)
	}
	info := d.statusInfo()
	if info["error"] == nil {
		t.Fatal("error field cleared despite Error != nil — filter is too aggressive")
	}
}

// TestOnStatus_TeardownFilter_SuppressesCleanDisconnected verifies the
// positive case: clean disconnected (no error) during the teardown window
// must be swallowed so external observers see no spurious state regression.
func TestOnStatus_TeardownFilter_SuppressesCleanDisconnected(t *testing.T) {
	d := New()

	// Baseline: simulate doUp pre-firing "connecting".
	d.OnStatus(engine.Status{State: engine.StateConnecting})
	if d.State() != engine.StateConnecting {
		t.Fatalf("baseline: state=%q, want %q", d.State(), engine.StateConnecting)
	}

	d.inDoUpTeardown.Add(1)
	d.OnStatus(engine.Status{State: engine.StateDisconnected}) // teardown noise

	if d.State() != engine.StateConnecting {
		t.Fatalf("teardown noise leaked: state=%q, want %q (filter should suppress)",
			d.State(), engine.StateConnecting)
	}
}

// TestOnStatus_OutsideTeardown_PassesAllEvents verifies the filter is inert
// when inDoUpTeardown counter is zero. Disconnected events must propagate
// normally outside the teardown window.
func TestOnStatus_OutsideTeardown_PassesAllEvents(t *testing.T) {
	d := New()
	// inDoUpTeardown stays 0 (zero value).

	d.OnStatus(engine.Status{State: engine.StateConnected})
	if d.State() != engine.StateConnected {
		t.Fatalf("connected event lost: state=%q", d.State())
	}

	d.OnStatus(engine.Status{State: engine.StateDisconnected})
	if d.State() != engine.StateDisconnected {
		t.Fatalf("disconnected event lost outside teardown window: state=%q", d.State())
	}
}

// TestTeardownFilter_CounterSemantics is the regression test for the
// concurrent-doUp race. With atomic.Bool, doUp #1's deferred Store(false)
// would stomp doUp #2's Store(true), causing doUp #2's closeTunnel emission
// to leak. With atomic.Int32, each doUp manages its own +1/-1 contribution.
//
// We don't try to drive the race deterministically — instead we assert the
// invariant that holds the fix together: when two pre-fires arm the counter
// and only one decrements, the filter must remain active.
func TestTeardownFilter_CounterSemantics(t *testing.T) {
	d := New()

	// Two concurrent doUp prologues both arm the counter.
	d.inDoUpTeardown.Add(1)
	d.inDoUpTeardown.Add(1)
	if got := d.inDoUpTeardown.Load(); got != 2 {
		t.Fatalf("after two Add(+1): counter=%d, want 2", got)
	}

	// One of them decrements (e.g., doUp #1's deferred clear). Filter must
	// stay active because doUp #2 still needs it for its closeTunnel.
	d.inDoUpTeardown.Add(-1)
	if got := d.inDoUpTeardown.Load(); got != 1 {
		t.Fatalf("after one Add(-1): counter=%d, want 1", got)
	}

	// Filter must still suppress clean disconnected.
	d.OnStatus(engine.Status{State: engine.StateConnecting})
	d.OnStatus(engine.Status{State: engine.StateDisconnected})
	if d.State() != engine.StateConnecting {
		t.Fatalf("filter not active despite counter=1: state=%q", d.State())
	}

	// Second decrement brings counter to 0 — filter goes inert.
	d.inDoUpTeardown.Add(-1)
	if got := d.inDoUpTeardown.Load(); got != 0 {
		t.Fatalf("after second Add(-1): counter=%d, want 0", got)
	}
	d.OnStatus(engine.Status{State: engine.StateDisconnected})
	if d.State() != engine.StateDisconnected {
		t.Fatalf("filter still active despite counter=0: state=%q", d.State())
	}
}

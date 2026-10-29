package daemon

import (
	"fmt"
	"testing"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/engine"
)

// ==========================================================================
// Network monitor adapter tests (T2)
// ==========================================================================

func TestNewNetworkMonitor_ReturnsAdapter(t *testing.T) {
	adapter, ifaceMon, err := NewNetworkMonitor()
	if err != nil {
		// Some platforms (e.g., containers, CI) may not support route sockets.
		t.Skipf("NewNetworkMonitor not supported on this platform: %v", err)
	}
	defer adapter.Close()

	if adapter == nil {
		t.Fatal("expected non-nil adapter")
	}
	if ifaceMon == nil {
		t.Fatal("expected non-nil interfaceMonitor")
	}
}

func TestDaemon_EngineConfig_IncludesMonitor(t *testing.T) {
	var captured engine.Config
	d := New()
	d.StartFunc = func(eng *engine.Engine, cfg engine.Config) error {
		captured = cfg
		eng.ForceConnected()
		return nil
	}

	// Inject a mock monitor factory.
	mockMon := &mockMonitor{}
	d.MonitorFactory = func() (engine.NetworkChangeNotifier, any, error) {
		return mockMon, "fake-iface-mon", nil
	}

	cfg := config.ClientFromURL("k2v5://test:tok@host:443")
	if err := d.doUp(cfg, 0); err != nil {
		t.Fatalf("doUp: %v", err)
	}

	if captured.NetworkMonitor == nil {
		t.Fatal("expected NetworkMonitor to be set")
	}
	if captured.InterfaceMonitor == nil {
		t.Fatal("expected InterfaceMonitor to be set")
	}
}

func TestDaemon_MonitorFactory_Failure_NonFatal(t *testing.T) {
	var captured engine.Config
	d := New()
	d.StartFunc = func(eng *engine.Engine, cfg engine.Config) error {
		captured = cfg
		eng.ForceConnected()
		return nil
	}

	// Factory returns error — doUp should still succeed.
	d.MonitorFactory = func() (engine.NetworkChangeNotifier, any, error) {
		return nil, nil, fmt.Errorf("no route socket")
	}

	cfg := config.ClientFromURL("k2v5://test:tok@host:443")
	if err := d.doUp(cfg, 0); err != nil {
		t.Fatalf("doUp should succeed even when monitor fails: %v", err)
	}
	if d.State() != engine.StateConnected {
		t.Fatalf("expected connected, got %s", d.State())
	}
	if captured.NetworkMonitor != nil {
		t.Fatal("expected nil NetworkMonitor when factory fails")
	}
}

// mockMonitor implements engine.NetworkChangeNotifier for testing.
type mockMonitor struct{}

func (m *mockMonitor) Start(callback func()) error { return nil }
func (m *mockMonitor) Close() error                { return nil }

package center

import (
	"testing"
)

func TestSlaveTunnelHasRelayAndHasTunnelFields(t *testing.T) {
	tunnel := SlaveTunnel{
		Domain:    "test.example.com",
		HasRelay:  BoolPtr(true),
		HasTunnel: BoolPtr(true),
	}

	if tunnel.HasRelay == nil || !*tunnel.HasRelay {
		t.Errorf("Expected HasRelay to be true, got %v", tunnel.HasRelay)
	}
	if tunnel.HasTunnel == nil || !*tunnel.HasTunnel {
		t.Errorf("Expected HasTunnel to be true, got %v", tunnel.HasTunnel)
	}
}

func TestSlaveTunnelDefaultValues(t *testing.T) {
	tunnel := SlaveTunnel{
		Domain: "test.example.com",
	}

	if tunnel.HasRelay != nil {
		t.Errorf("Expected HasRelay to be nil by default, got %v", tunnel.HasRelay)
	}
	if tunnel.HasTunnel != nil {
		t.Errorf("Expected HasTunnel to be nil by default, got %v", tunnel.HasTunnel)
	}
}

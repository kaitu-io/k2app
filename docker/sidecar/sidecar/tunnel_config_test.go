package sidecar

import (
	"testing"
)

func TestTunnelConfigHasCapabilityFields(t *testing.T) {
	config := TunnelConfig{
		Domain:    "test.example.com",
		Port:      443,
		HasRelay:  true,
		HasTunnel: true,
	}

	if !config.HasRelay {
		t.Errorf("Expected HasRelay to be true, got %v", config.HasRelay)
	}
	if !config.HasTunnel {
		t.Errorf("Expected HasTunnel to be true, got %v", config.HasTunnel)
	}
}

func TestTunnelConfigDefaultCapabilityValues(t *testing.T) {
	config := TunnelConfig{
		Domain: "test.example.com",
		Port:   443,
	}

	// Default values should be false (zero value for bool)
	if config.HasRelay {
		t.Errorf("Expected HasRelay to be false by default, got %v", config.HasRelay)
	}
	if config.HasTunnel {
		t.Errorf("Expected HasTunnel to be false by default, got %v", config.HasTunnel)
	}
}

func TestTunnelResultHasCapabilityFields(t *testing.T) {
	result := TunnelResult{
		Domain:    "test.example.com",
		Port:      443,
		HasRelay:  true,
		HasTunnel: true,
	}

	if !result.HasRelay {
		t.Errorf("Expected HasRelay to be true, got %v", result.HasRelay)
	}
	if !result.HasTunnel {
		t.Errorf("Expected HasTunnel to be true, got %v", result.HasTunnel)
	}
}

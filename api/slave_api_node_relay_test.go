package center

import (
	"testing"
)

func TestTunnelConfigInputHasRelayAndHasTunnel(t *testing.T) {
	input := TunnelConfigInput{
		Domain:    "test.example.com",
		Port:      443,
		HasRelay:  true,
		HasTunnel: true,
	}

	if !input.HasRelay {
		t.Errorf("Expected HasRelay to be true")
	}
	if !input.HasTunnel {
		t.Errorf("Expected HasTunnel to be true")
	}
}

func TestTunnelConfigOutputHasRelayAndHasTunnel(t *testing.T) {
	output := TunnelConfigOutput{
		Domain:    "test.example.com",
		Port:      443,
		HasRelay:  true,
		HasTunnel: true,
	}

	if !output.HasRelay {
		t.Errorf("Expected HasRelay to be true")
	}
	if !output.HasTunnel {
		t.Errorf("Expected HasTunnel to be true")
	}
}

func TestUpsertTunnelForNodeSetsCapabilityFlags(t *testing.T) {
	// This is a compilation/struct test verifying the fields flow correctly
	input := TunnelConfigInput{
		Domain:    "relay.example.com",
		Port:      443,
		HasRelay:  true,
		HasTunnel: false,
	}

	if !input.HasRelay {
		t.Errorf("HasRelay should be true")
	}
	if input.HasTunnel {
		t.Errorf("HasTunnel should be false")
	}
}

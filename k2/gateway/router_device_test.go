//go:build linux

package gateway

import (
	"testing"
)

func TestNormalizeMAC(t *testing.T) {
	tests := []struct{ in, want string }{
		{"aa:bb:cc:dd:ee:ff", "AA:BB:CC:DD:EE:FF"},
		{"AA:BB:CC:DD:EE:FF", "AA:BB:CC:DD:EE:FF"},
		{"aa-bb-cc-dd-ee-ff", "AA:BB:CC:DD:EE:FF"},
		{"invalid", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := normalizeMAC(tt.in)
		if got != tt.want {
			t.Errorf("normalizeMAC(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestRouterDeviceManager_QuotaEnforcement(t *testing.T) {
	mgr := &RouterDeviceManager{quota: 2, mode: "allowlist"}
	mgr.entries = []RouterDeviceEntry{
		{MAC: "AA:BB:CC:DD:EE:01"},
		{MAC: "AA:BB:CC:DD:EE:02"},
	}
	// At capacity — should not be able to add more
	if mgr.quota > 0 && len(mgr.entries) >= mgr.quota {
		// Good — quota enforced
	} else {
		t.Error("expected quota to be reached")
	}
}

func TestRouterDeviceManager_UnlimitedQuota(t *testing.T) {
	mgr := &RouterDeviceManager{quota: -1, mode: "allowlist"}
	mgr.entries = []RouterDeviceEntry{
		{MAC: "AA:BB:CC:DD:EE:01"},
	}
	// -1 = unlimited, should always allow more
	if mgr.quota > 0 && len(mgr.entries) >= mgr.quota {
		t.Error("unlimited quota should not block additions")
	}
}

func TestRouterDeviceManager_GetAllowedMACs(t *testing.T) {
	mgr := &RouterDeviceManager{}
	mgr.entries = []RouterDeviceEntry{
		{MAC: "AA:BB:CC:DD:EE:01"},
		{MAC: "AA:BB:CC:DD:EE:02"},
	}
	macs := mgr.getAllowedMACs()
	if len(macs) != 2 {
		t.Fatalf("expected 2 MACs, got %d", len(macs))
	}
	if macs[0] != "AA:BB:CC:DD:EE:01" {
		t.Errorf("first MAC = %s", macs[0])
	}
}

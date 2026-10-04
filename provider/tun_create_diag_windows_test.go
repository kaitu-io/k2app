//go:build windows

package provider

import (
	"regexp"
	"testing"
)

// Exercises the real SetupAPI/CfgMgr calls — the layer the fake enumerator in
// tun_create_diag_test.go replaces. A CI runner normally has no Wintun
// adapter, so this proves the calls are wired and don't fault, not that a
// failing device is decoded; that was checked by hand on a VM with a disabled
// k2tun adapter (problem=22(DISABLED)).
func TestEnumerateWintunDevices_RealSetupAPI(t *testing.T) {
	devs, err := enumerateWintunDevices()
	if err != nil {
		// An enumerator never registered on this box can make SetupAPI
		// refuse the query; that is a report-worthy sampleErr, not a fault.
		t.Logf("enumerate: %v", err)
	}
	for _, d := range devs {
		t.Logf("wintun device: %s", formatWintunDevice(d))
		if d.InstanceID == "" {
			t.Errorf("device without instance ID: %+v", d)
		}
	}
}

func TestWindowsBuildString(t *testing.T) {
	if s := windowsBuildString(); !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(s) {
		t.Errorf("windowsBuildString() = %q", s)
	}
}

package k2p

import (
	"net"
	"testing"
)

func TestDetectPrivateNetworks(t *testing.T) {
	cidrs, dns := DetectPrivateNetworks()
	if len(cidrs) == 0 {
		t.Skip("no private networks found (CI environment?)")
	}
	t.Logf("networks: %v, dns: %s", cidrs, dns)
	for _, c := range cidrs {
		if _, _, err := net.ParseCIDR(c); err != nil {
			t.Errorf("invalid CIDR %q: %v", c, err)
		}
	}
	if dns == "" {
		t.Error("expected non-empty DNS guess")
	}
}

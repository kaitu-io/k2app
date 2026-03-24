package k2p2p

import (
	"strings"
	"testing"
)

func TestDefaultSTUNServers_HasMinimumCount(t *testing.T) {
	servers := DefaultSTUNServers()
	if len(servers) < 5 {
		t.Errorf("expected at least 5 default STUN servers, got %d", len(servers))
	}
}

func TestDefaultSTUNServers_AllHavePort(t *testing.T) {
	for _, s := range DefaultSTUNServers() {
		if !strings.Contains(s, ":") {
			t.Errorf("server %q missing port", s)
		}
	}
}

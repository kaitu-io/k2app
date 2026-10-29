//go:build linux

package gateway

import "testing"

func TestIsVirtualInterface(t *testing.T) {
	tests := []struct {
		name   string
		expect bool
	}{
		{"eth0", false},
		{"enp3s0", false},
		{"wlan0", false},
		{"br-lan", false},
		{"docker0", true},
		{"docker1", true},
		{"veth1234", true},
		{"vethabcd", true},
		{"tun0", true},
		{"tun1", true},
		{"tap0", true},
		{"virbr0", true},
		{"lo", true},
	}
	for _, tt := range tests {
		if got := isVirtualInterface(tt.name); got != tt.expect {
			t.Errorf("isVirtualInterface(%q) = %v, want %v", tt.name, got, tt.expect)
		}
	}
}

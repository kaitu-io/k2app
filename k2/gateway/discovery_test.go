//go:build linux

package gateway

import (
	"os"
	"testing"
)

func TestParseDnsmasqLeases(t *testing.T) {
	content := "1712600000 aa:bb:cc:dd:ee:ff 192.168.1.100 iPhone *\n" +
		"1712600000 11:22:33:44:55:66 192.168.1.101 laptop client-id\n" +
		"1712600000 AA:BB:CC:DD:EE:11 192.168.1.102 * *\n"
	tmpFile := t.TempDir() + "/leases"
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	devices := discoverViaDnsmasqLeases(tmpFile)
	if len(devices) != 3 {
		t.Fatalf("expected 3 devices, got %d", len(devices))
	}
	if devices[0].MAC != "AA:BB:CC:DD:EE:FF" {
		t.Errorf("MAC should be uppercased, got %s", devices[0].MAC)
	}
	if devices[0].Hostname != "iPhone" {
		t.Errorf("hostname = %s, want iPhone", devices[0].Hostname)
	}
	if devices[2].Hostname != "" {
		t.Errorf("* hostname should be empty, got %s", devices[2].Hostname)
	}
}

func TestFilterBySubnets(t *testing.T) {
	devices := []LanDevice{
		{MAC: "AA:BB:CC:DD:EE:FF", IP: "192.168.1.100"},
		{MAC: "11:22:33:44:55:66", IP: "10.0.0.50"},
		{MAC: "77:88:99:AA:BB:CC", IP: "192.168.2.200"},
	}
	filtered := filterBySubnets(devices, []string{"192.168.1.0/24"})
	if len(filtered) != 1 {
		t.Fatalf("expected 1 device in 192.168.1.0/24, got %d", len(filtered))
	}
	if filtered[0].IP != "192.168.1.100" {
		t.Errorf("wrong device: %s", filtered[0].IP)
	}
}

func TestFilterBySubnets_Empty(t *testing.T) {
	devices := []LanDevice{{MAC: "AA:BB:CC:DD:EE:FF", IP: "192.168.1.100"}}
	filtered := filterBySubnets(devices, nil)
	if len(filtered) != 1 {
		t.Error("empty subnets should return all devices")
	}
}

func TestFilterBySubnets_MultipleNets(t *testing.T) {
	devices := []LanDevice{
		{MAC: "AA:BB:CC:DD:EE:FF", IP: "192.168.1.100"},
		{MAC: "11:22:33:44:55:66", IP: "10.0.0.50"},
		{MAC: "77:88:99:AA:BB:CC", IP: "172.16.0.1"},
	}
	filtered := filterBySubnets(devices, []string{"192.168.1.0/24", "10.0.0.0/8"})
	if len(filtered) != 2 {
		t.Fatalf("expected 2 devices, got %d", len(filtered))
	}
}

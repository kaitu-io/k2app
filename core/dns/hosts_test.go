package dns

import (
	"os"
	"testing"
)

func TestHostsFile_Lookup(t *testing.T) {
	// Create temporary hosts file
	content := `
# Test hosts file
127.0.0.1 localhost
127.0.0.1 localhost.localdomain
192.168.1.1 test.local alias1.local alias2.local
# Comment line
	# Indented comment
192.168.1.2   example.com   www.example.com

::1 localhost
2001:db8::1 ipv6.example.com
`

	tmpfile, err := os.CreateTemp("", "hosts-test-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.WriteString(content); err != nil {
		t.Fatal(err)
	}
	tmpfile.Close()

	// Load hosts file
	hosts := NewHostsFile()
	if err := hosts.Load(tmpfile.Name()); err != nil {
		t.Fatalf("Failed to load hosts file: %v", err)
	}

	// Test lookups
	tests := []struct {
		domain   string
		expected bool
		numIPs   int
	}{
		{"localhost", true, 2},           // IPv4 and IPv6
		{"test.local", true, 1},          // Single IPv4
		{"alias1.local", true, 1},        // Alias
		{"example.com", true, 1},         // IPv4
		{"www.example.com", true, 1},     // Alias
		{"ipv6.example.com", true, 1},    // IPv6
		{"notfound.com", false, 0},       // Not in hosts
		{"LOCALHOST", true, 2},           // Case insensitive
		{"TEST.LOCAL", true, 1},          // Case insensitive
	}

	for _, tt := range tests {
		ips, found := hosts.Lookup(tt.domain)
		if found != tt.expected {
			t.Errorf("Lookup(%q) found=%v, want %v", tt.domain, found, tt.expected)
			continue
		}
		if found && len(ips) != tt.numIPs {
			t.Errorf("Lookup(%q) returned %d IPs, want %d", tt.domain, len(ips), tt.numIPs)
		}
	}
}

func TestHostsFile_Size(t *testing.T) {
	content := `
127.0.0.1 localhost
192.168.1.1 test1.local test2.local
192.168.1.2 test3.local
`

	tmpfile, err := os.CreateTemp("", "hosts-test-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.WriteString(content); err != nil {
		t.Fatal(err)
	}
	tmpfile.Close()

	hosts := NewHostsFile()
	if err := hosts.Load(tmpfile.Name()); err != nil {
		t.Fatalf("Failed to load hosts file: %v", err)
	}

	// Should have 4 entries: localhost, test1.local, test2.local, test3.local
	if size := hosts.Size(); size != 4 {
		t.Errorf("Size() = %d, want 4", size)
	}
}


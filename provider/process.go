package provider

import "net/netip"

// ProcessSearcher finds the process that owns a network connection.
// Platform-specific implementations look up the process by source address.
type ProcessSearcher interface {
	FindProcess(network string, srcIP netip.Addr, srcPort uint16) (processName string, packageName string)
}

// NopProcessSearcher returns empty strings for all lookups.
// Used when platform doesn't support process detection.
type NopProcessSearcher struct{}

func (NopProcessSearcher) FindProcess(string, netip.Addr, uint16) (string, string) {
	return "", ""
}

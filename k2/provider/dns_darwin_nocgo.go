//go:build darwin && !ios && !cgo

package provider

// Stubs for cross-compilation with CGO_ENABLED=0 (CI builds).
// The real implementation is in dns_darwin.go (requires cgo for SystemConfiguration).

func CleanupStaleDNSOverride() {}

func saveDNSState(ifaceName, dnsServer string) {}
func removeDNSState()                          {}

type dnsOverride struct{}

func newDNSOverride(tunDNS string) *dnsOverride { return nil }
func (d *dnsOverride) set() error               { return nil }
func (d *dnsOverride) monitor()                 {}
func (d *dnsOverride) stop()                    {}

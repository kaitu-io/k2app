//go:build (!darwin && !windows) || ios

package provider

// CleanupStaleDNSOverride is a no-op on Linux/iOS.
func CleanupStaleDNSOverride() {}

func saveDNSState(ifaceName, dnsServer string) {}
func removeDNSState()                          {}

// dnsOverride is a no-op on Linux/iOS.
// Linux: sing-tun sets DNS via resolvectl.
type dnsOverride struct{}

func newDNSOverride(tunDNS string) *dnsOverride { return nil }
func (d *dnsOverride) set() error               { return nil }
func (d *dnsOverride) monitor()                 {}
func (d *dnsOverride) stop()                    {}

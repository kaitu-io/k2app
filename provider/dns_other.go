//go:build !darwin || ios

package provider

// dnsOverride is a no-op on non-macOS platforms.
// Linux: sing-tun sets DNS via resolvectl.
// Windows: sing-tun sets DNS via luid.SetDNS.
type dnsOverride struct{}

func newDNSOverride(tunDNS string) *dnsOverride { return nil }
func (d *dnsOverride) set() error               { return nil }
func (d *dnsOverride) monitor()                 {}
func (d *dnsOverride) stop()                    {}

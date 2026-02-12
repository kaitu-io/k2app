package directdial

import "net"

// detectDefault finds the default physical (non-loopback, non-TUN, UP) interface.
func detectDefault() (name string, idx int) {
	// 1. Try platform-preferred names first.
	for _, preferred := range []string{"en0", "eth0", "ens0", "enp0s1"} {
		iface, err := net.InterfaceByName(preferred)
		if err != nil {
			continue
		}
		if isUsableInterface(iface) {
			return iface.Name, iface.Index
		}
	}

	// 2. Fallback: enumerate all interfaces, pick first usable one.
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", 0
	}
	for _, iface := range ifaces {
		if isUsableInterface(&iface) {
			return iface.Name, iface.Index
		}
	}

	return "", 0
}

// isUsableInterface returns true for interfaces that are UP, not loopback,
// not TUN/WireGuard, and have at least one unicast address.
func isUsableInterface(iface *net.Interface) bool {
	if iface.Flags&net.FlagUp == 0 {
		return false
	}
	if iface.Flags&net.FlagLoopback != 0 {
		return false
	}
	// Skip TUN and WireGuard interfaces.
	name := iface.Name
	for _, prefix := range []string{"utun", "tun", "wg", "docker", "br-", "veth"} {
		if len(name) >= len(prefix) && name[:len(prefix)] == prefix {
			return false
		}
	}
	// Must have at least one unicast address.
	addrs, err := iface.Addrs()
	if err != nil || len(addrs) == 0 {
		return false
	}
	return true
}

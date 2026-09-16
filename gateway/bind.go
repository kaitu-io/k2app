//go:build linux

package gateway

import (
	"net"
	"strconv"
)

// ifaceAddrs decouples net.Interface enumeration from address resolution so
// resolveLANBindAddrs is unit-testable with injected stubs.
type ifaceAddrs struct {
	Name  string
	Addrs []net.Addr
}

// enumerateIfaceAddrs lists UP interfaces and their addresses from the kernel.
func enumerateIfaceAddrs() []ifaceAddrs {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []ifaceAddrs
	for _, in := range ifaces {
		if in.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, _ := in.Addrs()
		out = append(out, ifaceAddrs{Name: in.Name, Addrs: addrs})
	}
	return out
}

// resolveLANBindAddrs returns the "ip:port" list the panel HTTP API should
// bind to: loopback (127.0.0.1) plus every private (RFC1918 / ULA) interface
// IP. Public/WAN IPs are excluded so the control plane is never exposed to the
// internet; virtual interfaces (docker/veth/tun/tap/virbr/lo) are skipped.
func resolveLANBindAddrs(ifaces []ifaceAddrs, port int) []string {
	seen := map[string]bool{}
	var out []string
	add := func(ip string) {
		// JoinHostPort brackets IPv6 literals; a bare "%s:%d" produced
		// "fd1a:...::1:1779" and every LAN ULA bind failed with
		// "too many colons in address" (seen on OpenWrt 25.12).
		a := net.JoinHostPort(ip, strconv.Itoa(port))
		if !seen[a] {
			seen[a] = true
			out = append(out, a)
		}
	}
	add("127.0.0.1")
	for _, f := range ifaces {
		if isVirtualInterface(f.Name) {
			continue
		}
		for _, addr := range f.Addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipnet.IP
			if ip.To4() == nil && ip.To16() == nil {
				continue
			}
			if ip.IsLoopback() {
				continue
			}
			if !ip.IsPrivate() {
				continue
			}
			add(ip.String())
		}
	}
	return out
}

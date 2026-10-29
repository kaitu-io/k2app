//go:build linux

package gateway

import (
	"errors"
	"net"
	"strings"
)

func DiscoverLANSubnets() ([]string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var subnets []string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		if isVirtualInterface(iface.Name) {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			if !ipNet.IP.IsPrivate() {
				continue
			}
			subnets = append(subnets, ipNet.String())
		}
	}
	if len(subnets) == 0 {
		return nil, errors.New("gateway: no private LAN subnets found")
	}
	return subnets, nil
}

func isVirtualInterface(name string) bool {
	prefixes := []string{"docker", "veth", "tun", "tap", "virbr", "lo"}
	for _, p := range prefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

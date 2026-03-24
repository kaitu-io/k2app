package k2p

import "net"

// DetectPrivateNetworks returns the host's private IPv4 LAN CIDRs and a DNS
// guess (first IP in the first private subnet, typically the gateway).
func DetectPrivateNetworks() (cidrs []string, dns string) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok || !ipnet.IP.IsPrivate() {
				continue
			}
			// Skip IPv6 link-local.
			if ipnet.IP.To4() == nil {
				continue
			}
			cidrs = append(cidrs, ipnet.String())
			if dns == "" {
				gw := make(net.IP, 4)
				copy(gw, ipnet.IP.To4())
				gw[3] = 1
				dns = gw.String()
			}
		}
	}
	return cidrs, dns
}

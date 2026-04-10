//go:build linux

package gateway

import (
	"bufio"
	"encoding/json"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"strings"
)

// LanDevice represents a device discovered on the LAN.
type LanDevice struct {
	MAC      string `json:"mac"`
	IP       string `json:"ip"`
	Hostname string `json:"hostname"`
	Online   bool   `json:"online"`
}

// discoverLANDevices returns devices on the LAN using platform-specific methods.
// Priority: ubus (OpenWrt) → dnsmasq leases → ip neigh
func discoverLANDevices(lanSubnets []string) []LanDevice {
	if hasUbus() {
		devices := discoverViaUbus()
		if len(devices) > 0 {
			return filterBySubnets(devices, lanSubnets)
		}
	}
	if path := findDnsmasqLeases(); path != "" {
		devices := discoverViaDnsmasqLeases(path)
		if len(devices) > 0 {
			return filterBySubnets(devices, lanSubnets)
		}
	}
	return filterBySubnets(discoverViaIPNeigh(), lanSubnets)
}

func hasUbus() bool {
	_, err := exec.LookPath("ubus")
	return err == nil
}

// discoverViaUbus uses OpenWrt's ubus to get DHCP lease info.
// Format: {"device":{"br-lan":{"leases":[{"hostname":"iPhone","mac":"AA:BB:CC:DD:EE:FF","ip":"192.168.1.100","expire":12345}]}}}
func discoverViaUbus() []LanDevice {
	out, err := exec.Command("ubus", "call", "dhcp", "ipv4leases").Output()
	if err != nil {
		slog.Debug("gateway: ubus dhcp query failed", "err", err)
		return nil
	}
	var resp struct {
		Device map[string]struct {
			Leases []struct {
				Hostname string `json:"hostname"`
				MAC      string `json:"mac"`
				IP       string `json:"ip"`
				Expire   int64  `json:"expire"`
			} `json:"leases"`
		} `json:"device"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		slog.Debug("gateway: ubus parse failed", "err", err)
		return nil
	}
	var devices []LanDevice
	for _, iface := range resp.Device {
		for _, lease := range iface.Leases {
			devices = append(devices, LanDevice{
				MAC:      strings.ToUpper(lease.MAC),
				IP:       lease.IP,
				Hostname: lease.Hostname,
				Online:   true,
			})
		}
	}
	return devices
}

// findDnsmasqLeases checks common lease file locations.
func findDnsmasqLeases() string {
	paths := []string{
		"/tmp/dhcp.leases",             // OpenWrt
		"/var/lib/misc/dnsmasq.leases", // Debian/Ubuntu
		"/var/lib/dnsmasq/dnsmasq.leases",
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// discoverViaDnsmasqLeases parses dnsmasq lease file.
// Format: <expire_time> <mac> <ip> <hostname> <client_id>
func discoverViaDnsmasqLeases(path string) []LanDevice {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var devices []LanDevice
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 4 {
			continue
		}
		hostname := fields[3]
		if hostname == "*" {
			hostname = ""
		}
		devices = append(devices, LanDevice{
			MAC:      strings.ToUpper(fields[1]),
			IP:       fields[2],
			Hostname: hostname,
			Online:   true,
		})
	}
	return devices
}

// discoverViaIPNeigh parses `ip neigh show` output.
// Format: 192.168.1.100 dev br-lan lladdr aa:bb:cc:dd:ee:ff REACHABLE
func discoverViaIPNeigh() []LanDevice {
	out, err := exec.Command("ip", "neigh", "show").Output()
	if err != nil {
		return nil
	}
	var devices []LanDevice
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 5 {
			continue
		}
		ip := fields[0]
		macIdx := -1
		for i, f := range fields {
			if f == "lladdr" && i+1 < len(fields) {
				macIdx = i + 1
				break
			}
		}
		if macIdx < 0 {
			continue
		}
		mac := strings.ToUpper(fields[macIdx])
		state := fields[len(fields)-1]
		online := state == "REACHABLE" || state == "DELAY" || state == "STALE" || state == "PROBE"
		devices = append(devices, LanDevice{
			MAC:      mac,
			IP:       ip,
			Hostname: "",
			Online:   online,
		})
	}
	return devices
}

// filterBySubnets filters devices to only those in the configured LAN subnets.
func filterBySubnets(devices []LanDevice, subnets []string) []LanDevice {
	if len(subnets) == 0 {
		return devices
	}
	var nets []*net.IPNet
	for _, s := range subnets {
		_, ipNet, err := net.ParseCIDR(s)
		if err == nil {
			nets = append(nets, ipNet)
		}
	}
	var filtered []LanDevice
	for _, d := range devices {
		ip := net.ParseIP(d.IP)
		if ip == nil {
			continue
		}
		for _, n := range nets {
			if n.Contains(ip) {
				filtered = append(filtered, d)
				break
			}
		}
	}
	return filtered
}

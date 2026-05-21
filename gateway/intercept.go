//go:build linux

package gateway

import (
	"fmt"
	"net"
	"os/exec"
	"strings"
)

// Interceptor installs/removes firewall rules that redirect LAN traffic
// to a local TPROXY listener.
type Interceptor interface {
	Install(cfg InterceptConfig) error
	Remove()
	Name() string
}

// InterceptConfig describes what traffic to intercept.
type InterceptConfig struct {
	ListenPort int
	LANSubnets []string
	ExcludeIPs []string
	// Router features
	DNSRedirect   bool     // redirect port 53 to DNSPort
	DNSPort       int      // k2r DNS resolver port (from engine config)
	AllowlistMode bool     // true = only AllowedMACs can access proxy
	AllowedMACs   []string // MAC addresses in allowlist (e.g., "AA:BB:CC:DD:EE:FF")
}

// MACManager is an optional interface for interceptors that support live MAC set updates.
// Supported by nftNetlinkInterceptor (netlink set operations).
// Not supported by shell-based or iptables backends (require full reinstall).
type MACManager interface {
	AddMAC(mac string) error
	RemoveMAC(mac string) error
}

const DefaultListenPort = 12345

// NewInterceptor auto-detects the best available firewall backend.
// Priority: netlink (no binary needed) → nft shell → iptables.
func NewInterceptor() (Interceptor, error) {
	// Try Go nftables library (direct netlink) first — no nft binary needed
	if nft, err := newNftNetlinkInterceptor(); err == nil {
		return nft, nil
	}
	// Fallback: shell-based nft command
	if _, err := exec.LookPath("nft"); err == nil {
		return &nftShellInterceptor{}, nil
	}
	// Fallback: iptables
	if _, err := exec.LookPath("iptables"); err == nil {
		return &iptInterceptor{}, nil
	}
	return nil, fmt.Errorf("gateway: no firewall backend available (tried netlink, nft, iptables)")
}

func splitIPsByFamily(ips []string) (v4, v6 []string) {
	for _, ip := range ips {
		parsed := net.ParseIP(ip)
		if parsed == nil {
			continue
		}
		if parsed.To4() != nil {
			v4 = append(v4, ip)
		} else {
			v6 = append(v6, ip)
		}
	}
	return
}

func splitSubnetsByFamily(subnets []string) (v4, v6 []string) {
	for _, s := range subnets {
		ip, _, err := net.ParseCIDR(s)
		if err != nil {
			continue
		}
		if ip.To4() != nil {
			v4 = append(v4, s)
		} else {
			v6 = append(v6, s)
		}
	}
	return
}

// runCmd / listChainRules are package vars so unit tests can inject fakes
// without invoking real iptables. Production code MUST go through these
// indirections.
var (
	runCmd          = execRunCmd
	listChainRules  = execListChainRules
)

func execRunCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s %s: %s: %w", name, strings.Join(args, " "), out, err)
	}
	return nil
}

// execListChainRules returns the rules in the given iptables/ip6tables table
// in -S (restore) format. Used by the interceptor's Remove() to discover
// orphan K2R / K2R_DNS / K2R_MAC references that the in-memory state does
// not know about (e.g. left over from a prior daemon crash).
func execListChainRules(ipBin, table string) (string, error) {
	// -w 5: wait for xtables.lock — even -S (read) briefly grabs it.
	out, err := exec.Command(ipBin, ipt("-t", table, "-S")...).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

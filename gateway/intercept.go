//go:build linux

package gateway

import (
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strings"

	"github.com/sasha-s/go-deadlock"
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

// RemoveInterception deletes every firewall artifact a k2r daemon installs —
// the rule table or chains and the fwmark policy route — whether or not the
// daemon that installed them is still alive. Backend detection is
// deterministic, so this picks the backend that daemon used. Best-effort and
// idempotent: `k2r service uninstall` and the daemon's own shutdown both run
// it without knowing what, if anything, is there.
func RemoveInterception() {
	i, err := NewInterceptor()
	if err != nil {
		return
	}
	i.Remove()
}

// errGatewayStopping is what an Install gets once shutdown has begun.
var errGatewayStopping = errors.New("gateway: shutting down")

// installGate is the one door every interceptor Install goes through.
// Shutdown closes it before removing the rules, so an Install racing the
// shutdown (a doUp still dialing, a device-list change) has either finished
// before the removal or is refused — it can never land behind it and leave
// rules pointing at a process that is exiting.
type installGate struct {
	installMu deadlock.Mutex // serialization: held across Install
	closed    bool
}

// install runs i.Install unless the gate is closed. A nil gate never closes.
func (ig *installGate) install(i Interceptor, cfg InterceptConfig) error {
	if ig == nil {
		return i.Install(cfg)
	}
	ig.installMu.Lock()
	defer ig.installMu.Unlock()
	if ig.closed {
		return errGatewayStopping
	}
	return i.Install(cfg)
}

// close refuses every later install; it returns once no Install is running.
func (ig *installGate) close() {
	ig.installMu.Lock()
	ig.closed = true
	ig.installMu.Unlock()
}

// linkScopeDests lists, as CIDRs, the destinations that only mean something on
// the LAN link itself: multicast, the limited broadcast and each LAN subnet's
// directed broadcast. They must not be TPROXY'd. Nothing at the far end of the
// tunnel can answer them, so each one costs a tunnel session that ends as a
// relay timeout (three of those mark the datagram relay unhealthy); and the
// TPROXY socket takes the packet away from whoever on the router was meant to
// receive it (dnsmasq, an mDNS or UPnP daemon).
func linkScopeDests(lanSubnets []string) (v4, v6 []string) {
	v4Subnets, v6Subnets := splitSubnetsByFamily(lanSubnets)
	if len(v4Subnets) > 0 {
		v4 = append(v4, "224.0.0.0/4", "255.255.255.255/32")
	}
	for _, s := range v4Subnets {
		_, ipNet, _ := net.ParseCIDR(s)
		if ones, _ := ipNet.Mask.Size(); ones > 30 {
			continue // /31 and /32 have no broadcast address
		}
		network, bcast := ipNet.IP.To4(), make(net.IP, net.IPv4len)
		for i := range bcast {
			bcast[i] = network[i] | ^ipNet.Mask[i]
		}
		v4 = append(v4, bcast.String()+"/32")
	}
	if len(v6Subnets) > 0 {
		v6 = append(v6, "ff00::/8")
	}
	return
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

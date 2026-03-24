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
}

const DefaultListenPort = 12345

// NewInterceptor auto-detects the best available firewall backend.
func NewInterceptor() (Interceptor, error) {
	if _, err := exec.LookPath("nft"); err == nil {
		return &nftInterceptor{}, nil
	}
	if _, err := exec.LookPath("iptables"); err == nil {
		return &iptInterceptor{}, nil
	}
	return nil, fmt.Errorf("gateway: neither nft nor iptables found")
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

func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s %s: %s: %w", name, strings.Join(args, " "), out, err)
	}
	return nil
}

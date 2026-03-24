//go:build linux

package gateway

import (
	"strings"
	"testing"
)

func TestNftRuleGeneration(t *testing.T) {
	nft := &nftInterceptor{}
	script := nft.buildScript(InterceptConfig{
		ListenPort: 12345,
		LANSubnets: []string{"192.168.1.0/24", "10.0.0.0/8"},
		ExcludeIPs: []string{"1.2.3.4", "8.8.8.8"},
	})
	if !strings.Contains(script, "table inet k2r") {
		t.Error("missing table inet k2r")
	}
	if !strings.Contains(script, "ip saddr 192.168.1.0/24") {
		t.Error("missing first LAN subnet")
	}
	if !strings.Contains(script, "ip saddr 10.0.0.0/8") {
		t.Error("missing second LAN subnet")
	}
	if !strings.Contains(script, "1.2.3.4") {
		t.Error("missing exclude IP")
	}
	if !strings.Contains(script, ":12345") {
		t.Error("missing TPROXY port")
	}
	if !strings.Contains(script, "meta mark set 1") {
		t.Error("missing mark set")
	}
}

func TestNftRuleGeneration_IPv6(t *testing.T) {
	nft := &nftInterceptor{}
	script := nft.buildScript(InterceptConfig{
		ListenPort: 12345,
		LANSubnets: []string{"fd00::/64"},
		ExcludeIPs: []string{"2001:db8::1"},
	})
	if !strings.Contains(script, "ip6 saddr fd00::/64") {
		t.Error("missing IPv6 LAN subnet")
	}
	if !strings.Contains(script, "2001:db8::1") {
		t.Error("missing IPv6 exclude IP")
	}
}

func TestNftRuleGeneration_EmptyExclude(t *testing.T) {
	nft := &nftInterceptor{}
	script := nft.buildScript(InterceptConfig{
		ListenPort: 12345,
		LANSubnets: []string{"192.168.1.0/24"},
		ExcludeIPs: nil,
	})
	if strings.Contains(script, "exclude_v4") {
		t.Error("should not have exclude_v4 set with no excludes")
	}
}

func TestIptRuleGeneration(t *testing.T) {
	cmds, _, _ := buildIptCommands(InterceptConfig{
		ListenPort: 12345,
		LANSubnets: []string{"192.168.1.0/24"},
		ExcludeIPs: []string{"1.2.3.4", "8.8.8.8"},
	})
	var flat []string
	for _, c := range cmds {
		flat = append(flat, c.name+" "+strings.Join(c.args, " "))
	}
	joined := strings.Join(flat, "\n")
	if !strings.Contains(joined, "-N K2R") {
		t.Error("missing chain creation")
	}
	if !strings.Contains(joined, "-d 1.2.3.4 -j RETURN") {
		t.Error("missing exclude rule")
	}
	if !strings.Contains(joined, "--on-port 12345 --tproxy-mark 1") {
		t.Error("missing TPROXY rule")
	}
	if !strings.Contains(joined, "-A PREROUTING -s 192.168.1.0/24 -j K2R") {
		t.Error("missing PREROUTING jump")
	}
}

func TestIptRuleGeneration_IPv6(t *testing.T) {
	cmds, _, _ := buildIptCommands(InterceptConfig{
		ListenPort: 12345,
		LANSubnets: []string{"fd00::/64"},
		ExcludeIPs: []string{"2001:db8::1"},
	})
	var flat []string
	for _, c := range cmds {
		flat = append(flat, c.name+" "+strings.Join(c.args, " "))
	}
	joined := strings.Join(flat, "\n")
	if !strings.Contains(joined, "ip6tables") {
		t.Error("missing ip6tables commands")
	}
	if !strings.Contains(joined, "fd00::/64") {
		t.Error("missing IPv6 subnet")
	}
}

//go:build linux

package gateway

import (
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
)

type nftShellInterceptor struct {
	installed bool
}

func (n *nftShellInterceptor) Name() string { return "nftables" }

func (n *nftShellInterceptor) Install(cfg InterceptConfig) error {
	script := n.buildScript(cfg)
	cmd := exec.Command("nft", "-f", "-")
	cmd.Stdin = strings.NewReader(script)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("nft script: %s: %w", out, err)
	}
	for _, c := range []struct{ args []string }{
		{[]string{"rule", "add", "fwmark", "1", "table", "100"}},
		{[]string{"route", "add", "local", "0.0.0.0/0", "dev", "lo", "table", "100"}},
		{[]string{"-6", "rule", "add", "fwmark", "1", "table", "100"}},
		{[]string{"-6", "route", "add", "local", "::/0", "dev", "lo", "table", "100"}},
	} {
		if err := runCmd("ip", c.args...); err != nil {
			slog.Warn("gateway: ip rule/route setup", "args", c.args, "err", err)
		}
	}
	n.installed = true
	slog.Info("DIAG: gw-intercept-install", "backend", "nftables",
		"subnets", cfg.LANSubnets, "excludeCount", len(cfg.ExcludeIPs))
	return nil
}

func (n *nftShellInterceptor) Remove() {
	_ = runCmd("nft", "delete", "table", "inet", "k2r")
	_ = runCmd("ip", "rule", "del", "fwmark", "1", "table", "100")
	_ = runCmd("ip", "route", "del", "local", "0.0.0.0/0", "dev", "lo", "table", "100")
	_ = runCmd("ip", "-6", "rule", "del", "fwmark", "1", "table", "100")
	_ = runCmd("ip", "-6", "route", "del", "local", "::/0", "dev", "lo", "table", "100")
	n.installed = false
	slog.Info("gateway: nftables rules cleaned up")
}

func (n *nftShellInterceptor) buildScript(cfg InterceptConfig) string {
	port := fmt.Sprintf(":%d", cfg.ListenPort)
	v4Subnets, v6Subnets := splitSubnetsByFamily(cfg.LANSubnets)
	v4Excludes, v6Excludes := splitIPsByFamily(cfg.ExcludeIPs)

	var lines []string
	lines = append(lines, "add table inet k2r")

	if len(v4Excludes) > 0 {
		lines = append(lines, fmt.Sprintf(
			"add set inet k2r exclude_v4 { type ipv4_addr ; elements = { %s } ; }",
			strings.Join(v4Excludes, ", ")))
	}
	if len(v6Excludes) > 0 {
		lines = append(lines, fmt.Sprintf(
			"add set inet k2r exclude_v6 { type ipv6_addr ; elements = { %s } ; }",
			strings.Join(v6Excludes, ", ")))
	}

	lines = append(lines,
		"add chain inet k2r prerouting { type filter hook prerouting priority mangle ; policy accept ; }")

	// DNS redirect needs a NAT chain (redirect is a NAT operation).
	// dnsredir covers LAN→ANY:53; dnsredir_out covers locally-generated DNS
	// (dnsmasq → ISP upstream) which never traverses PREROUTING.
	if cfg.DNSRedirect && cfg.DNSPort > 0 {
		lines = append(lines,
			"add chain inet k2r dnsredir { type nat hook prerouting priority dstnat ; policy accept ; }",
			"add chain inet k2r dnsredir_out { type nat hook output priority -100 ; policy accept ; }")
	}

	// Anchor DNAT: unconditional, independent of LANSubnets/DNSRedirect/
	// AllowlistMode — see gateway/anchor.go.
	lines = append(lines, anchorNftLines()...)

	lines = append(lines,
		`add rule inet k2r prerouting iifname "lo" return`)

	if len(v4Excludes) > 0 {
		lines = append(lines, "add rule inet k2r prerouting ip daddr @exclude_v4 return")
	}
	if len(v6Excludes) > 0 {
		lines = append(lines, "add rule inet k2r prerouting ip6 daddr @exclude_v6 return")
	}

	// MAC allowlist
	if cfg.AllowlistMode && len(cfg.AllowedMACs) > 0 {
		lines = append(lines, fmt.Sprintf(
			"add set inet k2r allowed_macs { type ether_addr ; elements = { %s } ; }",
			strings.Join(cfg.AllowedMACs, ", ")))
		lines = append(lines,
			`add rule inet k2r prerouting meta iiftype != ether return`)
		lines = append(lines,
			"add rule inet k2r prerouting ether saddr != @allowed_macs drop")
	}

	// DNS redirect: port 53 → DNSPort (in NAT chain, not filter chain)
	if cfg.DNSRedirect && cfg.DNSPort > 0 {
		dnsPort := fmt.Sprintf("%d", cfg.DNSPort)
		for _, subnet := range v4Subnets {
			lines = append(lines, fmt.Sprintf(
				"add rule inet k2r dnsredir ip saddr %s meta l4proto { tcp, udp } th dport 53 redirect to :%s", subnet, dnsPort))
		}
		for _, subnet := range v6Subnets {
			lines = append(lines, fmt.Sprintf(
				"add rule inet k2r dnsredir ip6 saddr %s meta l4proto { tcp, udp } th dport 53 redirect to :%s", subnet, dnsPort))
		}

		// OUTPUT-hook chain — mark RETURN must be the FIRST rule so k2r's
		// own DirectDNS queries (stamped with K2RFwmark in
		// gatewayDirectDialer.ControlFunc) escape before the REDIRECT,
		// avoiding infinite recursion through k2r's own DNS listener.
		lines = append(lines, fmt.Sprintf(
			"add rule inet k2r dnsredir_out meta mark 0x%x return", K2RFwmark))
		lines = append(lines, fmt.Sprintf(
			"add rule inet k2r dnsredir_out meta l4proto { tcp, udp } th dport 53 redirect to :%s", dnsPort))
	}

	for _, subnet := range v4Subnets {
		lines = append(lines, fmt.Sprintf(
			"add rule inet k2r prerouting ip saddr %s meta l4proto tcp tproxy ip to %s meta mark set 1", subnet, port))
		lines = append(lines, fmt.Sprintf(
			"add rule inet k2r prerouting ip saddr %s meta l4proto udp tproxy ip to %s meta mark set 1", subnet, port))
	}
	for _, subnet := range v6Subnets {
		lines = append(lines, fmt.Sprintf(
			"add rule inet k2r prerouting ip6 saddr %s meta l4proto tcp tproxy ip6 to %s meta mark set 1", subnet, port))
		lines = append(lines, fmt.Sprintf(
			"add rule inet k2r prerouting ip6 saddr %s meta l4proto udp tproxy ip6 to %s meta mark set 1", subnet, port))
	}

	return strings.Join(lines, "\n") + "\n"
}

//go:build linux

package gateway

import (
	"fmt"
	"log/slog"
	"strings"
)

type iptInterceptor struct {
	installed bool
}

func (i *iptInterceptor) Name() string { return "iptables" }

type cmdSpec struct {
	name string
	args []string
}

// Install configures iptables rules for TPROXY interception. Idempotent:
// any stale K2R-family chains or PREROUTING jumps (from a previous crashed
// daemon, mid-life reconfigure, or partial Remove) are cleaned first.
func (i *iptInterceptor) Install(cfg InterceptConfig) error {
	// Pre-flight: tear down anything that resembles K2R state. Required so
	// `-N K2R` below doesn't fail with "Chain already exists" when a prior
	// crash left rules behind (bug discovered on OpenWrt MIPS during UAT).
	i.Remove()

	cmds, _, _ := buildIptCommands(cfg)
	for _, c := range cmds {
		if err := runCmd(c.name, c.args...); err != nil {
			i.Remove()
			return fmt.Errorf("gateway: %s %v: %w", c.name, c.args, err)
		}
	}
	i.installed = true
	slog.Info("DIAG: gw-intercept-install", "backend", "iptables",
		"subnets", cfg.LANSubnets, "excludeCount", len(cfg.ExcludeIPs))
	return nil
}

// Remove unconditionally tears down K2R-family chains (K2R, K2R_MAC, K2R_DNS)
// and their PREROUTING jumps in mangle + nat tables for both IPv4 and IPv6.
// Discovers what's present via `iptables -S` rather than relying on cached
// in-memory state — so it correctly cleans orphans left by a previous daemon
// instance whose state was lost on restart.
//
// All errors are logged at DEBUG, never propagated. Remove must be
// best-effort: it's called both as a pre-flight in Install and as a teardown
// in doDown.
func (i *iptInterceptor) Remove() {
	cleanK2RFamily("iptables", "mangle")
	cleanK2RFamily("iptables", "nat")
	cleanK2RFamily("ip6tables", "mangle")
	cleanK2RFamily("ip6tables", "nat")

	cleanupIPRules()

	i.installed = false
	slog.Info("gateway: iptables rules cleaned up")
}

// cleanK2RFamily removes every PREROUTING jump targeting a K2R* chain, then
// flushes and deletes every K2R* chain it finds. Discovers state from
// `iptables -t <table> -S` so it works even when the caller has no prior
// in-memory tracking.
func cleanK2RFamily(ipBin, table string) {
	out, err := listChainRules(ipBin, table)
	if err != nil {
		slog.Debug("gateway: listChainRules", "ipBin", ipBin, "table", table, "err", err)
		return
	}

	// Pass 1: delete every jump (from any built-in chain) targeting a
	// K2R-family chain. Must come before -X so the chain becomes deletable.
	//
	// Previously this scanned only PREROUTING. K2R_DNS_OUT is jumped from
	// OUTPUT (nat); without the generalization, the OUTPUT jump survives
	// doDown and permanently breaks system DNS once k2r removes the chain.
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "-A ") {
			continue
		}
		target := extractJumpTarget(line)
		if !strings.HasPrefix(target, "K2R") {
			continue
		}
		// Convert "-A <CHAIN> ..." to "-D <CHAIN> ..." and prepend -t.
		args := append(ipt("-t", table), strings.Fields("-D"+line[2:])...)
		if err := runCmd(ipBin, args...); err != nil {
			slog.Debug("gateway: drop jump", "ipBin", ipBin, "rule", line, "err", err)
		}
	}

	// Pass 2: flush + delete every K2R-family chain in this table.
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "-N ") {
			continue
		}
		chain := strings.TrimSpace(line[3:])
		if !strings.HasPrefix(chain, "K2R") {
			continue
		}
		if err := runCmd(ipBin, ipt("-t", table, "-F", chain)...); err != nil {
			slog.Debug("gateway: flush chain", "ipBin", ipBin, "table", table, "chain", chain, "err", err)
		}
		if err := runCmd(ipBin, ipt("-t", table, "-X", chain)...); err != nil {
			slog.Debug("gateway: delete chain", "ipBin", ipBin, "table", table, "chain", chain, "err", err)
		}
	}
}

// ipt prepends `-w 5` so iptables waits up to 5 s for /var/run/xtables.lock
// instead of failing immediately when another process holds it. On OpenWRT
// the common culprit is /etc/hotplug.d/iface/20-firewall firing on link
// state changes (WiFi re-association during a daemon respawn).
// iptables/ip6tables only; ip(8) uses netlink and rejects -w.
// Bug 8 fix (2026-05-21).
func ipt(args ...string) []string {
	return append([]string{"-w", "5"}, args...)
}

// extractJumpTarget returns the value after "-j " in an iptables rule line,
// or "" if no -j is present.
func extractJumpTarget(line string) string {
	idx := strings.Index(line, "-j ")
	if idx < 0 {
		return ""
	}
	rest := line[idx+3:]
	if sp := strings.IndexByte(rest, ' '); sp >= 0 {
		return rest[:sp]
	}
	return rest
}

// buildIptCommands is a pure function — generates commands without mutating receiver.
func buildIptCommands(cfg InterceptConfig) (cmds []cmdSpec, v4Subnets, v6Subnets []string) {
	v4Subnets, v6Subnets = splitSubnetsByFamily(cfg.LANSubnets)
	v4Excludes, v6Excludes := splitIPsByFamily(cfg.ExcludeIPs)
	v4Link, v6Link := linkScopeDests(cfg.LANSubnets)
	port := fmt.Sprintf("%d", cfg.ListenPort)

	if len(v4Subnets) > 0 {
		cmds = append(cmds, cmdSpec{"iptables", ipt("-t", "mangle", "-N", "K2R")})
		for _, ip := range v4Excludes {
			cmds = append(cmds, cmdSpec{"iptables", ipt("-t", "mangle", "-A", "K2R", "-d", ip, "-j", "RETURN")})
		}
		// MAC allowlist: non-allowed MACs get dropped before TPROXY
		// Allowed MACs jump to K2R_PROXY; non-matching fall through to DROP
		if cfg.AllowlistMode && len(cfg.AllowedMACs) > 0 {
			cmds = append(cmds, cmdSpec{"iptables", ipt("-t", "mangle", "-N", "K2R_MAC")})
			for _, mac := range cfg.AllowedMACs {
				// Allowed MACs: jump to TPROXY chain
				cmds = append(cmds, cmdSpec{"iptables", ipt("-t", "mangle", "-A", "K2R_MAC", "-m", "mac", "--mac-source", mac, "-j", "RETURN")})
			}
			// Non-allowed: drop
			cmds = append(cmds, cmdSpec{"iptables", ipt("-t", "mangle", "-A", "K2R_MAC", "-j", "DROP")})
			// Jump to MAC check from K2R — if RETURN, continues to TPROXY below
			cmds = append(cmds, cmdSpec{"iptables", ipt("-t", "mangle", "-A", "K2R", "-j", "K2R_MAC")})
		}
		// Link-scope destinations never enter the tunnel — see linkScopeDests.
		for _, cidr := range v4Link {
			cmds = append(cmds, cmdSpec{"iptables", ipt("-t", "mangle", "-A", "K2R", "-d", cidr, "-j", "RETURN")})
		}
		// Port 53 is K2R_DNS's (nat REDIRECT) when DNSRedirect is on; TPROXY
		// must not claim it as well — see the nft interceptor for the
		// double-handling failure this prevents.
		if cfg.DNSRedirect && cfg.DNSPort > 0 {
			cmds = append(cmds,
				cmdSpec{"iptables", ipt("-t", "mangle", "-A", "K2R", "-p", "tcp", "--dport", "53", "-j", "RETURN")},
				cmdSpec{"iptables", ipt("-t", "mangle", "-A", "K2R", "-p", "udp", "--dport", "53", "-j", "RETURN")},
			)
		}
		cmds = append(cmds,
			cmdSpec{"iptables", ipt("-t", "mangle", "-A", "K2R", "-p", "tcp", "-j", "TPROXY", "--on-port", port, "--tproxy-mark", "1")},
			cmdSpec{"iptables", ipt("-t", "mangle", "-A", "K2R", "-p", "udp", "-j", "TPROXY", "--on-port", port, "--tproxy-mark", "1")},
		)
		for _, s := range v4Subnets {
			cmds = append(cmds, cmdSpec{"iptables", ipt("-t", "mangle", "-A", "PREROUTING", "-s", s, "-j", "K2R")})
		}
	}

	if len(v6Subnets) > 0 {
		cmds = append(cmds, cmdSpec{"ip6tables", ipt("-t", "mangle", "-N", "K2R")})
		for _, ip := range v6Excludes {
			cmds = append(cmds, cmdSpec{"ip6tables", ipt("-t", "mangle", "-A", "K2R", "-d", ip, "-j", "RETURN")})
		}
		if cfg.AllowlistMode && len(cfg.AllowedMACs) > 0 {
			cmds = append(cmds, cmdSpec{"ip6tables", ipt("-t", "mangle", "-N", "K2R_MAC")})
			for _, mac := range cfg.AllowedMACs {
				cmds = append(cmds, cmdSpec{"ip6tables", ipt("-t", "mangle", "-A", "K2R_MAC", "-m", "mac", "--mac-source", mac, "-j", "RETURN")})
			}
			cmds = append(cmds, cmdSpec{"ip6tables", ipt("-t", "mangle", "-A", "K2R_MAC", "-j", "DROP")})
			cmds = append(cmds, cmdSpec{"ip6tables", ipt("-t", "mangle", "-A", "K2R", "-j", "K2R_MAC")})
		}
		for _, cidr := range v6Link {
			cmds = append(cmds, cmdSpec{"ip6tables", ipt("-t", "mangle", "-A", "K2R", "-d", cidr, "-j", "RETURN")})
		}
		if cfg.DNSRedirect && cfg.DNSPort > 0 {
			cmds = append(cmds,
				cmdSpec{"ip6tables", ipt("-t", "mangle", "-A", "K2R", "-p", "tcp", "--dport", "53", "-j", "RETURN")},
				cmdSpec{"ip6tables", ipt("-t", "mangle", "-A", "K2R", "-p", "udp", "--dport", "53", "-j", "RETURN")},
			)
		}
		cmds = append(cmds,
			cmdSpec{"ip6tables", ipt("-t", "mangle", "-A", "K2R", "-p", "tcp", "-j", "TPROXY", "--on-port", port, "--tproxy-mark", "1")},
			cmdSpec{"ip6tables", ipt("-t", "mangle", "-A", "K2R", "-p", "udp", "-j", "TPROXY", "--on-port", port, "--tproxy-mark", "1")},
		)
		for _, s := range v6Subnets {
			cmds = append(cmds, cmdSpec{"ip6tables", ipt("-t", "mangle", "-A", "PREROUTING", "-s", s, "-j", "K2R")})
		}
	}

	// DNS redirect (nat table, separate chain)
	if cfg.DNSRedirect && cfg.DNSPort > 0 {
		dnsPort := fmt.Sprintf("%d", cfg.DNSPort)
		fwmark := fmt.Sprintf("0x%x", K2RFwmark)
		if len(v4Subnets) > 0 {
			cmds = append(cmds, cmdSpec{"iptables", ipt("-t", "nat", "-N", "K2R_DNS")})
			for _, s := range v4Subnets {
				cmds = append(cmds,
					cmdSpec{"iptables", ipt("-t", "nat", "-A", "K2R_DNS", "-s", s, "-p", "tcp", "--dport", "53", "-j", "REDIRECT", "--to-ports", dnsPort)},
					cmdSpec{"iptables", ipt("-t", "nat", "-A", "K2R_DNS", "-s", s, "-p", "udp", "--dport", "53", "-j", "REDIRECT", "--to-ports", dnsPort)},
				)
			}
			cmds = append(cmds, cmdSpec{"iptables", ipt("-t", "nat", "-A", "PREROUTING", "-j", "K2R_DNS")})
		}
		if len(v6Subnets) > 0 {
			cmds = append(cmds, cmdSpec{"ip6tables", ipt("-t", "nat", "-N", "K2R_DNS")})
			for _, s := range v6Subnets {
				cmds = append(cmds,
					cmdSpec{"ip6tables", ipt("-t", "nat", "-A", "K2R_DNS", "-s", s, "-p", "tcp", "--dport", "53", "-j", "REDIRECT", "--to-ports", dnsPort)},
					cmdSpec{"ip6tables", ipt("-t", "nat", "-A", "K2R_DNS", "-s", s, "-p", "udp", "--dport", "53", "-j", "REDIRECT", "--to-ports", dnsPort)},
				)
			}
			cmds = append(cmds, cmdSpec{"ip6tables", ipt("-t", "nat", "-A", "PREROUTING", "-j", "K2R_DNS")})
		}

		// K2R_DNS_OUT: OUTPUT-chain REDIRECT for locally-generated DNS (the
		// dnsmasq → ISP upstream leak path). Mark RETURN must come FIRST so
		// k2r's own DirectDNS queries (stamped with K2RFwmark via
		// gatewayDirectDialer.ControlFunc) escape before REDIRECT — otherwise
		// every DirectDNS query would land back in k2r's DNS listener and
		// recurse.
		//
		// Dual-stack: always installed (router may originate v4 or v6 DNS).
		cmds = append(cmds,
			cmdSpec{"iptables", ipt("-t", "nat", "-N", "K2R_DNS_OUT")},
			cmdSpec{"iptables", ipt("-t", "nat", "-A", "K2R_DNS_OUT", "-m", "mark", "--mark", fwmark, "-j", "RETURN")},
			cmdSpec{"iptables", ipt("-t", "nat", "-A", "K2R_DNS_OUT", "-p", "tcp", "-j", "REDIRECT", "--to-ports", dnsPort)},
			cmdSpec{"iptables", ipt("-t", "nat", "-A", "K2R_DNS_OUT", "-p", "udp", "-j", "REDIRECT", "--to-ports", dnsPort)},
			cmdSpec{"iptables", ipt("-t", "nat", "-A", "OUTPUT", "-p", "tcp", "--dport", "53", "-j", "K2R_DNS_OUT")},
			cmdSpec{"iptables", ipt("-t", "nat", "-A", "OUTPUT", "-p", "udp", "--dport", "53", "-j", "K2R_DNS_OUT")},
		)
		cmds = append(cmds,
			cmdSpec{"ip6tables", ipt("-t", "nat", "-N", "K2R_DNS_OUT")},
			cmdSpec{"ip6tables", ipt("-t", "nat", "-A", "K2R_DNS_OUT", "-m", "mark", "--mark", fwmark, "-j", "RETURN")},
			cmdSpec{"ip6tables", ipt("-t", "nat", "-A", "K2R_DNS_OUT", "-p", "tcp", "-j", "REDIRECT", "--to-ports", dnsPort)},
			cmdSpec{"ip6tables", ipt("-t", "nat", "-A", "K2R_DNS_OUT", "-p", "udp", "-j", "REDIRECT", "--to-ports", dnsPort)},
			cmdSpec{"ip6tables", ipt("-t", "nat", "-A", "OUTPUT", "-p", "tcp", "--dport", "53", "-j", "K2R_DNS_OUT")},
			cmdSpec{"ip6tables", ipt("-t", "nat", "-A", "OUTPUT", "-p", "udp", "--dport", "53", "-j", "K2R_DNS_OUT")},
		)
	}

	// Anchor DNAT: unconditional, independent of LANSubnets/DNSRedirect/
	// AllowlistMode — see gateway/anchor.go.
	cmds = append(cmds, anchorIptCommands()...)

	cmds = append(cmds,
		cmdSpec{"ip", []string{"rule", "add", "fwmark", "1", "table", "100"}},
		cmdSpec{"ip", []string{"route", "add", "local", "0.0.0.0/0", "dev", "lo", "table", "100"}},
		cmdSpec{"ip", []string{"-6", "rule", "add", "fwmark", "1", "table", "100"}},
		cmdSpec{"ip", []string{"-6", "route", "add", "local", "::/0", "dev", "lo", "table", "100"}},
	)

	return cmds, v4Subnets, v6Subnets
}

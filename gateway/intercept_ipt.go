//go:build linux

package gateway

import (
	"fmt"
	"log/slog"
)

type iptInterceptor struct {
	installed   bool
	hadV4Subnet bool
	hadV6Subnet bool
	v4Subnets   []string
	v6Subnets   []string
}

func (i *iptInterceptor) Name() string { return "iptables" }

type cmdSpec struct {
	name string
	args []string
}

func (i *iptInterceptor) Install(cfg InterceptConfig) error {
	cmds, v4Subnets, v6Subnets := buildIptCommands(cfg)
	for _, c := range cmds {
		if err := runCmd(c.name, c.args...); err != nil {
			i.Remove()
			return fmt.Errorf("gateway: %s %v: %w", c.name, c.args, err)
		}
	}
	i.v4Subnets = v4Subnets
	i.v6Subnets = v6Subnets
	i.hadV4Subnet = len(v4Subnets) > 0
	i.hadV6Subnet = len(v6Subnets) > 0
	i.installed = true
	slog.Info("DIAG: gw-intercept-install", "backend", "iptables",
		"subnets", cfg.LANSubnets, "excludeCount", len(cfg.ExcludeIPs))
	return nil
}

func (i *iptInterceptor) Remove() {
	if i.hadV4Subnet {
		for _, s := range i.v4Subnets {
			_ = runCmd("iptables", "-t", "mangle", "-D", "PREROUTING", "-s", s, "-j", "K2R")
		}
		_ = runCmd("iptables", "-t", "mangle", "-F", "K2R")
		_ = runCmd("iptables", "-t", "mangle", "-X", "K2R")
	}
	if i.hadV6Subnet {
		for _, s := range i.v6Subnets {
			_ = runCmd("ip6tables", "-t", "mangle", "-D", "PREROUTING", "-s", s, "-j", "K2R")
		}
		_ = runCmd("ip6tables", "-t", "mangle", "-F", "K2R")
		_ = runCmd("ip6tables", "-t", "mangle", "-X", "K2R")
	}
	_ = runCmd("ip", "rule", "del", "fwmark", "1", "table", "100")
	_ = runCmd("ip", "route", "del", "local", "0.0.0.0/0", "dev", "lo", "table", "100")
	_ = runCmd("ip", "-6", "rule", "del", "fwmark", "1", "table", "100")
	_ = runCmd("ip", "-6", "route", "del", "local", "::/0", "dev", "lo", "table", "100")
	i.installed = false
	slog.Info("gateway: iptables rules cleaned up")
}

// buildIptCommands is a pure function — generates commands without mutating receiver.
func buildIptCommands(cfg InterceptConfig) (cmds []cmdSpec, v4Subnets, v6Subnets []string) {
	v4Subnets, v6Subnets = splitSubnetsByFamily(cfg.LANSubnets)
	v4Excludes, v6Excludes := splitIPsByFamily(cfg.ExcludeIPs)
	port := fmt.Sprintf("%d", cfg.ListenPort)

	if len(v4Subnets) > 0 {
		cmds = append(cmds, cmdSpec{"iptables", []string{"-t", "mangle", "-N", "K2R"}})
		for _, ip := range v4Excludes {
			cmds = append(cmds, cmdSpec{"iptables", []string{"-t", "mangle", "-A", "K2R", "-d", ip, "-j", "RETURN"}})
		}
		cmds = append(cmds,
			cmdSpec{"iptables", []string{"-t", "mangle", "-A", "K2R", "-p", "tcp", "-j", "TPROXY", "--on-port", port, "--tproxy-mark", "1"}},
			cmdSpec{"iptables", []string{"-t", "mangle", "-A", "K2R", "-p", "udp", "-j", "TPROXY", "--on-port", port, "--tproxy-mark", "1"}},
		)
		for _, s := range v4Subnets {
			cmds = append(cmds, cmdSpec{"iptables", []string{"-t", "mangle", "-A", "PREROUTING", "-s", s, "-j", "K2R"}})
		}
	}

	if len(v6Subnets) > 0 {
		cmds = append(cmds, cmdSpec{"ip6tables", []string{"-t", "mangle", "-N", "K2R"}})
		for _, ip := range v6Excludes {
			cmds = append(cmds, cmdSpec{"ip6tables", []string{"-t", "mangle", "-A", "K2R", "-d", ip, "-j", "RETURN"}})
		}
		cmds = append(cmds,
			cmdSpec{"ip6tables", []string{"-t", "mangle", "-A", "K2R", "-p", "tcp", "-j", "TPROXY", "--on-port", port, "--tproxy-mark", "1"}},
			cmdSpec{"ip6tables", []string{"-t", "mangle", "-A", "K2R", "-p", "udp", "-j", "TPROXY", "--on-port", port, "--tproxy-mark", "1"}},
		)
		for _, s := range v6Subnets {
			cmds = append(cmds, cmdSpec{"ip6tables", []string{"-t", "mangle", "-A", "PREROUTING", "-s", s, "-j", "K2R"}})
		}
	}

	cmds = append(cmds,
		cmdSpec{"ip", []string{"rule", "add", "fwmark", "1", "table", "100"}},
		cmdSpec{"ip", []string{"route", "add", "local", "0.0.0.0/0", "dev", "lo", "table", "100"}},
		cmdSpec{"ip", []string{"-6", "rule", "add", "fwmark", "1", "table", "100"}},
		cmdSpec{"ip", []string{"-6", "route", "add", "local", "::/0", "dev", "lo", "table", "100"}},
	)

	return cmds, v4Subnets, v6Subnets
}

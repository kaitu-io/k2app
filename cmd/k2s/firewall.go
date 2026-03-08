package main

import (
	"fmt"
	"os/exec"
	"strings"
)

// firewallType represents the detected firewall system.
type firewallType string

const (
	firewallUFW      firewallType = "ufw"
	firewallFirewalld firewallType = "firewalld"
	firewallIPTables firewallType = "iptables"
	firewallNone     firewallType = "none"
)

// shellCmd represents a structured shell command with explicit name and args.
type shellCmd struct {
	name string
	args []string
}

func (c shellCmd) String() string {
	return c.name + " " + strings.Join(c.args, " ")
}

func (c shellCmd) Run() error {
	return exec.Command(c.name, c.args...).Run()
}

// detectFirewall detects the active firewall system.
func detectFirewall() firewallType {
	// Check ufw
	if _, err := exec.LookPath("ufw"); err == nil {
		// Verify ufw is active
		out, err := exec.Command("ufw", "status").CombinedOutput()
		if err == nil && strings.Contains(string(out), "active") {
			return firewallUFW
		}
	}

	// Check firewalld
	if _, err := exec.LookPath("firewall-cmd"); err == nil {
		if exec.Command("firewall-cmd", "--state").Run() == nil {
			return firewallFirewalld
		}
	}

	// Check if iptables is available
	if _, err := exec.LookPath("iptables"); err == nil {
		return firewallIPTables
	}

	return firewallNone
}

// firewallRules generates the commands needed to open ports for the given firewall type.
func firewallRules(fw firewallType, port int, hopStart, hopEnd int) []shellCmd {
	switch fw {
	case firewallUFW:
		return ufwRules(port, hopStart, hopEnd)
	case firewallFirewalld:
		return firewalldRules(port, hopStart, hopEnd)
	case firewallIPTables:
		return iptablesRules(port, hopStart, hopEnd)
	default:
		return nil
	}
}

func ufwRules(port int, hopStart, hopEnd int) []shellCmd {
	rules := []shellCmd{
		{name: "ufw", args: []string{"allow", fmt.Sprintf("%d/tcp", port)}},
		{name: "ufw", args: []string{"allow", fmt.Sprintf("%d/udp", port)}},
	}
	if hopStart > 0 && hopEnd > 0 {
		rules = append(rules, shellCmd{name: "ufw", args: []string{"allow", fmt.Sprintf("%d:%d/udp", hopStart, hopEnd)}})
	}
	return rules
}

func firewalldRules(port int, hopStart, hopEnd int) []shellCmd {
	rules := []shellCmd{
		{name: "firewall-cmd", args: []string{"--permanent", fmt.Sprintf("--add-port=%d/tcp", port)}},
		{name: "firewall-cmd", args: []string{"--permanent", fmt.Sprintf("--add-port=%d/udp", port)}},
	}
	if hopStart > 0 && hopEnd > 0 {
		rules = append(rules,
			shellCmd{name: "firewall-cmd", args: []string{"--permanent", fmt.Sprintf("--add-port=%d-%d/udp", hopStart, hopEnd)}},
			shellCmd{name: "firewall-cmd", args: []string{"--permanent", fmt.Sprintf("--add-forward-port=port=%d-%d:proto=udp:toport=%d", hopStart, hopEnd, port)}},
		)
	}
	rules = append(rules, shellCmd{name: "firewall-cmd", args: []string{"--reload"}})
	return rules
}

func iptablesRules(port int, hopStart, hopEnd int) []shellCmd {
	rules := []shellCmd{
		{name: "iptables", args: []string{"-A", "INPUT", "-p", "tcp", "--dport", fmt.Sprintf("%d", port), "-j", "ACCEPT"}},
		{name: "iptables", args: []string{"-A", "INPUT", "-p", "udp", "--dport", fmt.Sprintf("%d", port), "-j", "ACCEPT"}},
	}
	if hopStart > 0 && hopEnd > 0 {
		rules = append(rules,
			shellCmd{name: "iptables", args: []string{"-A", "INPUT", "-p", "udp", "--dport", fmt.Sprintf("%d:%d", hopStart, hopEnd), "-j", "ACCEPT"}},
			shellCmd{name: "iptables", args: []string{"-t", "nat", "-A", "PREROUTING", "-p", "udp", "--dport", fmt.Sprintf("%d:%d", hopStart, hopEnd), "-j", "REDIRECT", "--to-port", fmt.Sprintf("%d", port)}},
		)
	}
	return rules
}

// hopNATRules generates NAT redirect rules for port hopping (separate from port opening).
// These redirect hop ports to the main listen port.
func hopNATRules(fw firewallType, port, hopStart, hopEnd int) []shellCmd {
	if hopStart == 0 || hopEnd == 0 {
		return nil
	}
	switch fw {
	case firewallUFW:
		// ufw doesn't manage NAT, use raw iptables
		return []shellCmd{
			{name: "iptables", args: []string{"-t", "nat", "-A", "PREROUTING", "-p", "udp", "--dport", fmt.Sprintf("%d:%d", hopStart, hopEnd), "-j", "REDIRECT", "--to-port", fmt.Sprintf("%d", port)}},
		}
	case firewallFirewalld:
		// Already included in firewalldRules
		return nil
	case firewallIPTables:
		// Already included in iptablesRules
		return nil
	default:
		return nil
	}
}

// runFirewallSetup detects the firewall, shows rules, and optionally executes them.
func runFirewallSetup(port int, hopStart, hopEnd int) {
	fmt.Println()
	fmt.Println("🛡️  防火墙配置 / Firewall setup")
	fmt.Println()

	fw := detectFirewall()

	switch fw {
	case firewallNone:
		biPrint("未检测到防火墙", "No firewall detected")
		fmt.Println()
		biPrint("⚠️  如使用云服务器，请在安全组中放行以下端口",
			"⚠️  If using cloud VPS, open these ports in Security Group")
		printRequiredPorts(port, hopStart, hopEnd)
		return

	case firewallIPTables:
		biPrint("检测到 iptables（无 ufw/firewalld）", "Detected: iptables (no ufw/firewalld)")
		fmt.Println()
		biPrint("请手动执行以下命令", "Please run these commands manually")
		rules := firewallRules(fw, port, hopStart, hopEnd)
		for _, rule := range rules {
			fmt.Printf("  %s\n", rule)
		}
		fmt.Println()
		biPrint("⚠️  如使用云服务器，请同时在安全组中放行以上端口",
			"⚠️  If using cloud VPS, also open these ports in Security Group")
		return

	default:
		// ufw or firewalld — can auto-execute
		biPrintf("检测到 %s", "Detected: %s", string(fw))
	}

	fmt.Println()
	biPrint("需要放行以下端口", "Ports to open")
	printRequiredPorts(port, hopStart, hopEnd)
	fmt.Println()

	autoConfig := promptYesNo("自动配置？", "Auto-configure?", true)

	rules := firewallRules(fw, port, hopStart, hopEnd)

	// For ufw with hop, also need NAT rules
	natRules := hopNATRules(fw, port, hopStart, hopEnd)

	allRules := append(rules, natRules...)

	if autoConfig {
		for _, cmd := range allRules {
			fmt.Printf("  ✔ %s\n", cmd)
			if err := cmd.Run(); err != nil {
				fmt.Printf("  ⚠️  %s: %v\n", cmd, err)
			}
		}
	} else {
		biPrint("请手动执行以下命令", "Please run these commands manually")
		for _, cmd := range allRules {
			fmt.Printf("  %s\n", cmd)
		}
	}

	fmt.Println()
	biPrint("⚠️  如使用云服务器，请同时在安全组中放行以上端口",
		"⚠️  If using cloud VPS, also open these ports in Security Group")
}

// printRequiredPorts displays the ports that need to be opened.
func printRequiredPorts(port int, hopStart, hopEnd int) {
	fmt.Printf("  %d/tcp + %d/udp (主端口 / main)\n", port, port)
	if hopStart > 0 && hopEnd > 0 {
		fmt.Printf("  %d-%d/udp      (端口跳跃 / hop)\n", hopStart, hopEnd)
	}
}

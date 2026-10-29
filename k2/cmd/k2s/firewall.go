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
func firewallRules(fw firewallType, port int) []shellCmd {
	switch fw {
	case firewallUFW:
		return ufwRules(port)
	case firewallFirewalld:
		return firewalldRules(port)
	case firewallIPTables:
		return iptablesRules(port)
	default:
		return nil
	}
}

func ufwRules(port int) []shellCmd {
	return []shellCmd{
		{name: "ufw", args: []string{"allow", fmt.Sprintf("%d/tcp", port)}},
		{name: "ufw", args: []string{"allow", fmt.Sprintf("%d/udp", port)}},
	}
}

func firewalldRules(port int) []shellCmd {
	return []shellCmd{
		{name: "firewall-cmd", args: []string{"--permanent", fmt.Sprintf("--add-port=%d/tcp", port)}},
		{name: "firewall-cmd", args: []string{"--permanent", fmt.Sprintf("--add-port=%d/udp", port)}},
		{name: "firewall-cmd", args: []string{"--reload"}},
	}
}

func iptablesRules(port int) []shellCmd {
	return []shellCmd{
		{name: "iptables", args: []string{"-A", "INPUT", "-p", "tcp", "--dport", fmt.Sprintf("%d", port), "-j", "ACCEPT"}},
		{name: "iptables", args: []string{"-A", "INPUT", "-p", "udp", "--dport", fmt.Sprintf("%d", port), "-j", "ACCEPT"}},
	}
}

// runFirewallSetup detects the firewall, shows rules, and optionally executes them.
func runFirewallSetup(port int) {
	fmt.Println()
	fmt.Println("Firewall setup")
	fmt.Println()

	fw := detectFirewall()

	switch fw {
	case firewallNone:
		fmt.Println("No firewall detected")
		fmt.Println()
		fmt.Println("If using cloud VPS, open these ports in Security Group:")
		printRequiredPorts(port)
		return

	case firewallIPTables:
		fmt.Println("Detected: iptables (no ufw/firewalld)")
		fmt.Println()
		fmt.Println("Please run these commands manually:")
		rules := firewallRules(fw, port)
		for _, rule := range rules {
			fmt.Printf("  %s\n", rule)
		}
		fmt.Println()
		fmt.Println("If using cloud VPS, also open these ports in Security Group")
		return

	default:
		// ufw or firewalld — can auto-execute
		fmt.Printf("Detected: %s\n", string(fw))
	}

	fmt.Println()
	fmt.Println("Ports to open:")
	printRequiredPorts(port)
	fmt.Println()

	autoConfig := promptYesNo("Auto-configure?", true)

	rules := firewallRules(fw, port)

	if autoConfig {
		for _, cmd := range rules {
			fmt.Printf("  %s\n", cmd)
			if err := cmd.Run(); err != nil {
				fmt.Printf("  %s: %v\n", cmd, err)
			}
		}
	} else {
		fmt.Println("Please run these commands manually:")
		for _, cmd := range rules {
			fmt.Printf("  %s\n", cmd)
		}
	}

	fmt.Println()
	fmt.Println("If using cloud VPS, also open these ports in Security Group")
}

// printRequiredPorts displays the ports that need to be opened.
func printRequiredPorts(port int) {
	fmt.Printf("  %d/tcp + %d/udp (main)\n", port, port)
}

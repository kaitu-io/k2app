package main

import (
	_ "embed"
	"fmt"
	"os"
	"strings"
)

//go:embed client.demo.yml
var demoConfig string

var (
	version = "dev"
	commit  = "none"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	args := os.Args[1:]
	for len(args) > 0 && strings.HasPrefix(args[0], "--") {
		switch args[0] {
		case "--json":
			out.json = true
			args = args[1:]
		case "--quiet":
			args = args[1:]
		case "--help", "-h":
			printUsage()
			return
		default:
			fmt.Fprintf(os.Stderr, "Unknown flag: %s\nRun 'k2 help' for usage.\n", args[0])
			os.Exit(1)
		}
	}

	if len(args) == 0 {
		printUsage()
		os.Exit(1)
	}

	cmd := args[0]
	rest := args[1:]

	switch cmd {
	case "up":
		os.Exit(cmdUp(rest))
	case "down":
		os.Exit(cmdDown(rest))
	case "status":
		os.Exit(cmdStatus(rest))
	case "setup":
		cmdSetup(rest)
	case "run":
		cmdRun(rest)
	case "config":
		os.Exit(cmdConfig(rest))
	case "service":
		cmdService(rest)
	case "upgrade":
		cmdUpgrade(rest)
	case "healthcheck":
		cmdHealthcheck()
	case "version":
		fmt.Printf("k2 %s (%s)\n", version, commit)
	case "help", "--help", "-h":
		if len(rest) > 0 {
			printCommandHelp(rest[0])
		} else {
			printUsage()
		}
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\nRun 'k2 help' for usage.\n", cmd)
		os.Exit(1)
	}
}

// resolveConfigPath returns cfgFlag if set, else the empty string (caller will search).
func resolveConfigPath(cfgFlag string) string {
	return cfgFlag
}

// resolvePIDPath returns the PID path derived from config or the default.
func resolvePIDPath(cfgFlag string) string {
	if cfgFlag != "" {
		// Try loading config for its pid_file field.
		if cfg, err := loadClientConfigFile(cfgFlag); err == nil && cfg.PIDFile != "" {
			return cfg.PIDFile
		}
	}
	return defaultPIDPath()
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `k2 — Kaitu network tunnel

Quick start:
  k2 setup <URL>            Install as system service + connect

Commands:
  up        Connect via background daemon
  down      Disconnect
  status    Connection status

  setup     First-time setup                          [root]
  run       Start daemon (service entry point)        [root for TUN]

  config    Configuration (test, show)
  service   System service (install, uninstall)       [root]
  upgrade   Self-update
  version   Version info

Flags:
  --json    Machine-readable JSON output
  --quiet   Minimal output
  --help    Help for any command
`)
}

func printCommandHelp(cmd string) {
	switch cmd {
	case "up":
		cmdUp([]string{"--help"})
	case "setup":
		cmdSetup([]string{"--help"})
	case "config":
		cmdConfig([]string{"--help"})
	case "service":
		cmdService([]string{"--help"})
	default:
		fmt.Fprintf(os.Stderr, "No help for %q. Run 'k2 help' for command list.\n", cmd)
	}
}


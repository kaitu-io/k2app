package main

import (
	"bytes"
	_ "embed"
	"flag"
	"fmt"
	"os"
	"strings"
)

//go:embed server.demo.yml
var demoConfig string

// Version info (set by build flags).
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
			fmt.Fprintf(os.Stderr, "Unknown flag: %s\nRun 'k2s help' for usage.\n", args[0])
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
	case "run":
		cmdRun(rest)
	case "setup":
		cmdSetup(rest)
	case "user":
		cmdUser(rest)
	case "config":
		os.Exit(cmdConfig(rest))
	case "service":
		cmdService(rest)
	case "upgrade":
		cmdUpgrade(rest)
	case "version":
		fmt.Printf("k2s %s (%s)\n", version, commit)
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\nRun 'k2s help' for usage.\n", cmd)
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
		if cfg, err := loadServerConfigFile(cfgFlag); err == nil && cfg.PIDFile != "" {
			return cfg.PIDFile
		}
	}
	return defaultPIDPath()
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `k2s — Kaitu network tunnel server

Quick start:
  k2s run                   Deploy server (prints connect URL)

Commands:
  run       Start/deploy server
  setup     Setup wizard (or --port/--username for automation)
  user      User management (add, list, del, reset)

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

// cmdRun handles "k2s run [--foreground] [-c config]".
func cmdRun(args []string) {
	fs := flag.NewFlagSet("k2s run", flag.ExitOnError)
	foreground := fs.Bool("foreground", false, "stay in foreground (for service managers)")
	configPath := fs.String("c", "", "config file path")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `Start or deploy k2s server.

Usage: k2s run [flags]

  Without --foreground: run server in foreground (default for now).
  With --foreground: same behavior, explicit for service manager usage.

Flags:
`)
		fs.PrintDefaults()
	}
	fs.Parse(args)
	_ = *foreground // Both paths run foreground for now
	runServer(*configPath)
}

// cmdConfig handles "k2s config <test|show>".
func cmdConfig(args []string) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintf(os.Stderr, "Usage: k2s config <test|show>\n\n  test  Test configuration\n  show  Print example config\n")
		if len(args) == 0 {
			return 1
		}
		return 0
	}
	switch args[0] {
	case "test":
		fs := flag.NewFlagSet("k2s config test", flag.ExitOnError)
		cfgPath := fs.String("c", "", "config file path")
		fs.Parse(args[1:])
		var buf bytes.Buffer
		if err := runTestConfig(resolveConfigPath(*cfgPath), &buf); err != nil {
			out.fail("INVALID_CONFIG", err.Error(), "")
			return 2
		}
		out.printResult(buf.String())
		out.ok(map[string]string{"status": "ok"})
		return 0
	case "show":
		fmt.Print(demoConfig)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "Unknown config subcommand: %s\n", args[0])
		return 1
	}
}

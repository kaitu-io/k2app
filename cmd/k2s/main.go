package main

import (
	"bytes"
	_ "embed"
	"flag"
	"fmt"
	"os"
)

//go:embed server.demo.yml
var demoConfig string

// Version info (set by build flags).
var (
	version = "dev"
	commit  = "none"
)

func main() {
	// Define top-level flags (nginx-style: flags come before subcommands).
	fs := flag.NewFlagSet("k2s", flag.ContinueOnError)
	configPath := fs.String("c", "", "config file path")
	testConfig := fs.Bool("t", false, "test configuration and exit")
	sendSignal := fs.String("s", "", "send signal to server: stop|reload")
	showVersion := fs.Bool("v", false, "show version and exit")
	fs.Usage = printUsage

	// Parse flags stopping at first non-flag argument (subcommand).
	if err := fs.Parse(os.Args[1:]); err != nil {
		if err == flag.ErrHelp {
			os.Exit(0)
		}
		os.Exit(1)
	}

	// -v: print version and exit.
	if *showVersion {
		fmt.Printf("k2s %s (%s)\n", version, commit)
		return
	}

	// Check remaining args for subcommand.
	args := fs.Args()

	if len(args) > 0 {
		switch args[0] {
		case "version":
			fmt.Printf("k2s %s (%s)\n", version, commit)
			return
		case "setup":
			cmdSetup(args[1:])
			return
		case "service":
			cmdService(args[1:])
			return
		case "upgrade":
			cmdUpgrade(args[1:])
			return
		case "demo-config":
			fmt.Print(demoConfig)
			return
		case "-h", "--help", "help":
			printUsage()
			return
		default:
			fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", args[0])
			printUsage()
			os.Exit(1)
		}
	}

	// No subcommand — handle flags-only invocations.

	// -t: test config and exit.
	if *testConfig {
		var out bytes.Buffer
		if err := runTestConfig(resolveConfigPath(*configPath), &out); err != nil {
			fmt.Fprintf(os.Stderr, "Configuration test failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Print(out.String())
		return
	}

	// -s: send signal.
	if *sendSignal != "" {
		pidPath := resolvePIDPath(*configPath)
		var out bytes.Buffer
		if err := runSignal(*sendSignal, pidPath, &out); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Print(out.String())
		return
	}

	// Default: run as foreground server.
	runServer(*configPath)
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

Usage:
  k2s [-c config]              Start server (foreground)
  k2s -t [-c config]           Test configuration and exit
  k2s -s stop|reload           Send signal to running server
  k2s -v                       Show version and exit

  k2s setup                    Install service + start + print connect URL
  k2s service install          Install system service
  k2s service uninstall        Remove system service
  k2s upgrade [--check]        Check for and install updates
  k2s upgrade --channel=beta   Switch to beta update channel
  k2s upgrade --channel=stable Switch to stable update channel
  k2s demo-config              Print example server config (YAML)
  k2s version                  Show version info

Client: use k2 binary for client deployment.
`)
}


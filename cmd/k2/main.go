package main

import (
	"bytes"
	_ "embed"
	"flag"
	"fmt"
	"os"
)

//go:embed client.demo.yml
var demoConfig string

// Version info (set by build flags)
var (
	version = "dev"
	commit  = "none"
)

func main() {
	// Define top-level flags (nginx-style: flags come before subcommands).
	fs := flag.NewFlagSet("k2", flag.ContinueOnError)
	configPath := fs.String("c", "", "config file path")
	testConfig := fs.Bool("t", false, "test configuration and exit")
	sendSignal := fs.String("s", "", "send signal to daemon: stop|reload")
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
		fmt.Printf("k2 %s (%s)\n", version, commit)
		return
	}

	// Check remaining args for subcommand.
	args := fs.Args()

	if len(args) > 0 {
		switch args[0] {
		case "version":
			fmt.Printf("k2 %s (%s)\n", version, commit)
			return
		case "ctl":
			cmdCtl(args[1:])
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

	// Default: run as foreground daemon.
	runDaemon(*configPath)
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

Usage:
  k2 [-c config]              Start daemon (foreground)
  k2 -t [-c config]           Test configuration and exit
  k2 -s stop|reload           Send signal to running daemon
  k2 -v                       Show version and exit

  k2 ctl up [URL|config]      Connect tunnel
  k2 ctl down                 Disconnect tunnel
  k2 ctl status               Show connection status

  k2 setup [URL]              Generate config + install service + start
  k2 setup -c config.yaml     Install service from existing config

  k2 service install          Install system service
  k2 service uninstall        Remove system service

  k2 upgrade [--check]        Download and install latest version
  k2 demo-config              Print example client config (YAML)
  k2 version                  Show version info

Server: use k2s binary for server deployment.
`)
}


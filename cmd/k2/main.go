package main

import (
	_ "embed"
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
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "up":
		cmdUp(args)
	case "down":
		cmdDown(args)
	case "status":
		cmdStatus(args)
	case "run":
		cmdRun(args)
	case "open":
		cmdOpen(args)
	case "upgrade":
		cmdUpgrade(args)
	case "version":
		cmdVersion()
	case "demo-config":
		fmt.Print(demoConfig)
	case "-h", "--help", "help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `k2 — Kaitu network tunnel

Usage:
  k2 up [URL or config.yaml]   Connect tunnel (auto-manages daemon)
  k2 down                      Disconnect tunnel
  k2 status                    Show connection status
  k2 run                       Start daemon (called by service manager)
  k2 run -c <config.yaml>      Client foreground mode (dev/debug, needs sudo)

  k2 open                      Open webapp in browser
  k2 upgrade                   Download and install latest version
  k2 version                   Show version info
  k2 demo-config                Print example client config (YAML)

Server: use k2s binary for server deployment.
`)
}

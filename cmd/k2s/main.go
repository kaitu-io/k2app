package main

import (
	_ "embed"
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
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "run":
		cmdRun(args)
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
	fmt.Fprintf(os.Stderr, `k2s — Kaitu network tunnel server

Usage:
  k2s run                       Install service, start, and print connect URL
  k2s run -s <server.yaml>     Start server with config file (foreground)
  k2s run --foreground          Run in foreground (for debugging)
  k2s version                   Show version info
  k2s demo-config               Print example server config (YAML)
`)
}

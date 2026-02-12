package main

import (
	"fmt"
	"os"
)

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
  k2s run -s <server.yaml>     Start server (foreground)
  k2s run --install             Install as system service
  k2s version                   Show version info
`)
}

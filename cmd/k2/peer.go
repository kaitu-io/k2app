package main

import (
	"fmt"
	"os"
)

func cmdPeer(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: k2 peer <setup|run>")
		os.Exit(1)
	}
	switch args[0] {
	case "setup":
		cmdPeerSetup(args[1:])
	case "run":
		cmdPeerRun(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "Unknown peer command: %s\n", args[0])
		os.Exit(1)
	}
}

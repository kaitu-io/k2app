package main

import (
	"fmt"
	"os"
)

// cmdService handles "k2s service install|uninstall".
func cmdService(args []string) {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: k2s service <install|uninstall>\n")
		os.Exit(1)
	}

	switch args[0] {
	case "--help", "-h":
		fmt.Fprintf(os.Stderr, "Usage: k2s service <install|uninstall>\n")
		return

	case "install":
		fmt.Println("Installing service...")
		if err := installService(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: install service: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Service installed and started.")

	case "uninstall":
		fmt.Println("Removing service...")
		if err := uninstallService(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: uninstall service: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Service removed.")

	default:
		fmt.Fprintf(os.Stderr, "Unknown service subcommand: %s\n\nUsage: k2s service <install|uninstall>\n", args[0])
		os.Exit(1)
	}
}

package main

import (
	"fmt"
	"os"

	"github.com/kaitu-io/k2/daemon"
)

// cmdService handles "k2 service install|uninstall".
// It delegates to daemon.ServiceManager for platform-specific service management.
func cmdService(args []string) {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: k2 service <install|uninstall>\n")
		os.Exit(1)
	}

	svc := daemon.NewServiceManager()

	switch args[0] {
	case "install":
		fmt.Println("Installing service...")
		if err := svc.Install(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: install service: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Service installed and started.")

	case "uninstall":
		fmt.Println("Removing service...")
		if err := svc.Uninstall(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: uninstall service: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Service removed.")

	default:
		fmt.Fprintf(os.Stderr, "Unknown service subcommand: %s\n\nUsage: k2 service <install|uninstall>\n", args[0])
		os.Exit(1)
	}
}

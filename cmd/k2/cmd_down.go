package main

import (
	"fmt"
	"os"

	"github.com/kaitu-io/k2/daemon"
)

func cmdDown(args []string) {
	client := daemon.NewClient(daemon.DefaultAddr)
	resp, err := client.Do("down", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if resp.Code != 0 {
		fmt.Fprintf(os.Stderr, "Error: %s\n", resp.Message)
		os.Exit(1)
	}

	fmt.Println("Disconnected")
}

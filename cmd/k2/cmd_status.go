package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/kaitu-io/k2/daemon"
)

func cmdStatus(args []string) {
	client := daemon.NewClient(daemon.DefaultAddr)
	resp, err := client.Do("status", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot reach daemon (%v)\n", err)
		os.Exit(1)
	}

	if resp.Code != 0 {
		fmt.Fprintf(os.Stderr, "Error: %s\n", resp.Message)
		os.Exit(1)
	}

	data, _ := json.MarshalIndent(resp.Data, "", "  ")
	fmt.Println(string(data))
}

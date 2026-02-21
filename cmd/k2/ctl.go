package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/daemon"
)

// cmdCtl handles the "k2 ctl up|down|status" subcommands.
// It sends IPC commands directly to the running daemon.
// If the daemon is not reachable, it prints a clear error (no auto-install magic).
func cmdCtl(args []string) {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: k2 ctl <up|down|status>\n")
		os.Exit(1)
	}

	sub := args[0]
	rest := args[1:]

	switch sub {
	case "up":
		ctlUp(rest)
	case "down":
		ctlDown(rest)
	case "status":
		ctlStatus(rest)
	default:
		fmt.Fprintf(os.Stderr, "Unknown ctl subcommand: %s\n\nUsage: k2 ctl <up|down|status>\n", sub)
		os.Exit(1)
	}
}

// ctlUp sends the "up" action to the daemon with an optional config.
func ctlUp(args []string) {
	fs := flag.NewFlagSet("ctl up", flag.ExitOnError)
	pid := fs.Int("pid", 0, "parent PID to monitor (auto-disconnect on exit)")
	fs.Parse(args)

	// Resolve positional arg into a ClientConfig.
	params := map[string]any{}
	if fs.NArg() > 0 {
		arg := fs.Arg(0)
		var cfg *config.ClientConfig
		if strings.Contains(arg, "://") {
			// Wire URL — resolve to full config with defaults.
			cfg = config.ClientFromURL(arg)
		} else {
			// YAML config file.
			absPath, err := filepath.Abs(arg)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			var loadErr error
			cfg, loadErr = config.LoadClient(absPath)
			if loadErr != nil {
				fmt.Fprintf(os.Stderr, "Error: load config: %v\n", loadErr)
				os.Exit(1)
			}
		}
		params["config"] = cfg
	}
	// No arg = use daemon's saved config.

	if *pid > 0 {
		params["pid"] = *pid
	}

	// Connect to daemon — no auto-install, clear error if not reachable.
	client := daemon.NewClient(daemon.DefaultAddr)
	resp, err := client.Do("up", params)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot reach daemon at %s (%v)\n", daemon.DefaultAddr, err)
		fmt.Fprintf(os.Stderr, "Is the daemon running? Start it with: k2\n")
		os.Exit(1)
	}

	if resp.Code != 0 {
		fmt.Fprintf(os.Stderr, "Error: %s\n", resp.Message)
		os.Exit(1)
	}

	fmt.Println("Connecting...")

	// Poll status until connected or error.
	for i := 0; i < 30; i++ {
		time.Sleep(1 * time.Second)
		resp, err := client.Do("status", nil)
		if err != nil {
			continue
		}
		if resp.Code != 0 {
			continue
		}

		data, _ := json.Marshal(resp.Data)
		var status struct {
			State string `json:"state"`
			Error string `json:"error"`
		}
		json.Unmarshal(data, &status)

		switch status.State {
		case "connected":
			fmt.Println("Connected")
			return
		case "stopped":
			if status.Error != "" {
				fmt.Fprintf(os.Stderr, "Connection failed: %s\n", status.Error)
			} else {
				fmt.Fprintf(os.Stderr, "Connection stopped\n")
			}
			os.Exit(1)
		}
	}

	fmt.Println("Connected (or still connecting, check `k2 ctl status`)")
}

// ctlDown sends the "down" action to the daemon.
func ctlDown(args []string) {
	_ = args // no flags for down

	client := daemon.NewClient(daemon.DefaultAddr)
	resp, err := client.Do("down", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot reach daemon at %s (%v)\n", daemon.DefaultAddr, err)
		fmt.Fprintf(os.Stderr, "Is the daemon running? Start it with: k2\n")
		os.Exit(1)
	}

	if resp.Code != 0 {
		fmt.Fprintf(os.Stderr, "Error: %s\n", resp.Message)
		os.Exit(1)
	}

	fmt.Println("Disconnected")
}

// ctlStatus sends the "status" action to the daemon and prints JSON output.
func ctlStatus(args []string) {
	_ = args // no flags for status

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

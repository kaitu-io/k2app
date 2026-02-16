package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/kaitu-io/k2/daemon"
)

func cmdUp(args []string) {
	fs := flag.NewFlagSet("up", flag.ExitOnError)
	pid := fs.Int("pid", 0, "parent PID to monitor (auto-disconnect on exit)")
	fs.Parse(args)

	// Determine params from positional arg
	params := map[string]any{}
	if fs.NArg() > 0 {
		arg := fs.Arg(0)
		if strings.Contains(arg, "://") {
			// URL
			params["wire_url"] = arg
		} else {
			// Config file — resolve to absolute path
			absPath, err := filepath.Abs(arg)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			params["config_path"] = absPath
		}
	}
	// No arg = use daemon's saved config

	if *pid > 0 {
		params["pid"] = *pid
	}

	// Ensure daemon is running
	if err := ensureServiceRunning(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Send up command to daemon
	client := daemon.NewClient(daemon.DefaultAddr)
	resp, err := client.Do("up", params)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if resp.Code != 0 {
		fmt.Fprintf(os.Stderr, "Error: %s\n", resp.Message)
		os.Exit(1)
	}

	fmt.Println("Connecting...")

	// Poll status until connected or error
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

	fmt.Println("Connected (or still connecting, check `k2 status`)")
}

func ensureServiceRunning() error {
	client := daemon.NewClient(daemon.DefaultAddr)
	if client.Ping() == nil {
		return nil
	}

	fmt.Println("Service not running, installing...")
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find executable: %w", err)
	}

	cmd := exec.Command("sudo", exe, "run", "--install")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("install service: %w", err)
	}

	return waitForDaemon(5 * time.Second)
}

func waitForDaemon(timeout time.Duration) error {
	client := daemon.NewClient(daemon.DefaultAddr)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if client.Ping() == nil {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("daemon not ready after %v", timeout)
}

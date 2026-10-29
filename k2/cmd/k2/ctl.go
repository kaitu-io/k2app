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

func cmdUp(args []string) int {
	fs := flag.NewFlagSet("k2 up", flag.ExitOnError)
	pid := fs.Int("pid", 0, "parent PID to monitor (auto-disconnect on exit)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `Connect via background daemon.

Usage: k2 up [URL|config.yaml] [flags]

  No args: reconnect with saved config.
  URL:     connect with k2v5:// or k2subs:// URL.
  File:    connect with YAML config file.

Flags:
`)
		fs.PrintDefaults()
	}
	fs.Parse(args)

	params := map[string]any{}
	if fs.NArg() > 0 {
		arg := fs.Arg(0)
		var cfg *config.ClientConfig
		if strings.HasPrefix(arg, "k2subs://") {
			cfg = config.ClientFromSubsURL(arg)
		} else if strings.Contains(arg, "://") {
			cfg = config.ClientFromURL(arg)
		} else {
			absPath, err := filepath.Abs(arg)
			if err != nil {
				out.fail("INVALID_ARG", err.Error(), "")
				return 1
			}
			var loadErr error
			cfg, loadErr = config.LoadClient(absPath)
			if loadErr != nil {
				out.fail("INVALID_CONFIG", loadErr.Error(), "")
				return 1
			}
		}
		params["config"] = cfg
	}
	if *pid > 0 {
		params["pid"] = *pid
	}

	client := daemon.NewClient(daemon.DefaultAddr)
	resp, err := client.Do("up", params)
	if err != nil {
		out.fail("DAEMON_NOT_RUNNING", "daemon not running", daemonNotRunningHint())
		return 2
	}
	if resp.Code != 0 {
		out.fail("CONNECT_FAILED", resp.Message, "")
		return 2
	}

	out.printResult("Connecting...")

	for i := 0; i < 30; i++ {
		time.Sleep(1 * time.Second)
		resp, err := client.Do("status", nil)
		if err != nil || resp.Code != 0 {
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
			out.printResult("Connected")
			out.ok(resp.Data)
			return 0
		case "stopped":
			msg := "connection stopped"
			if status.Error != "" {
				msg = status.Error
			}
			out.fail("CONNECT_FAILED", msg, "")
			return 2
		}
	}

	out.printResult("Still connecting, check 'k2 status'")
	return 0
}

func cmdDown(args []string) int {
	fs := flag.NewFlagSet("k2 down", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Disconnect tunnel.\n\nUsage: k2 down\n")
	}
	fs.Parse(args)

	client := daemon.NewClient(daemon.DefaultAddr)
	resp, err := client.Do("down", nil)
	if err != nil {
		out.fail("DAEMON_NOT_RUNNING", "daemon not running", daemonNotRunningHint())
		return 2
	}
	if resp.Code != 0 {
		out.fail("DISCONNECT_FAILED", resp.Message, "")
		return 2
	}
	out.printResult("Disconnected")
	out.ok(map[string]string{"state": "disconnected"})
	return 0
}

func cmdStatus(args []string) int {
	fs := flag.NewFlagSet("k2 status", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Show connection status.\n\nUsage: k2 status\n")
	}
	fs.Parse(args)

	client := daemon.NewClient(daemon.DefaultAddr)
	resp, err := client.Do("status", nil)
	if err != nil {
		out.fail("DAEMON_NOT_RUNNING", "daemon not running", daemonNotRunningHint())
		return 2
	}
	if resp.Code != 0 {
		out.fail("STATUS_FAILED", resp.Message, "")
		return 2
	}

	if out.json {
		out.ok(resp.Data)
	} else {
		data, _ := json.MarshalIndent(resp.Data, "", "  ")
		fmt.Println(string(data))
	}
	return 0
}

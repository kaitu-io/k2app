//go:build linux

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/gateway"
)

var (
	version = "dev"
	commit  = "unknown"

	// centerBeaconBase is the Center base URL the gateway's public-IP discovery
	// beacon POSTs to. Compile-time overridable via ldflags
	// -X main.centerBeaconBase=...
	centerBeaconBase = "https://kaitu.io"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	args := os.Args[1:]
	for len(args) > 0 && strings.HasPrefix(args[0], "--") {
		switch args[0] {
		case "--json":
			out.json = true
			args = args[1:]
		case "--quiet":
			args = args[1:]
		case "--help", "-h":
			printUsage()
			return
		default:
			fmt.Fprintf(os.Stderr, "Unknown flag: %s\nRun 'k2r help' for usage.\n", args[0])
			os.Exit(1)
		}
	}

	if len(args) == 0 {
		printUsage()
		os.Exit(1)
	}

	cmd := args[0]
	rest := args[1:]

	switch cmd {
	case "run":
		cmdRun(rest)
	case "setup":
		cmdSetup(rest)
	case "up":
		os.Exit(cmdUp(rest))
	case "down":
		os.Exit(cmdDown(rest))
	case "status":
		os.Exit(cmdStatus(rest))
	case "reset":
		os.Exit(cmdReset(rest))
	case "service":
		cmdService(rest)
	case "upgrade":
		cmdUpgrade(rest)
	case "version":
		fmt.Printf("k2r %s (%s)\n", version, commit)
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\nRun 'k2r help' for usage.\n", cmd)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `k2r — Kaitu network tunnel gateway

Quick start:
  k2r setup <URL>            Install as gateway service + connect
  k2r run                    Start gateway daemon

Commands:
  run       Start gateway daemon (service entry point)
  setup     First-time setup: generate config + install service

  up        Connect via gateway daemon
  down      Disconnect
  status    Connection status
  reset     Unbind router (wipe credential, state, control key)

  service   System service (install, uninstall)
  upgrade   Check and install updates
  version   Version info

Flags:
  --json    Machine-readable JSON output
  --quiet   Minimal output
  --help    Help for any command
`)
}

// cmdRun starts the gateway daemon (service entry point).
func cmdRun(args []string) {
	fs := flag.NewFlagSet("k2r run", flag.ExitOnError)
	configPath := fs.String("c", "", "config file path")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Start gateway daemon.\n\nUsage: k2r run [-c config.yml]\n\nFlags:\n")
		fs.PrintDefaults()
	}
	fs.Parse(args)

	cfgPath := *configPath
	if cfgPath == "" {
		cfgPath = "/etc/k2r/k2r.yml"
	}
	cfg, err := gateway.LoadGateway(cfgPath)
	if err != nil {
		cfg = &gateway.GatewayConfig{}
		gateway.SetGatewayDefaults(cfg)
	}
	closer, err := config.SetupLogging(cfg.Log, "k2r")
	if err != nil {
		out.fail("LOG_FAILED", err.Error(), "")
		os.Exit(1)
	}
	defer closer()

	g := gateway.New(version, commit, runtime.GOARCH, centerBeaconBase)
	stopOnSignal(g.Stop, func() { os.Exit(1) })
	if err := g.Run(cfg.Listen); err != nil {
		out.fail("GATEWAY_FAILED", err.Error(), "")
		os.Exit(1)
	}
}

// cmdSetup handles "k2r setup <URL>" or "k2r setup -c config.yml".
func cmdSetup(args []string) {
	fs := flag.NewFlagSet("k2r setup", flag.ExitOnError)
	configFile := fs.String("c", "", "existing config file to install")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `First-time gateway setup.

Usage: k2r setup <k2v5://URL>
       k2r setup -c config.yml

Flags:
`)
		fs.PrintDefaults()
	}
	fs.Parse(args)

	var cfg *gateway.GatewayConfig
	if *configFile != "" {
		var err error
		cfg, err = gateway.LoadGateway(*configFile)
		if err != nil {
			out.fail("INVALID_CONFIG", err.Error(), "")
			os.Exit(1)
		}
	} else if fs.NArg() > 0 {
		cfg = gateway.GatewayFromURL(fs.Arg(0))
	} else {
		fs.Usage()
		os.Exit(1)
	}

	// Shared persistence path with the panel set-credential action (Cleanup
	// #1): one source-of-truth k2r.yml that `k2r run` reads back on restart.
	if err := gateway.PersistConfig(cfg); err != nil {
		out.fail("WRITE_FAILED", err.Error(), "")
		os.Exit(1)
	}

	if err := InstallService(); err != nil {
		out.fail("SERVICE_FAILED", err.Error(), "")
		os.Exit(1)
	}

	// The daemon only auto-connects from a persisted state.json (previous
	// session), never from a freshly written k2r.yml — and when the service
	// was already running (install script: `service install` then `setup`),
	// InstallService's start is a no-op. Without this, "installed and started"
	// left the router disconnected until someone ran `k2r up` by hand.
	if msg, err := connectAfterSetup(); err != nil {
		out.printResult("k2r installed and started, but the first connect failed: " + err.Error())
		out.printResult("  k2r up       -- retry")
	} else {
		out.printResult("k2r installed and started (" + msg + ").")
	}
	out.printResult("  k2r status   -- check connection")
	out.printResult("  k2r down     -- disconnect")
	out.ok(map[string]string{"status": "installed", "config": "/etc/k2r/k2r.yml"})
}

// stopOnSignal turns the service manager's SIGTERM (`/etc/init.d/k2r stop`,
// `systemctl stop`, reboot) and a foreground Ctrl-C into a graceful stop, so
// the daemon takes its firewall rules with it. Go's default for both signals
// is to die on the spot, which left the rules behind: LAN DNS redirected to a
// port nobody listened on. A second signal means the first stop is stuck —
// give up.
//
// SIGINT is left alone when the process was started with it ignored (a
// background job of a non-interactive shell): Notify would un-ignore it.
func stopOnSignal(stop, giveUp func()) {
	sigs := []os.Signal{syscall.SIGTERM}
	if !signal.Ignored(syscall.SIGINT) {
		sigs = append(sigs, syscall.SIGINT)
	}
	ch := make(chan os.Signal, 2)
	signal.Notify(ch, sigs...)
	go func() {
		sig := <-ch
		slog.Info("k2r: stopping", "signal", sig.String())
		stop()
		<-ch
		giveUp()
	}()
}

// connectAfterSetup waits for the gateway daemon's IPC endpoint to come up
// (procd/systemd start is asynchronous) and issues an argument-less "up",
// which makes the daemon load the k2r.yml that setup just persisted.
func connectAfterSetup() (string, error) {
	client := gateway.NewClient(gateway.DefaultGatewayAddr)
	var lastErr error
	deadline := time.Now().Add(setupConnectWait)
	for time.Now().Before(deadline) {
		resp, err := client.Do("up", nil)
		if err == nil {
			if resp.Code != 0 {
				return "", fmt.Errorf("%s", resp.Message)
			}
			return resp.Message, nil
		}
		lastErr = err
		time.Sleep(500 * time.Millisecond)
	}
	return "", fmt.Errorf("gateway daemon not reachable: %w", lastErr)
}

// setupConnectWait bounds how long `k2r setup` waits for the daemon's IPC
// endpoint after starting the service.
const setupConnectWait = 15 * time.Second

// cmdUp connects the gateway via daemon IPC.
func cmdUp(args []string) int {
	fs := flag.NewFlagSet("k2r up", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `Connect via gateway daemon.

Usage: k2r up [URL|config.yml]

  No args: reconnect with saved config.
  URL:     connect with k2v5:// URL.
  File:    connect with YAML config file.
`)
	}
	fs.Parse(args)

	client := gateway.NewClient(gateway.DefaultGatewayAddr)
	params := map[string]any{}
	if fs.NArg() > 0 {
		arg := fs.Arg(0)
		if _, err := os.Stat(arg); err == nil {
			cfg, err := gateway.LoadGateway(arg)
			if err != nil {
				out.fail("INVALID_CONFIG", err.Error(), "")
				return 1
			}
			params["config"] = cfg
		} else {
			cfg := gateway.GatewayFromURL(arg)
			params["config"] = cfg
		}
	}

	resp, err := client.Do("up", params)
	if err != nil {
		out.fail("DAEMON_NOT_RUNNING", "gateway daemon not running", "run 'k2r setup <URL>' or 'systemctl start k2r'")
		return 2
	}
	if resp.Code != 0 {
		out.fail("CONNECT_FAILED", resp.Message, "")
		return 2
	}

	out.printResult(resp.Message)
	out.ok(map[string]string{"status": "connecting"})
	return 0
}

// cmdDown disconnects the gateway.
func cmdDown(args []string) int {
	fs := flag.NewFlagSet("k2r down", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Disconnect gateway.\n\nUsage: k2r down\n")
	}
	fs.Parse(args)

	client := gateway.NewClient(gateway.DefaultGatewayAddr)
	resp, err := client.Do("down", nil)
	if err != nil {
		out.fail("DAEMON_NOT_RUNNING", "gateway daemon not running", "run 'k2r setup <URL>' or 'systemctl start k2r'")
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

// cmdStatus shows the gateway connection status.
func cmdStatus(args []string) int {
	fs := flag.NewFlagSet("k2r status", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Show gateway connection status.\n\nUsage: k2r status\n")
	}
	fs.Parse(args)

	client := gateway.NewClient(gateway.DefaultGatewayAddr)
	resp, err := client.Do("status", nil)
	if err != nil {
		out.fail("DAEMON_NOT_RUNNING", "gateway daemon not running", "run 'k2r setup <URL>' or 'systemctl start k2r'")
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

// cmdReset unbinds the router. Daemon running → authenticated-exempt loopback
// IPC; daemon stopped → wipe files directly.
func cmdReset(args []string) int {
	fs := flag.NewFlagSet("k2r reset", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Unbind router: wipe credential, state and control key.\n\nUsage: k2r reset\n")
	}
	fs.Parse(args)

	client := gateway.NewClient(gateway.DefaultGatewayAddr)
	if resp, err := client.Do("reset", nil); err == nil {
		if resp.Code != 0 {
			out.fail("RESET_FAILED", resp.Message, "")
			return 2
		}
		out.printResult("Reset complete")
		out.ok(map[string]string{"state": "unbound"})
		return 0
	}
	// Daemon not running — wipe on-disk state directly.
	if err := gateway.WipeBindingFiles(); err != nil {
		out.fail("RESET_FAILED", err.Error(), "run as root")
		return 2
	}
	out.printResult("Reset complete (daemon not running, files wiped)")
	out.ok(map[string]string{"state": "unbound"})
	return 0
}

func cmdService(args []string) {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintf(os.Stderr, "Usage: k2r service <install|uninstall>\n")
		if len(args) == 0 {
			os.Exit(1)
		}
		return
	}

	switch args[0] {
	case "install":
		if err := InstallService(); err != nil {
			out.fail("SERVICE_FAILED", err.Error(), "")
			os.Exit(1)
		}
		out.printResult("k2r service installed and started.")
		out.ok(map[string]string{"status": "installed"})

	case "uninstall":
		if err := UninstallService(); err != nil {
			out.fail("SERVICE_FAILED", err.Error(), "")
			os.Exit(1)
		}
		out.printResult("k2r service removed.")
		out.ok(map[string]string{"status": "removed"})

	default:
		fmt.Fprintf(os.Stderr, "Unknown service command: %s\n", args[0])
		os.Exit(1)
	}
}

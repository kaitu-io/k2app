//go:build linux

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/gateway"
	"gopkg.in/yaml.v3"
)

var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	fs := flag.NewFlagSet("k2r", flag.ContinueOnError)
	configPath := fs.String("c", "", "config file path")
	showVersion := fs.Bool("v", false, "show version")
	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(1)
	}

	if *showVersion {
		fmt.Printf("k2r %s (%s)\n", version, commit)
		return
	}

	args := fs.Args()
	if len(args) == 0 {
		runDaemon(*configPath)
		return
	}

	switch args[0] {
	case "setup":
		cmdSetup(args[1:])
	case "ctl":
		cmdCtl(args[1:])
	case "service":
		cmdService(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", args[0])
		fmt.Fprintln(os.Stderr, "usage: k2r [setup|ctl|service] or k2r -c config.yml")
		os.Exit(1)
	}
}

func runDaemon(configPath string) {
	if configPath == "" {
		configPath = "/etc/k2r/k2r.yml"
	}
	cfg, err := gateway.LoadGateway(configPath)
	if err != nil {
		cfg = &gateway.GatewayConfig{}
		gateway.SetGatewayDefaults(cfg)
	}
	closer, err := config.SetupLogging(cfg.Log, "k2r")
	if err != nil {
		fmt.Fprintf(os.Stderr, "logging: %v\n", err)
		os.Exit(1)
	}
	defer closer()

	g := gateway.New()
	if err := g.Run(cfg.Listen); err != nil {
		fmt.Fprintf(os.Stderr, "gateway: %v\n", err)
		os.Exit(1)
	}
}

func cmdSetup(args []string) {
	fs := flag.NewFlagSet("setup", flag.ExitOnError)
	configFile := fs.String("c", "", "existing config file to install")
	fs.Parse(args)

	var cfg *gateway.GatewayConfig
	if *configFile != "" {
		var err error
		cfg, err = gateway.LoadGateway(*configFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "load config: %v\n", err)
			os.Exit(1)
		}
	} else if fs.NArg() > 0 {
		cfg = gateway.GatewayFromURL(fs.Arg(0))
	} else {
		fmt.Fprintln(os.Stderr, "usage: k2r setup <k2v5://URL> or k2r setup -c config.yml")
		os.Exit(1)
	}

	os.MkdirAll("/etc/k2r", 0755)
	data, _ := yaml.Marshal(cfg)
	if err := os.WriteFile("/etc/k2r/k2r.yml", data, 0600); err != nil {
		fmt.Fprintf(os.Stderr, "save config: %v\n", err)
		os.Exit(1)
	}

	if err := gateway.InstallService(); err != nil {
		fmt.Fprintf(os.Stderr, "install service: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("k2r installed and started.")
	fmt.Println("  k2r ctl status   -- check connection")
	fmt.Println("  k2r ctl down     -- disconnect")
}

func cmdCtl(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: k2r ctl [up|down|status]")
		os.Exit(1)
	}

	client := gateway.NewClient(gateway.DefaultGatewayAddr)

	switch args[0] {
	case "up":
		params := map[string]any{}
		if len(args) > 1 {
			arg := args[1]
			if _, err := os.Stat(arg); err == nil {
				cfg, err := gateway.LoadGateway(arg)
				if err != nil {
					fmt.Fprintf(os.Stderr, "load config: %v\n", err)
					os.Exit(1)
				}
				params["config"] = cfg
			} else {
				cfg := gateway.GatewayFromURL(arg)
				params["config"] = cfg
			}
		}
		resp, err := client.Do("up", params)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		fmt.Println(resp.Message)

	case "down":
		resp, err := client.Do("down", nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		fmt.Println(resp.Message)

	case "status":
		resp, err := client.Do("status", nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		data, _ := json.MarshalIndent(resp.Data, "", "  ")
		fmt.Println(string(data))

	default:
		fmt.Fprintf(os.Stderr, "unknown ctl command: %s\n", args[0])
		os.Exit(1)
	}
}

func cmdService(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: k2r service [install|uninstall]")
		os.Exit(1)
	}

	switch args[0] {
	case "install":
		if err := gateway.InstallService(); err != nil {
			fmt.Fprintf(os.Stderr, "install: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("k2r service installed and started.")

	case "uninstall":
		if err := gateway.UninstallService(); err != nil {
			fmt.Fprintf(os.Stderr, "uninstall: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("k2r service removed.")

	default:
		fmt.Fprintf(os.Stderr, "unknown service command: %s\n", args[0])
		os.Exit(1)
	}
}

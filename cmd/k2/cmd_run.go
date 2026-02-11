package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/daemon"
)

func cmdRun(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	clientCfg := fs.String("c", "", "client config file (foreground mode)")
	serverCfg := fs.String("s", "", "server config file (foreground mode)")
	install := fs.Bool("install", false, "install as system service and start")
	fs.Parse(args)

	switch {
	case *install:
		runInstall()
	case *clientCfg != "":
		runClientForeground(*clientCfg)
	case *serverCfg != "":
		runServerForeground(*serverCfg)
	default:
		runDaemon()
	}
}

func runInstall() {
	svc := daemon.NewServiceManager()
	if err := svc.Install(); err != nil {
		fmt.Fprintf(os.Stderr, "Error installing service: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Service installed and started")
}

func runDaemon() {
	daemon.ApplyResourceLimits()

	d := daemon.New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("Shutting down...")
		cancel()
	}()

	if err := d.Run(ctx); err != nil && err != context.Canceled {
		log.Fatalf("Daemon error: %v", err)
	}
}

// runClientForeground runs the client in foreground (dev/debug mode, needs sudo).
// TODO: Implement when L4 tunnel rewrite is complete.
// The wire layer now uses DialTCP/DialUDP (k2v5), but core/tunnel.go still
// has the old L3 VPN model. This needs the L4 tunnel rewrite first.
func runClientForeground(cfgPath string) {
	cfg, err := config.LoadClient(cfgPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	if cfg.Server == "" {
		log.Fatal("server URL is required in client config")
	}

	log.Printf("k2 client foreground mode")
	log.Printf("Server: %s", cfg.Server)
	log.Fatal("Not yet implemented: requires L4 tunnel rewrite (core/tunnel.go)")
}

// runServerForeground runs the server in foreground (VPS deployment).
// TODO: Implement when full QUIC/TCP-WS server is complete.
func runServerForeground(cfgPath string) {
	cfg, err := config.LoadServer(cfgPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	log.Printf("k2 server foreground mode")
	log.Printf("Listen: %s", cfg.Listen)
	log.Fatal("Not yet implemented: requires full QUIC/TCP-WS server")
}

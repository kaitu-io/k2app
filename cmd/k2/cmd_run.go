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
	"github.com/kaitu-io/k2/wintun"
)

func cmdRun(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	clientCfg := fs.String("c", "", "client config file (foreground mode)")
	install := fs.Bool("install", false, "install as system service and start")
	fs.Parse(args)

	switch {
	case *install:
		runInstall()
	case *clientCfg != "":
		runClientForeground(*clientCfg)
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
	if _, err := wintun.EnsureExtracted(); err != nil {
		log.Printf("wintun: %v", err)
	}

	daemon.ApplyResourceLimits()

	go checkVersionAsync()

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

// runClientForeground runs the client in foreground (dev/debug mode, needs sudo for TUN).
func runClientForeground(cfgPath string) {
	if _, err := wintun.EnsureExtracted(); err != nil {
		log.Printf("wintun: %v", err)
	}

	cfg, err := config.LoadClient(cfgPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	if cfg.Server == "" {
		log.Fatal("server URL is required in client config")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	tunnel, err := daemon.BuildTunnel(ctx, cfg)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer tunnel.Close()

	log.Printf("k2 connected (%s), Ctrl+C to stop", cfg.Server)
	<-ctx.Done()
}
